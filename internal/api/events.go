package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// events streams what changed (ADR-072) to an open dashboard page: the
// tables written, so it refreshes what it shows (SSE, one per tab).
func (s *server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok || s.cfg.Events == nil {
		writeError(w, http.StatusNotImplemented, "không có luồng sự kiện")
		return
	}
	notes, stop := s.cfg.Events.Subscribe()
	defer stop()
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	h.Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ": ok\n\n")
	flusher.Flush()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
		case tables := <-notes:
			raw, _ := json.Marshal(map[string]any{"tables": tables})
			fmt.Fprintf(w, "event: change\ndata: %s\n\n", raw)
		}
		flusher.Flush()
	}
}
