package response

import (
	"encoding/json"
	"fmt"
	run "github.com/977ADAM/marketing-agents/internal/core/run"
	"net/http"
)

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func WriteError(w http.ResponseWriter, status int, code, msg string) {
	WriteJSON(w, status, map[string]apiError{"error": {Code: code, Message: msg}})
}

func WriteSSE(w http.ResponseWriter, event string, snap run.Snapshot) {
	b, _ := json.Marshal(snap)
	if event != "" {
		fmt.Fprintf(w, "event: %s\n", event)
	}
	fmt.Fprintf(w, "data: %s\n\n", b)
}
