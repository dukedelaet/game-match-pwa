package game

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// One mutex serialises every session transition and every poll. With the PWA
// polling each live session once a second, every poll queues behind every other
// poll and behind complete(), which runs the scorer while holding the lock. The
// mutex is also per-process, so the engine cannot be scaled horizontally at all.
func TestUnrelatedSessionsDoNotBlockEachOther(t *testing.T) {
	e, st, _ := newEngineRig(t)
	ctx := context.Background()

	alex := userID(t, st, "Alex")
	jordan := userID(t, st, "Jordan")

	one, err := e.StartSession(ctx, "this_or_that", "queue_1v1", []string{alex, jordan})
	require.NoError(t, err)
	two, err := e.StartSession(ctx, "this_or_that", "queue_1v1", []string{jordan, alex})
	require.NoError(t, err)
	require.NotEqual(t, one.ID, two.ID)

	// Hold session one's lock, then check that session two is still servable.
	held := e.sessionLock(one.ID)
	held.Lock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = e.View(ctx, two.ID, jordan)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		held.Unlock()
		t.Fatal("a view of an unrelated session blocked behind another session's lock")
	}
	held.Unlock()
}

// Polls for the same session must still be serialised: that is what the lock is
// for.
func TestSameSessionOperationsAreSerialised(t *testing.T) {
	e, st, _ := newEngineRig(t)
	ctx := context.Background()

	alex := userID(t, st, "Alex")
	jordan := userID(t, st, "Jordan")
	ses, err := e.StartSession(ctx, "this_or_that", "queue_1v1", []string{alex, jordan})
	require.NoError(t, err)

	held := e.sessionLock(ses.ID)
	held.Lock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = e.View(ctx, ses.ID, alex)
	}()

	select {
	case <-done:
		held.Unlock()
		t.Fatal("a view of the held session ran while its lock was held")
	case <-time.After(200 * time.Millisecond):
	}

	held.Unlock()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the view never completed after the lock was released")
	}
}

// Many sessions polled at once must all complete, without deadlock.
func TestConcurrentPollsAcrossSessionsAllComplete(t *testing.T) {
	e, st, _ := newEngineRig(t)
	ctx := context.Background()

	alex := userID(t, st, "Alex")
	jordan := userID(t, st, "Jordan")

	var sessions []string
	for i := 0; i < 4; i++ {
		ses, err := e.StartSession(ctx, "this_or_that", "queue_1v1", []string{alex, jordan})
		require.NoError(t, err)
		sessions = append(sessions, ses.ID)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(sessions)*4)
	for _, id := range sessions {
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				if _, err := e.View(ctx, id, alex); err != nil {
					errCh <- err
				}
			}(id)
		}
	}

	waitCh := make(chan struct{})
	go func() { wg.Wait(); close(waitCh) }()
	select {
	case <-waitCh:
	case <-time.After(10 * time.Second):
		t.Fatal("concurrent polls deadlocked")
	}
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
}
