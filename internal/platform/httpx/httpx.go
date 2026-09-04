// Package httpx holds the small helpers every module's HTTP handlers
// share, so none of them hand-rolls JSON encoding or status codes.
package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
)

// WriteJSON writes v as the response body with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// DecodeJSON reads a JSON request body into v.
func DecodeJSON(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return errors.New("invalid JSON body")
	}
	return nil
}
