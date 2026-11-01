package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// An error caused by the request itself, answered with 400 instead of 500.
type badRequest struct{ msg string }

func (e badRequest) Error() string { return e.msg }

func badRequestf(format string, args ...any) error {
	return badRequest{fmt.Sprintf(format, args...)}
}

// Register a POST endpoint that takes a JSON body and answers with the
// handler's result as JSON. A failure is answered as
// {"success": false, "message": ...}.
func postJSON[Req any](path string, handle func(Req) (any, error)) {
	http.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req Req
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request format", http.StatusBadRequest)
			return
		}

		result, err := handle(req)
		fmt.Println(path, "handled", err == nil, err)

		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			status := http.StatusInternalServerError
			if errors.As(err, &badRequest{}) {
				status = http.StatusBadRequest
			}
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(SendMessageResponse{Success: false, Message: err.Error()})
			return
		}
		json.NewEncoder(w).Encode(result)
	})
}
