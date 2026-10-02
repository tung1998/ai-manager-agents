package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/audit"
	"bitbucket.org/senprints/agent-office/internal/events"
	"bitbucket.org/senprints/agent-office/internal/mcpgateway"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// The office's MCP servers behind its gateway (ADR-091).
func (s *server) mcpServerRoutes(mux *http.ServeMux, admin func(http.HandlerFunc) http.Handler) {
	mux.Handle("/mcp/s/{name}", s.cfg.Gateway) // authenticated by the run's token, like /mcp
	mux.Handle("GET /api/mcp/servers", admin(s.listMCPServers))
	mux.Handle("POST /api/mcp/servers", admin(s.createMCPServer))
	mux.Handle("PATCH /api/mcp/servers/{id}", admin(s.updateMCPServer))
	mux.Handle("DELETE /api/mcp/servers/{id}", admin(s.deleteMCPServer))
	mux.Handle("POST /api/mcp/servers/{id}/check", admin(s.checkMCPServer))
}

type mcpServerDTO struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Kind            string            `json:"kind"`
	URL             string            `json:"url"`
	Headers         map[string]string `json:"headers"` // values masked
	Scope           string            `json:"scope"`
	Origin          string            `json:"origin"`
	Enabled         bool              `json:"enabled"`
	Path            string            `json:"path"` // its address on office
	LastCheckAt     *time.Time        `json:"last_check_at"`
	LastCheckStatus string            `json:"last_check_status"`
	LastCheckError  string            `json:"last_check_error"`
	Tools           []storage.MCPTool `json:"tools"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

// mcpServerDTO never carries a secret: header values come masked.
func (s *server) mcpServerDTO(m storage.MCPServer) mcpServerDTO {
	headers, err := mcpgateway.OpenMap(s.cfg.Providers.Box(), m.HeadersEnc)
	if err != nil {
		headers = map[string]string{}
	}
	return mcpServerDTO{ID: m.ID, Name: m.Name, Kind: m.Kind, URL: m.URL, Headers: mcpgateway.MaskMap(headers), Scope: m.Scope, Origin: m.Origin,
		Enabled: m.Enabled, Path: "/mcp/s/" + m.Name, LastCheckAt: m.LastCheckAt, LastCheckStatus: m.LastCheckStatus, LastCheckError: m.LastCheckError,
		Tools: m.LastTools, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
}

// auditMCP is what the change log keeps of a server (masked, no check results).
func (s *server) auditMCP(m storage.MCPServer) map[string]any {
	d := s.mcpServerDTO(m)
	return map[string]any{"name": d.Name, "kind": d.Kind, "url": d.URL, "headers": d.Headers, "scope": d.Scope, "origin": d.Origin, "enabled": d.Enabled}
}

type mcpServerInput struct {
	Name    *string            `json:"name"`
	Kind    string             `json:"kind"`
	URL     *string            `json:"url"`
	Headers *map[string]string `json:"headers"` // write only; an empty value keeps the stored one
	Scope   *string            `json:"scope"`
	Enabled *bool              `json:"enabled"`
}

func (s *server) applyMCPServer(in mcpServerInput, m *storage.MCPServer) error {
	if in.Name != nil {
		m.Name = strings.TrimSpace(*in.Name)
	}
	if !mcpgateway.ValidName(m.Name) {
		return errors.New(`tên chỉ gồm chữ thường, số và "-" (vd context7), không quá 40 ký tự, không dùng "office"`)
	}
	if in.Scope != nil {
		m.Scope = strings.TrimSpace(*in.Scope)
	}
	if m.Scope != "machine" { // GĐ1: machine-wide only
		return errors.New("phạm vi hiện chỉ có machine (toàn máy)")
	}
	if in.URL != nil {
		m.URL = strings.TrimSpace(*in.URL)
	}
	if u, err := url.Parse(m.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("URL không hợp lệ: cần dạng https://…")
	}
	if in.Headers != nil {
		old, err := mcpgateway.OpenMap(s.cfg.Providers.Box(), m.HeadersEnc)
		if err != nil {
			old = map[string]string{}
		}
		enc, err := mcpgateway.SealMap(s.cfg.Providers.Box(), mcpgateway.MergeHeaders(old, *in.Headers))
		if err != nil {
			return err
		}
		m.HeadersEnc = enc
	}
	if in.Enabled != nil {
		m.Enabled = *in.Enabled
	}
	return nil
}

func (s *server) listMCPServers(w http.ResponseWriter, r *http.Request) {
	list, err := s.cfg.Store.MCPServers().List(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	out := make([]mcpServerDTO, 0, len(list))
	for _, m := range list {
		out = append(out, s.mcpServerDTO(m))
	}
	writeJSON(w, http.StatusOK, map[string]any{"servers": out})
}

func (s *server) createMCPServer(w http.ResponseWriter, r *http.Request) {
	var in mcpServerInput
	if !decode(w, r, &in) {
		return
	}
	if in.Kind != "" && in.Kind != "http" {
		writeError(w, http.StatusBadRequest, "office hiện chỉ nối được MCP loại http")
		return
	}
	m := storage.MCPServer{Kind: "http", Scope: "machine", Origin: "manual", Enabled: true}
	if err := s.applyMCPServer(in, &m); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	m, err := s.cfg.Store.MCPServers().Create(r.Context(), m)
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	s.audit(r, audit.Change{Action: "mcp_server.create", ResourceID: m.ID, After: s.auditMCP(m)})
	s.checkMCPLater(m)
	writeJSON(w, http.StatusCreated, map[string]any{"server": s.mcpServerDTO(m)})
}

func (s *server) updateMCPServer(w http.ResponseWriter, r *http.Request) {
	var in mcpServerInput
	if !decode(w, r, &in) {
		return
	}
	m, err := s.cfg.Store.MCPServers().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	if in.Kind != "" && in.Kind != m.Kind {
		writeError(w, http.StatusBadRequest, "không đổi được loại MCP")
		return
	}
	old := m
	if err := s.applyMCPServer(in, &m); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.cfg.Store.MCPServers().Update(r.Context(), m); err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	s.audit(r, audit.Change{Action: "mcp_server.update", ResourceID: m.ID, Before: s.auditMCP(old), After: s.auditMCP(m),
		Detail: map[string]any{"headers_changed": old.HeadersEnc != m.HeadersEnc}})
	if m.Enabled && (old.URL != m.URL || old.HeadersEnc != m.HeadersEnc || !old.Enabled) {
		s.checkMCPLater(m)
	}
	m, _ = s.cfg.Store.MCPServers().Get(r.Context(), m.ID)
	writeJSON(w, http.StatusOK, map[string]any{"server": s.mcpServerDTO(m)})
}

func (s *server) deleteMCPServer(w http.ResponseWriter, r *http.Request) {
	m, err := s.cfg.Store.MCPServers().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	if err := s.cfg.Store.MCPServers().Delete(r.Context(), m.ID); err != nil {
		s.internal(w, r, err)
		return
	}
	s.audit(r, audit.Change{Action: "mcp_server.delete", ResourceID: m.ID, Before: s.auditMCP(m)})
	w.WriteHeader(http.StatusNoContent)
}

// checkMCPServer connects to the server like a client (a few seconds) and
// returns what it found; open pages get it as mcp.gateway.status.
func (s *server) checkMCPServer(w http.ResponseWriter, r *http.Request) {
	m, err := s.cfg.Store.MCPServers().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	if m, err = s.cfg.Gateway.CheckServer(r.Context(), m); err != nil {
		s.internal(w, r, err)
		return
	}
	d := s.mcpServerDTO(m)
	s.sendMCPStatus(d)
	writeJSON(w, http.StatusOK, map[string]any{"server": d})
}

// checkMCPLater checks a server just saved, in the background.
func (s *server) checkMCPLater(m storage.MCPServer) {
	if !m.Enabled {
		return
	}
	go func() {
		m, err := s.cfg.Gateway.CheckServer(context.Background(), m)
		if err != nil {
			s.log.Warn("mcp gateway: check", "server", m.Name, "err", err)
			return
		}
		s.sendMCPStatus(s.mcpServerDTO(m))
	}()
}

func (s *server) sendMCPStatus(d mcpServerDTO) {
	if s.cfg.Events != nil {
		s.cfg.Events.Send(events.Event{Name: "mcp.gateway.status", Data: d}, admins)
	}
}
