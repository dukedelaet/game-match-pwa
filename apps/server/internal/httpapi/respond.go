package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
)

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorEnvelope struct {
	Error apiError `json:"error"`
}

// WriteJSON writes a JSON response.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("encode response: %v", err)
	}
}

// WriteError writes the app's error envelope.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, errorEnvelope{Error: apiError{Code: code, Message: message}})
}

// notFound is the generic 404 used to avoid leaking existence.
func notFound(w http.ResponseWriter) {
	WriteError(w, http.StatusNotFound, "not_found", "Not found")
}
