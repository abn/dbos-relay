// Package problem implements RFC 9457 problem details, the error format the
// API contract requires on every non-2xx response.
package problem

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// ContentType is the media type RFC 9457 defines.
const ContentType = "application/problem+json"

// Problem is a single RFC 9457 problem detail object.
type Problem struct {
	Type     string    `json:"type,omitempty"`
	Title    string    `json:"title"`
	Status   int       `json:"status"`
	Detail   string    `json:"detail,omitempty"`
	Instance string    `json:"instance,omitempty"`
	Errors   []Invalid `json:"errors,omitempty"`
}

// Invalid describes one validation failure within a Problem.
type Invalid struct {
	Detail  string `json:"detail"`
	Pointer string `json:"pointer,omitempty"`
}

// Error lets a Problem travel as an error through handler code.
func (p *Problem) Error() string { return p.Title }

// Write renders p to w with the correct media type and status.
func Write(w http.ResponseWriter, p *Problem) {
	w.Header().Set("Content-Type", ContentType)
	w.WriteHeader(p.Status)
	if err := json.NewEncoder(w).Encode(p); err != nil {
		// The status line is already sent, so there is nowhere to report
		// this to the client. Log it and move on.
		slog.Error("writing problem response", "error", err)
	}
}
