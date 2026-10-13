package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"
)

// A server with no timeouts accepts unlimited slow-loris connections, each
// holding a goroutine for as long as the client keeps the socket open. Caddy
// fronts the public listener, but the service also binds loopback and the
// runbook curls it directly, so the limits belong in the binary.
func TestHTTPServerHasTimeouts(t *testing.T) {
	srv := newHTTPServer("127.0.0.1:0", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	for name, got := range map[string]time.Duration{
		"ReadHeaderTimeout": srv.ReadHeaderTimeout,
		"ReadTimeout":       srv.ReadTimeout,
		"WriteTimeout":      srv.WriteTimeout,
		"IdleTimeout":       srv.IdleTimeout,
	} {
		if got <= 0 {
			t.Errorf("%s = %v, want a positive value", name, got)
		}
	}
}

// ReadHeaderTimeout is the one that actually stops a slow-loris: it bounds the
// window between opening a connection and finishing the request headers.
func TestReadHeaderTimeoutIsShort(t *testing.T) {
	srv := newHTTPServer("127.0.0.1:0", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	if srv.ReadHeaderTimeout > 15*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want <= 15s", srv.ReadHeaderTimeout)
	}
	if srv.ReadHeaderTimeout <= 0 {
		t.Error("ReadHeaderTimeout must be set; the zero value means unlimited")
	}
}

// Shutdown must let an in-flight request finish instead of severing it. A
// deploy is a systemctl restart, and today that drops whatever the client
// happened to be doing -- including writes whose commit landed but whose
// response was lost, which the client then retries and duplicates.
func TestShutdownDrainsInFlightRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)

	srv := newHTTPServer("127.0.0.1:0", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("finished"))
	}))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { done <- srv.Serve(ln) }()

	base := "http://" + ln.Addr().String()
	respCh := make(chan string, 1)
	go func() {
		resp, err := http.Get(base + "/slow")
		if err != nil {
			respCh <- "error: " + err.Error()
			return
		}
		defer func() { _ = resp.Body.Close() }()
		buf := make([]byte, 16)
		n, _ := resp.Body.Read(buf)
		respCh <- string(buf[:n])
	}()

	<-started // the handler is mid-flight

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- srv.Shutdown(ctx) }()

	close(release) // let the handler finish

	select {
	case got := <-respCh:
		if got != "finished" {
			t.Errorf("client got %q, want %q -- the request was severed instead of drained", got, "finished")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("client never received a response")
	}

	select {
	case err := <-shutdownDone:
		if err != nil {
			t.Errorf("Shutdown returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown did not return")
	}

	// Serve must report ErrServerClosed once Shutdown has run.
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("Serve returned %v, want ErrServerClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after Shutdown")
	}
}

// A shutdown with an already-cancelled context must give up rather than block,
// so a stuck connection cannot prevent the process from exiting.
func TestShutdownHonoursContextDeadline(t *testing.T) {
	blocked := make(chan struct{})
	defer close(blocked)

	srv := newHTTPServer("127.0.0.1:0", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-blocked // never returns
	}))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()

	// Open a connection so the server has something to wait for.
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_, _ = conn.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"))

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	if err := srv.Shutdown(ctx); err == nil {
		t.Log("Shutdown returned nil; the request completed before the deadline")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("Shutdown took %v, want it bounded by the context deadline", elapsed)
	}
}
