package limitalert

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// failures is what a connection's prompts did lately (in memory: a restart
// starts the count again).
type failures struct {
	streak     int         // failed in a row
	at         []time.Time // failures within the window
	toldStreak bool        // this streak was told
	toldUntil  time.Time   // the window rule is quiet until then
	told       bool        // something was told and nothing went through since
}

// Run takes one prompt's result (usage.Service's record of it): failures in
// a row or too many within a while are told in the alert chat once, and the
// connection working again after that is told too. A prompt over budget is
// not the connection failing.
func (a *Alerter) Run(ctx context.Context, r storage.Run) {
	if r.ProviderID == "" || r.Status == "blocked" {
		return
	}
	s := Load(ctx, a.store)
	if s.ChannelID == "" || s.ChatID == "" || a.notify == nil {
		return
	}
	rule := *s.Errors
	now := r.CreatedAt
	if now.IsZero() {
		now = time.Now()
	}
	name := r.ProviderName
	if name == "" {
		name = r.ProviderID
	}

	// Held for the whole read-send-commit sequence (like Check() in
	// limitalert.go), so concurrent Run() calls for the same provider (usage.go
	// records each result in its own goroutine) can't both see an unarmed
	// streak/window and both send, and a success can't interleave with an
	// in-flight failure send and wrongly commit a stale "told" afterwards.
	a.errMu.Lock()
	defer a.errMu.Unlock()
	f := a.errs[r.ProviderID]
	if f == nil {
		f = &failures{}
		a.errs[r.ProviderID] = f
	}
	var text string
	var window time.Duration
	var matchedStreak bool
	isError := r.Status == "error"
	if !isError {
		if f.told {
			text = fmt.Sprintf("✅ %s gửi prompt được lại (%s).", name, orModel(r.Model)) // i18n-ignore
		}
		f.streak, f.toldStreak, f.told = 0, false, false
		if text == "" {
			return
		}
	} else {
		f.streak++
		window = time.Duration(rule.Window) * time.Minute
		keep := f.at[:0]
		for _, t := range f.at {
			if now.Sub(t) < window {
				keep = append(keep, t)
			}
		}
		f.at = append(keep, now)
		reason := oneLine(r.Error, 300)
		switch {
		case rule.Streak > 0 && f.streak >= rule.Streak && !f.toldStreak:
			matchedStreak = true
			text = fmt.Sprintf("🚨 %s: %d lần gửi prompt lỗi liên tiếp (%s). Lỗi gần nhất: %s", name, f.streak, orModel(r.Model), reason) // i18n-ignore
		case rule.Count > 0 && len(f.at) >= rule.Count && now.After(f.toldUntil):
			text = fmt.Sprintf("🚨 %s: %d lần gửi prompt lỗi trong %d phút (%s). Lỗi gần nhất: %s", name, len(f.at), rule.Window, orModel(r.Model), reason) // i18n-ignore
		}
		if text == "" {
			return
		}
	}
	nctx, cancel := context.WithTimeout(ctx, notifyTimeout)
	err := a.notify(nctx, s.ChannelID, s.ChatID, text)
	cancel()
	if err != nil {
		slog.Warn("limitalert: error alert failed", "provider", r.ProviderID, "err", err)
		return
	}
	if isError {
		if matchedStreak {
			f.toldStreak = true
		}
		f.told = true
		f.toldUntil = now.Add(window) // told: quiet for a window, not once per failure
	}
}

func orModel(m string) string {
	if m == "" {
		return "model mặc định" // i18n-ignore
	}
	return m
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
