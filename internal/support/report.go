package support

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/collections"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Report is how support went over some days (ADR-174).
type Report struct {
	Days     int            `json:"days"`
	Since    time.Time      `json:"since"`
	Tickets  int            `json:"tickets"`
	ByStatus map[string]int `json:"by_status"`
	ByCase   map[string]int `json:"by_case"`
	Answered int            `json:"answered"` // tickets with a first answer
	// FirstResponseMinutes: the average wait for the first answer
	FirstResponseMinutes float64 `json:"first_response_minutes"`
	SLABreached          int     `json:"sla_breached"`
	Escalated            int     `json:"escalated"` // handed to a person
	// replies: sent by the AI on its own, approved as written, edited by a
	// person first, written by a person; drafts the AI could not write
	AIAuto     int `json:"ai_auto"`
	AIApproved int `json:"ai_approved"`
	AIEdited   int `json:"ai_edited"`
	ByPerson   int `json:"by_person"`
	AIFailed   int `json:"ai_failed"`
	// Unanswered: the latest customer messages with no reply (a playbook missing?)
	Unanswered []string `json:"unanswered"`
}

// Report sums up the tickets made in the last days (default 7).
func (s *Service) Report(ctx context.Context, projectID string, days int) (Report, error) {
	if days <= 0 || days > 365 {
		days = 7
	}
	since := time.Now().UTC().AddDate(0, 0, -days)
	r := Report{Days: days, Since: since, ByStatus: map[string]int{}, ByCase: map[string]int{}, Unanswered: []string{}}
	list, err := s.store.Support().TicketsSince(ctx, projectID, since)
	if err != nil {
		return r, err
	}
	var wait time.Duration
	for _, t := range list {
		r.Tickets++
		r.ByStatus[t.Status]++
		r.ByCase[firstNonEmpty(t.CaseType, "-")]++
		if t.SLABreached {
			r.SLABreached++
		}
		if contains(t.Tags, "human") {
			r.Escalated++
		}
		if t.FirstResponseAt != nil {
			r.Answered++
			wait += t.FirstResponseAt.Sub(t.CreatedAt)
		}
		evs, err := s.store.Support().Events(ctx, t.ID)
		if err != nil {
			return r, err
		}
		drafts := map[string]storage.SupportEvent{}
		lastCustomer, replied := "", false
		for _, e := range evs {
			switch {
			case e.Kind == "ai_draft":
				drafts[e.ID] = e
				if e.State == DraftFailed {
					r.AIFailed++
				}
			case e.Kind == "customer_msg":
				lastCustomer, replied = e.Body, false
			case e.Kind == "sent":
				replied = true
				d, fromDraft := drafts[str(e.Meta["draft"])]
				switch {
				case !fromDraft:
					r.ByPerson++
				case d.Meta["edited"] == true:
					r.AIEdited++
				case strings.HasPrefix(e.Actor, "human:") || (e.Actor != "agent" && e.Actor != ""):
					r.AIApproved++
				default:
					r.AIAuto++
				}
			}
		}
		if lastCustomer != "" && !replied && len(r.Unanswered) < 20 {
			r.Unanswered = append(r.Unanswered, cut(lastCustomer, 160))
		}
	}
	if r.Answered > 0 {
		r.FirstResponseMinutes = wait.Minutes() / float64(r.Answered)
	}
	return r, nil
}

// Playbooks from files and links (ADR-174): a playbook whose source is a
// text file of the project folder or a web page keeps that text in its
// content, so it is searched; fetched again when it changed. PDF/DOCX are
// not read: their text goes in content by hand.
var textFile = regexp.MustCompile(`(?i)\.(md|markdown|txt|csv|json|html?|ya?ml)$`)

const maxPlaybookBytes = 512 << 10

// SyncPlaybooks refreshes the content of the project's file and link
// playbooks; it reports how many changed.
func (s *Service) SyncPlaybooks(ctx context.Context, projectID string) (int, error) {
	p, err := s.store.Repos().Get(ctx, projectID)
	if err != nil {
		return 0, err
	}
	n := 0
	for offset := 0; ; offset += 200 {
		list, total, err := s.collections.Query(ctx, projectID, "playbooks", collections.Query{Limit: 200, Offset: offset})
		if err != nil {
			return n, err
		}
		for _, x := range list {
			text, err := s.playbookText(ctx, p, x)
			if err != nil || text == "" {
				continue
			}
			sum := sha256.Sum256([]byte(text))
			hash := hex.EncodeToString(sum[:8])
			if str(x.Data["synced_hash"]) == hash {
				continue
			}
			if _, _, err := s.collections.Upsert(ctx, projectID, "playbooks", x.ID, map[string]any{"content": text, "synced_hash": hash}, "support:sync"); err == nil {
				n++
			}
		}
		if offset+200 >= total {
			return n, nil
		}
	}
}

var tags = regexp.MustCompile(`(?s)<script.*?</script>|<style.*?</style>|<[^>]+>`)

func (s *Service) playbookText(ctx context.Context, p storage.Repo, x storage.Record) (string, error) {
	src := str(x.Data["source"])
	switch str(x.Data["kind"]) {
	case "link":
		if !strings.HasPrefix(src, "http://") && !strings.HasPrefix(src, "https://") {
			return "", nil
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
		if err != nil {
			return "", err
		}
		resp, err := s.client.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return "", errors.New(resp.Status)
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, maxPlaybookBytes))
		if err != nil {
			return "", err
		}
		text := string(b)
		if strings.Contains(resp.Header.Get("Content-Type"), "html") {
			text = tags.ReplaceAllString(text, " ")
		}
		return strings.Join(strings.Fields(text), " "), nil
	case "doc", "file":
		if p.Path == "" || src == "" || !textFile.MatchString(src) {
			return "", nil
		}
		full := filepath.Join(p.Path, filepath.Clean("/"+src)) // inside the project folder only
		info, err := os.Stat(full)
		if err != nil || info.Size() > maxPlaybookBytes {
			return "", err
		}
		b, err := os.ReadFile(full)
		return strings.TrimSpace(string(b)), err
	}
	return "", nil
}
