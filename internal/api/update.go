package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"time"

	"bitbucket.org/senprints/agent-office/internal/selfupdate"
)

type buildInfo struct {
	Version  string `json:"version"`
	Revision string `json:"revision,omitempty"`
	Time     string `json:"time,omitempty"`
	Dirty    bool   `json:"dirty"`
}

func currentBuild(version string) buildInfo {
	b := buildInfo{Version: version}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				b.Revision = s.Value
			case "vcs.time":
				b.Time = s.Value
			case "vcs.modified":
				b.Dirty = s.Value == "true"
			}
		}
	}
	return b
}

// updateStatus tells the dashboard whether office can update itself, what is
// running, and how the last update went.
func (s *server) updateStatus(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{
		"supervised": s.cfg.Supervised,
		"build":      currentBuild(s.cfg.Version),
		"last":       selfupdate.LoadResult(s.cfg.System.HomeDir),
		"busy":       s.busyWork(),
	}
	if s.cfg.Updater != nil {
		src := s.cfg.Updater.Source()
		_, _, st := s.cfg.Updater.Since(0)
		out["source"], out["state"] = src, st
	}
	writeJSON(w, http.StatusOK, out)
}

// busyWork lists work an update restart would cut off.
func (s *server) busyWork() map[string]int {
	b := map[string]int{"chats": 0, "tasks": 0}
	if s.cfg.Chat != nil {
		b["chats"] = s.cfg.Chat.ActiveTurns()
	}
	if s.cfg.Tasks != nil {
		b["tasks"] = s.cfg.Tasks.Running()
	}
	return b
}

func (s *server) startUpdate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Test  bool `json:"test"`
		Force bool `json:"force"`
	}
	if !decode(w, r, &in) {
		return
	}
	if b := s.busyWork(); !in.Force && (b["chats"] > 0 || b["tasks"] > 0) {
		writeJSON(w, http.StatusConflict, map[string]any{"code": "busy", "busy": b,
			"error": fmt.Sprintf("Đang có %d lượt chat và %d Việc chạy; khởi động lại sẽ cắt ngang chúng", b["chats"], b["tasks"])})
		return
	}
	if err := s.cfg.Updater.Start(selfupdate.Options{Test: in.Test}); err != nil {
		if errors.Is(err, selfupdate.ErrBusy) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		s.internal(w, r, err)
		return
	}
	s.auditAction(r, "system.update", "", map[string]any{"test": in.Test, "force": in.Force})
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

// streamUpdate streams the build output and state as SSE (`lines`, `state`).
func (s *server) streamUpdate(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	seq := 0
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	var last []byte
	for {
		lines, wake, st := s.cfg.Updater.Since(seq)
		if len(lines) > 0 {
			raw, _ := json.Marshal(lines)
			seq = lines[len(lines)-1].Seq
			fmt.Fprintf(w, "event: lines\ndata: %s\n\n", raw)
		}
		if raw, _ := json.Marshal(st); string(raw) != string(last) {
			last = raw
			fmt.Fprintf(w, "event: state\ndata: %s\n\n", raw)
		}
		flusher.Flush()
		select {
		case <-wake:
		case <-tick.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
