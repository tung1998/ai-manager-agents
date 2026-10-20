package transfer

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/peer"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/team"
)

// What a project pack may carry (ADR-170). Code goes by git only.
const (
	IncludeConfig = "config" // agents, their permissions, workflows
	IncludeChats  = "chats"  // the project's chats and their messages
	IncludeMemory = "memory" // the agents' notes
)

// Includes are every part, in order.
var Includes = []string{IncludeConfig, IncludeChats, IncludeMemory}

// PackFile is the pack's one entry in its zip.
const PackFile = "project.json"

// MaxPack bounds a pack read from a file, a link or another office.
const MaxPack = 64 << 20

// Pack is one project carried to another office (ADR-170): its config and,
// as chosen, its chats and its agents' notes. Never a local path or a secret.
type Pack struct {
	Version    int          `json:"version"`
	ExportedAt time.Time    `json:"exported_at"`
	Include    []string     `json:"include"`
	Project    ProjectSpec  `json:"project"`
	Chats      []PackChat   `json:"chats,omitempty"`
	Memories   []PackMemory `json:"memories,omitempty"`
}

// PackChat is a chat; agents are named by key.
type PackChat struct {
	Title     string        `json:"title,omitempty"`
	Agent     string        `json:"agent,omitempty"`
	CreatedBy string        `json:"created_by,omitempty"`
	CreatedAt time.Time     `json:"created_at"`
	Messages  []PackMessage `json:"messages"`
}

// PackMessage is a chat's message (text only, no tools or attachments).
type PackMessage struct {
	Role      string    `json:"role"`
	Author    string    `json:"author,omitempty"`
	Agent     string    `json:"agent,omitempty"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// PackMemory is an agent's note.
type PackMemory struct {
	Agent   string `json:"agent"`
	Text    string `json:"text"`
	Source  string `json:"source,omitempty"`
	Topic   string `json:"topic,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// CleanIncludes keeps the known parts, in order; none: config only.
func CleanIncludes(in []string) []string {
	out := []string{}
	for _, x := range Includes {
		if slices.ContainsFunc(in, func(y string) bool { return strings.EqualFold(strings.TrimSpace(y), x) }) {
			out = append(out, x)
		}
	}
	if len(out) == 0 {
		out = []string{IncludeConfig}
	}
	return out
}

// ExportPack packs project repoID with include; keep, when not nil, picks
// the chats that go.
func (s *Service) ExportPack(ctx context.Context, repoID string, include []string, keep func(storage.Conversation) bool) (Pack, error) {
	r, err := s.store.Repos().Get(ctx, repoID)
	if err != nil {
		return Pack{}, err
	}
	include = CleanIncludes(include)
	names, err := s.connectionNames(ctx)
	if err != nil {
		return Pack{}, err
	}
	spec, err := s.projectSpec(ctx, r, names)
	if err != nil {
		return Pack{}, err
	}
	spec.Path, spec.GitRemote = "", peer.PublicRemote(spec.GitRemote) // the zip may travel
	if !slices.Contains(include, IncludeConfig) {
		spec.Agents, spec.Workflows = nil, nil
	}
	p := Pack{Version: Version, ExportedAt: time.Now().UTC(), Include: include, Project: spec}
	agents, err := s.store.Agents().List(ctx, r.ID)
	if err != nil {
		return p, err
	}
	keyOf := map[string]string{}
	for _, a := range agents {
		keyOf[a.ID] = a.Key
	}
	if slices.Contains(include, IncludeChats) {
		convs, err := s.store.Chat().ListConversationsFrom(ctx, r.ID, "all", 100000)
		if err != nil {
			return p, err
		}
		for _, c := range convs {
			if keep != nil && !keep(c) {
				continue
			}
			msgs, err := s.store.Chat().ListMessages(ctx, c.ID)
			if err != nil {
				return p, err
			}
			pc := PackChat{Title: c.Title, Agent: keyOf[c.AgentID], CreatedBy: c.CreatedBy, CreatedAt: c.CreatedAt, Messages: []PackMessage{}}
			for _, m := range msgs {
				pc.Messages = append(pc.Messages, PackMessage{Role: m.Role, Author: m.Author, Agent: keyOf[m.AgentID], Content: m.Content, CreatedAt: m.CreatedAt})
			}
			p.Chats = append(p.Chats, pc)
		}
	}
	if slices.Contains(include, IncludeMemory) {
		for _, a := range agents {
			notes, err := s.store.Memories().List(ctx, r.ID, a.ID)
			if err != nil {
				return p, err
			}
			for _, m := range notes {
				p.Memories = append(p.Memories, PackMemory{Agent: a.Key, Text: m.Text, Source: m.Source, Topic: m.Topic, Summary: m.Summary})
			}
		}
	}
	return p, nil
}

// WriteZip writes p as a zip holding PackFile.
func WriteZip(w io.Writer, p Pack) error {
	z := zip.NewWriter(w)
	f, err := z.Create(PackFile)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(p); err != nil {
		return err
	}
	return z.Close()
}

// ReadZip reads a pack from a zip holding PackFile.
func ReadZip(data []byte) (Pack, error) {
	bad := errors.New("file không phải gói project của office (.zip có " + PackFile + ")")
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return Pack{}, bad
	}
	for _, f := range z.File {
		if f.Name != PackFile {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return Pack{}, bad
		}
		defer rc.Close()
		var p Pack
		if err := json.NewDecoder(io.LimitReader(rc, 4*MaxPack)).Decode(&p); err != nil {
			return Pack{}, bad
		}
		if p.Version != Version && p.Version != 1 {
			return Pack{}, fmt.Errorf("không hỗ trợ gói project version %d", p.Version)
		}
		if strings.TrimSpace(p.Project.Name) == "" {
			return Pack{}, errors.New("gói project không có tên project")
		}
		p.Include = CleanIncludes(p.Include)
		return p, nil
	}
	return Pack{}, bad
}

