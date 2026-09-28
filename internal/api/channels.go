package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/audit"
	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// ChannelReloader restarts a channel's bot after its settings change.
type ChannelReloader interface{ Reload(id string) }

type channelDTO struct {
	ID            string     `json:"id"`
	ProjectID     string     `json:"project_id"`
	Kind          string     `json:"kind"`
	Name          string     `json:"name"`
	HasToken      bool       `json:"has_token"`
	AgentID       string     `json:"agent_id"`
	Mode          string     `json:"mode"`
	Enabled       bool       `json:"enabled"`
	Allow         []string   `json:"allow"`
	Scope         string     `json:"scope"`
	FilterEnabled bool       `json:"filter_enabled"`
	Refusal       string     `json:"refusal"`
	BotName       string     `json:"bot_name"`
	LastError     string     `json:"last_error"`
	LastMessageAt *time.Time `json:"last_message_at"`
}

func toChannelDTO(c storage.Channel) channelDTO {
	allow := c.Allow
	if allow == nil {
		allow = []string{}
	}
	return channelDTO{c.ID, c.ProjectID, c.Kind, c.Name, c.TokenEnc != "", c.AgentID, c.Mode, c.Enabled, allow, c.Scope, c.FilterEnabled, c.Refusal,
		c.BotName, c.LastError, c.LastMessageAt}
}

type channelInput struct {
	Kind          string    `json:"kind"`
	Name          *string   `json:"name"`
	Token         *string   `json:"token"` // write only
	AgentID       *string   `json:"agent_id"`
	Mode          *string   `json:"mode"`
	Enabled       *bool     `json:"enabled"`
	Allow         *[]string `json:"allow"`
	Scope         *string   `json:"scope"`
	FilterEnabled *bool     `json:"filter_enabled"`
	Refusal       *string   `json:"refusal"`
}

func (s *server) applyChannel(in channelInput, c *storage.Channel) error {
	if in.Name != nil {
		c.Name = strings.TrimSpace(*in.Name)
	}
	if in.Token != nil && strings.TrimSpace(*in.Token) != "" {
		if s.cfg.Providers == nil {
			return errors.New("office không lưu được bí mật")
		}
		enc, err := s.cfg.Providers.Box().Seal(strings.TrimSpace(*in.Token))
		if err != nil {
			return err
		}
		c.TokenEnc = enc
	}
	if in.AgentID != nil {
		c.AgentID = *in.AgentID
	}
	if in.Mode != nil {
		if !perm.Valid(*in.Mode) {
			return errors.New("mức quyền không hợp lệ")
		}
		c.Mode = *in.Mode
	}
	if in.Enabled != nil {
		c.Enabled = *in.Enabled
	}
	if in.Allow != nil {
		c.Allow = []string{}
		for _, a := range *in.Allow {
			if a = strings.TrimSpace(a); a != "" {
				c.Allow = append(c.Allow, a)
			}
		}
	}
	if in.Scope != nil {
		c.Scope = strings.TrimSpace(*in.Scope)
	}
	if in.FilterEnabled != nil {
		c.FilterEnabled = *in.FilterEnabled
	}
	if in.Refusal != nil {
		c.Refusal = strings.TrimSpace(*in.Refusal)
	}
	if c.Name == "" {
		return errors.New("hãy đặt tên cho kênh")
	}
	if c.TokenEnc == "" {
		return errors.New("cần token của bot")
	}
	return nil
}

func (s *server) listChannels(w http.ResponseWriter, r *http.Request) {
	list, err := s.cfg.Store.Channels().List(r.Context(), r.PathValue("id"))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	out := make([]channelDTO, 0, len(list))
	for _, c := range list {
		out = append(out, toChannelDTO(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": out})
}

func (s *server) createChannel(w http.ResponseWriter, r *http.Request) {
	var in channelInput
	if !decode(w, r, &in) {
		return
	}
	if in.Kind != "telegram" && in.Kind != "discord" {
		writeError(w, http.StatusBadRequest, "loại kênh phải là telegram hoặc discord")
		return
	}
	if _, err := s.cfg.Store.Repos().Get(r.Context(), r.PathValue("id")); err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	c := storage.Channel{ProjectID: r.PathValue("id"), Kind: in.Kind, Mode: perm.Read, Enabled: true}
	if err := s.applyChannel(in, &c); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	c, err := s.cfg.Store.Channels().Create(r.Context(), c)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.audit(r, audit.Change{Action: "channel.create", ResourceID: c.ID, ProjectID: c.ProjectID, After: toChannelDTO(c)})
	if s.cfg.Channels != nil {
		s.cfg.Channels.Reload(c.ID)
	}
	writeJSON(w, http.StatusCreated, map[string]any{"channel": toChannelDTO(c)})
}

func (s *server) updateChannel(w http.ResponseWriter, r *http.Request) {
	var in channelInput
	if !decode(w, r, &in) {
		return
	}
	c, err := s.cfg.Store.Channels().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	old := c
	if err := s.applyChannel(in, &c); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.cfg.Store.Channels().Update(r.Context(), c); err != nil {
		s.internal(w, r, err)
		return
	}
	s.audit(r, audit.Change{Action: "channel.update", ResourceID: c.ID, ProjectID: c.ProjectID, Before: toChannelDTO(old), After: toChannelDTO(c),
		Detail: map[string]any{"token_changed": in.Token != nil && *in.Token != ""}})
	if s.cfg.Channels != nil {
		s.cfg.Channels.Reload(c.ID)
	}
	c, _ = s.cfg.Store.Channels().Get(r.Context(), c.ID)
	writeJSON(w, http.StatusOK, map[string]any{"channel": toChannelDTO(c)})
}

func (s *server) deleteChannel(w http.ResponseWriter, r *http.Request) {
	c, err := s.cfg.Store.Channels().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	if err := s.cfg.Store.Channels().Delete(r.Context(), c.ID); err != nil {
		s.internal(w, r, err)
		return
	}
	s.audit(r, audit.Change{Action: "channel.delete", ResourceID: c.ID, ProjectID: c.ProjectID, Before: toChannelDTO(c)})
	if s.cfg.Channels != nil {
		s.cfg.Channels.Reload(c.ID) // stops it
	}
	w.WriteHeader(http.StatusNoContent)
}
