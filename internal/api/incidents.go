package api

import (
	"fmt"
	"net/http"
	"sort"
	"time"

	"bitbucket.org/senprints/agent-office/internal/assistant"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// incident is something across the office that needs a person, with where to
// fix it: a monitor down, a process crashed, an automation office turned off,
// a bot that lost its connection, tasks and runs that failed, a card waiting.
type incident struct {
	Kind        string    `json:"kind"`     // monitor | process | automation | bot | jobs | approval
	Severity    string    `json:"severity"` // error | warning
	ProjectID   string    `json:"project_id"`
	ProjectName string    `json:"project_name"`
	Title       string    `json:"title"`
	Detail      string    `json:"detail"`
	At          time.Time `json:"at"`
	Link        string    `json:"link"`
}

func (s *server) incidents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projects, err := s.cfg.Store.Repos().List(ctx)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	hidden := assistant.ID(ctx, s.cfg.Store)
	day := time.Now().UTC().Add(-24 * time.Hour)
	out := []incident{}
	for _, p := range projects {
		if p.ID == hidden {
			continue
		}
		base := "/projects/" + p.ID
		add := func(kind, sev, title, detail string, at *time.Time, link string) {
			it := incident{Kind: kind, Severity: sev, ProjectID: p.ID, ProjectName: p.Name, Title: title, Detail: detail, Link: link}
			if at != nil {
				it.At = *at
			}
			out = append(out, it)
		}
		if ms, err := s.cfg.Store.Monitors().List(ctx, p.ID); err == nil {
			for _, m := range ms {
				if m.Enabled && m.Status == "down" {
					add("monitor", "error", m.Name, m.LastMessage, m.LastChangeAt, base+"?tab=ops")
				}
			}
		}
		if s.cfg.Ops != nil {
			if ps, err := s.cfg.Store.Processes().List(ctx, p.ID); err == nil {
				for _, x := range ps {
					if st := s.cfg.Ops.State(x.ID); st.Status == "crashed" {
						add("process", "error", x.Name, fmt.Sprintf("thoát, đã khởi động lại %d lần", st.Restarts), st.FinishedAt, base+"?tab=ops")
					}
				}
			}
		}
		if as, err := s.cfg.Store.Automations().List(ctx, p.ID); err == nil {
			for _, a := range as {
				if a.DisabledCode != "" {
					add("automation", "warning", a.Name, a.DisabledReason, &a.UpdatedAt, base+"/automations/"+a.ID)
				}
			}
		}
		if cs, err := s.cfg.Store.Channels().List(ctx, p.ID); err == nil {
			for _, c := range cs {
				if c.Enabled && c.LastError != "" {
					add("bot", "error", firstNonEmptyStr(c.BotName, c.Name), c.LastError, &c.UpdatedAt, base+"/bots/"+c.ID)
				}
			}
		}
		if failed, err := s.cfg.Store.Jobs().List(ctx, storage.JobFilter{ProjectID: p.ID, Status: "failed", Since: day, Limit: 200}); err == nil && len(failed) > 0 {
			last := failed[0]
			add("jobs", "warning", fmt.Sprintf("%d job lỗi trong 24 giờ", len(failed)), last.Title+": "+last.Error, &last.CreatedAt, "/jobs?project="+p.ID)
		}
	}
	if userFrom(r).Role == storage.RoleAdmin { // cards waiting for a person
		if acts, err := s.cfg.Store.Actions().Pending(ctx, 100); err == nil {
			for _, a := range acts {
				name := a.ProjectID
				for _, p := range projects {
					if p.ID == a.ProjectID {
						name = p.Name
					}
				}
				link := "/projects/" + a.ProjectID + "?tab=chat"
				if a.ConversationID != "" {
					link += "&c=" + a.ConversationID
				} else {
					link = "/assistant"
				}
				out = append(out, incident{Kind: "approval", Severity: "warning", ProjectID: a.ProjectID, ProjectName: name,
					Title: firstNonEmptyStr(a.Target, a.Kind), Detail: a.Reason, At: a.CreatedAt, Link: link})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].Severity == "error") != (out[j].Severity == "error") {
			return out[i].Severity == "error"
		}
		return out[i].At.After(out[j].At)
	})
	writeJSON(w, http.StatusOK, map[string]any{"incidents": out, "count": len(out)})
}