// ImportPack puts what include picks of p into project r: config replaces
// its agents and workflows; chats and notes are added (a note already there
// is not added twice).
func (s *Service) ImportPack(ctx context.Context, p Pack, r storage.Repo, include []string) (Result, error) {
	include = slices.DeleteFunc(CleanIncludes(include), func(x string) bool { return !slices.Contains(p.Include, x) })
	res := Result{Changes: []Change{}}
	if slices.Contains(include, IncludeConfig) && (p.Project.Agents != nil || len(p.Project.Workflows) > 0) {
		spec := p.Project
		spec.Name, spec.Path, spec.GitRemote = r.Name, r.Path, r.GitRemote
		spec.Description = orStr(r.Description, spec.Description)
		got, err := s.Import(ctx, Bundle{Version: Version, Projects: []ProjectSpec{spec}}, false)
		if err != nil {
			return res, err
		}
		res.Changes = append(res.Changes, got.Changes...)
	}
	agents, err := s.store.Agents().List(ctx, r.ID)
	if err != nil {
		return res, err
	}
	if len(agents) == 0 && s.team != nil { // a project always has an agent
		if err := s.team.AddFromLibrary(ctx, r.ID, team.StarterAgent); err != nil {
			return res, err
		}
		if agents, err = s.store.Agents().List(ctx, r.ID); err != nil {
			return res, err
		}
	}
	byKey := map[string]storage.Agent{}
	for _, a := range agents {
		byKey[a.Key] = a
	}
	if slices.Contains(include, IncludeChats) {
		for _, pc := range p.Chats {
			a := byKey[pc.Agent]
			c, err := s.store.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: r.ID, AgentID: a.ID, AgentName: a.Name,
				Title: pc.Title, CreatedBy: pc.CreatedBy})
			if err != nil {
				return res, err
			}
			for _, m := range pc.Messages {
				if m.Role != "user" && m.Role != "assistant" && m.Role != "error" {
					continue
				}
				if _, err := s.store.Chat().AddMessage(ctx, storage.Message{ConversationID: c.ID, Role: m.Role, Author: m.Author,
					AgentID: byKey[m.Agent].ID, Content: m.Content, CreatedAt: m.CreatedAt}); err != nil {
					return res, err
				}
			}
		}
		res.Changes = append(res.Changes, Change{Kind: "chat", Name: fmt.Sprintf("%d chat", len(p.Chats)), Op: "create"})
	}
	if slices.Contains(include, IncludeMemory) {
		added := 0
		have := map[string][]storage.Memory{}
		for _, m := range p.Memories {
			a, ok := byKey[m.Agent]
			if !ok || strings.TrimSpace(m.Text) == "" {
				continue
			}
			if _, ok := have[a.ID]; !ok {
				if have[a.ID], err = s.store.Memories().List(ctx, r.ID, a.ID); err != nil {
					return res, err
				}
			}
			if slices.ContainsFunc(have[a.ID], func(x storage.Memory) bool { return x.Text == m.Text && x.Topic == m.Topic }) {
				continue
			}
			nm, err := s.store.Memories().Create(ctx, storage.Memory{ProjectID: r.ID, AgentID: a.ID, Text: m.Text,
				Source: orStr(m.Source, "person"), Topic: m.Topic, Summary: m.Summary, CreatedBy: "import"})
			if err != nil {
				return res, err
			}
			have[a.ID] = append(have[a.ID], nm)
			added++
		}
		res.Changes = append(res.Changes, Change{Kind: "memory", Name: fmt.Sprintf("%d ghi nhớ", added), Op: "create"})
	}
	return res, nil
}

func orStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
