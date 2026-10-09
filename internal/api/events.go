package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"bitbucket.org/senprints/agent-office/internal/events"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// events streams what changed (ADR-072) to an open dashboard page: the
// tables written, so it refreshes what it shows, and the data the server
// pushes as it changes (the machine's figures, ADR-078). SSE, one per tab;
// its first event says its id, for the topics it turns on.
func (s *server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok || s.cfg.Events == nil {
		writeError(w, http.StatusNotImplemented, "không có luồng sự kiện")
		return
	}
	u := userFrom(r)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	h.Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	sub, stop := s.cfg.Events.Subscribe(events.Viewer{UserID: u.ID, Email: u.Email, Admin: u.Role == storage.RoleAdmin})
	defer stop()
	write := func(name string, data any) {
		raw, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, raw)
	}
	write("hello", map[string]any{"sid": sub.ID})
	flusher.Flush()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
		case ev := <-sub.C:
			write(ev.Name, ev.Data)
		}
		flusher.Flush()
	}
}

// eventTopic turns a topic on or off for the caller's open page (the Máy
// tab's full figures: "machine").
func (s *server) eventTopic(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SID   int    `json:"sid"`
		Topic string `json:"topic"`
		On    bool   `json:"on"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Topic != "machine" || s.cfg.Events == nil || !s.cfg.Events.SetTopic(in.SID, userFrom(r).ID, in.Topic, in.On) {
		writeError(w, http.StatusNotFound, "Không có luồng sự kiện này.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// eventTurn follows an answer being written on the caller's open page's
// stream ({sid, turn, from, on}), as "turn" events {turn, events}, instead
// of a stream of its own: a tab keeps one connection to office however many
// answers it follows (ADR-127). from: the first event wanted (after a
// reconnect: the one after the last seen). 404: the answer has ended (the
// page reloads the thread) or the stream is gone.
func (s *server) eventTurn(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SID  int    `json:"sid"`
		Turn string `json:"turn"`
		From int    `json:"from"`
		On   bool   `json:"on"`
	}
	if !decode(w, r, &in) {
		return
	}
	key := fmt.Sprintf("%d:%s", in.SID, in.Turn)
	if old, ok := s.turnFollows.LoadAndDelete(key); ok {
		close(old.(chan struct{}))
	}
	if !in.On {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	turn, ok := s.cfg.Chat.Turn(in.Turn)
	if !ok || !s.mayOpen(r, turn.ConversationID) {
		writeError(w, http.StatusNotFound, "Lượt trả lời đã kết thúc hoặc không tồn tại")
		return
	}
	var sub *events.Sub
	if s.cfg.Events != nil {
		sub, ok = s.cfg.Events.Get(in.SID, userFrom(r).ID)
	}
	if sub == nil || !ok {
		writeError(w, http.StatusNotFound, "Không có luồng sự kiện này.")
		return
	}
	stop := make(chan struct{})
	if prev, loaded := s.turnFollows.Swap(key, stop); loaded { // a second ask at once: the last wins
		close(prev.(chan struct{}))
	}
	go func() {
		defer s.turnFollows.CompareAndDelete(key, stop)
		seq := in.From
		for {
			evs, done, wake := turn.Since(seq)
			if len(evs) > 0 {
				if !s.cfg.Events.SendWait(sub, events.Event{Name: "turn", Data: map[string]any{"turn": in.Turn, "events": evs}}, stop) {
					return
				}
				seq = evs[len(evs)-1].Seq + 1
			}
			if done {
				return
			}
			select {
			case <-wake:
			case <-stop:
				return
			case <-sub.Gone():
				return
			}
		}
	}()
	w.WriteHeader(http.StatusNoContent)
}
