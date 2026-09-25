package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/llm"
	"bitbucket.org/senprints/agent-office/internal/orgmodel"
	"bitbucket.org/senprints/agent-office/internal/provider"
	"bitbucket.org/senprints/agent-office/internal/repos"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

func (s *server) orgRoutes(mux *http.ServeMux) {
	auth := func(h http.HandlerFunc) http.Handler { return s.requireAuth(h) }
	admin := func(h http.HandlerFunc) http.Handler { return s.requireRole(storage.RoleAdmin, h) }

	mux.Handle("GET /api/system", auth(s.system))

	mux.Handle("GET /api/provider-kinds", auth(s.providerKinds))
	mux.Handle("GET /api/providers", auth(s.listProviders))
	mux.Handle("GET /api/providers/stats", auth(s.providerStats))
	mux.Handle("POST /api/providers", admin(s.createProvider))
	mux.Handle("PATCH /api/providers/{id}", admin(s.updateProvider))
	mux.Handle("DELETE /api/providers/{id}", admin(s.deleteProvider))
	mux.Handle("POST /api/providers/{id}/default", admin(s.defaultProvider))
	mux.Handle("POST /api/providers/{id}/test", admin(s.testProvider))

	mux.Handle("GET /api/templates", auth(s.listTemplates))
	mux.Handle("POST /api/templates", admin(s.createTemplate))
	mux.Handle("POST /api/templates/{key}/reset", admin(s.resetTemplate))
	mux.Handle("GET /api/org-models/{id}", auth(s.getOrgModel))
	mux.Handle("GET /api/org-models/{id}/export", auth(s.exportOrgModel))
	mux.Handle("PATCH /api/org-models/{id}", admin(s.updateOrgModel))
	mux.Handle("DELETE /api/org-models/{id}", admin(s.deleteOrgModel))
	mux.Handle("POST /api/org-models/{id}/agents", admin(s.createAgent))
	mux.Handle("GET /api/org-models/{id}/revisions", auth(s.listRevisions))
	mux.Handle("GET /api/revisions/{id}", auth(s.getRevision))
	mux.Handle("POST /api/revisions/{id}/restore", admin(s.restoreRevision))
	mux.Handle("PATCH /api/agents/{id}", admin(s.updateAgent))
	mux.Handle("DELETE /api/agents/{id}", admin(s.deleteAgent))

	mux.Handle("GET /api/projects", auth(s.listRepos))
	mux.Handle("POST /api/projects", admin(s.createRepo))
	mux.Handle("GET /api/projects/{id}", auth(s.getRepo))
	mux.Handle("PATCH /api/projects/{id}", admin(s.updateRepo))
	mux.Handle("DELETE /api/projects/{id}", admin(s.deleteRepo))
	mux.Handle("POST /api/projects/{id}/model", admin(s.applyRepoModel))

	mux.Handle("GET /api/fs/dirs", admin(s.listDirs))

	if s.cfg.Chat != nil {
		mux.Handle("GET /api/projects/{id}/chat/agents", auth(s.chatAgents))
		mux.Handle("GET /api/projects/{id}/conversations", auth(s.listConversations))
		mux.Handle("POST /api/projects/{id}/conversations", auth(s.createConversation))
		mux.Handle("GET /api/projects/{id}/skills", auth(s.chatSkills))
		mux.Handle("POST /api/projects/{id}/attachments", auth(s.uploadAttachment))
		mux.Handle("GET /api/attachments/{id}", auth(s.getAttachment))
		mux.Handle("GET /api/conversations/{id}", auth(s.getConversation))
		mux.Handle("DELETE /api/conversations/{id}", auth(s.deleteConversation))
		mux.Handle("POST /api/conversations/{id}/messages", auth(s.sendMessage))
		mux.Handle("GET /api/chat/turns/{id}/stream", auth(s.streamTurn))
		mux.Handle("POST /api/chat/turns/{id}/cancel", auth(s.cancelTurn))
		mux.Handle("POST /api/patches/{id}/approve", admin(s.approvePatch))
		mux.Handle("POST /api/patches/{id}/reject", admin(s.rejectPatch))
	}
	if s.cfg.Tasks != nil {
		mux.Handle("GET /api/jobs", auth(s.jobs))
		mux.Handle("GET /api/projects/{id}/tasks", auth(s.listTasks))
		mux.Handle("POST /api/projects/{id}/tasks", auth(s.createTask))
		mux.Handle("GET /api/tasks/{id}", auth(s.getTask))
		mux.Handle("DELETE /api/tasks/{id}", admin(s.deleteTask))
		mux.Handle("GET /api/tasks/{id}/stream", auth(s.streamTask))
		mux.Handle("POST /api/tasks/{id}/cancel", auth(s.cancelTask))
	}
	if s.cfg.Automation != nil {
		s.automationRoutes(mux, admin)
	}
	if s.cfg.Ops != nil {
		s.opsRoutes(mux, auth, admin)
	}
	if s.cfg.Monitors != nil {
		s.monitorRoutes(mux, auth, admin)
	}
	if s.cfg.Actions != nil {
		mux.Handle("POST /api/actions/{id}/approve", admin(s.decideAction(true)))
		mux.Handle("POST /api/actions/{id}/reject", admin(s.decideAction(false)))
	}
	if s.cfg.MCP != nil {
		mux.Handle("/mcp", s.cfg.MCP) // authenticated by its own per-run token
	}
	mux.Handle("GET /api/cli-tools", admin(s.cliTools))
	if s.cfg.CLITools != nil {
		mux.Handle("GET /api/cli-tools/{id}", admin(s.cliTool))
		mux.Handle("POST /api/cli-tools/{id}/install", admin(s.cliInstall))
		mux.Handle("POST /api/cli-tools/{id}/login", admin(s.cliLogin))
		mux.Handle("GET /api/cli-jobs/{id}", admin(s.cliJob))
		mux.Handle("POST /api/cli-jobs/{id}/input", admin(s.cliJobInput))
		mux.Handle("POST /api/cli-jobs/{id}/cancel", admin(s.cliJobCancel))
	}
	if s.cfg.Usage != nil {
		mux.Handle("GET /api/usage/summary", auth(s.usageSummary))
		mux.Handle("GET /api/usage/runs", auth(s.usageRuns))
		mux.Handle("PUT /api/usage/settings", admin(s.usageSettings))
	}
	if s.cfg.Transfer != nil {
		mux.Handle("GET /api/transfer/export", admin(s.transferExport))
		mux.Handle("POST /api/transfer/import", admin(s.transferImport))
	}
	if s.cfg.Backup != nil {
		mux.Handle("POST /api/transfer/backup", admin(s.transferBackup))
	}
	if s.cfg.Setup != nil {
		mux.Handle("POST /api/projects/{id}/setup/scan", admin(s.setupScan))
		mux.Handle("POST /api/projects/{id}/setup/propose", admin(s.setupPropose))
		mux.Handle("POST /api/projects/{id}/setup/build", admin(s.setupBuild))
		mux.Handle("POST /api/projects/{id}/setup/apply", admin(s.setupApply))
	}
}

// writeDomainError maps service errors to HTTP statuses.
func (s *server) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	var ve *orgmodel.ValidationError
	switch {
	case errors.As(err, &ve):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Mô hình không hợp lệ", "problems": ve.Problems})
	case errors.Is(err, storage.ErrNotFound):
		writeError(w, http.StatusNotFound, "Không tìm thấy")
	case errors.Is(err, storage.ErrConflict), errors.Is(err, provider.ErrNameTaken), errors.Is(err, orgmodel.ErrHasInstance):
		writeError(w, http.StatusConflict, conflictMsg(err))
	case errors.Is(err, provider.ErrInvalidKind), errors.Is(err, provider.ErrNeedsKey), errors.Is(err, provider.ErrBadTier),
		errors.Is(err, provider.ErrKeyRequiredForNewURL),
		errors.Is(err, orgmodel.ErrNotTemplate), errors.Is(err, orgmodel.ErrBuiltinReset), errors.Is(err, errBadInput):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		s.internal(w, r, err)
	}
}

var errBadInput = errors.New("dữ liệu không hợp lệ")

func conflictMsg(err error) string {
	switch {
	case errors.Is(err, orgmodel.ErrHasInstance):
		return "Project đã có mô hình, chọn thay thế để áp mô hình mới"
	case errors.Is(err, provider.ErrNameTaken):
		return err.Error()
	}
	return "Dữ liệu bị trùng (key đã tồn tại, hoặc thư mục đã được thêm làm project)"
}

func (s *server) system(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.cfg.System)
}

// ---- providers ----

type providerDTO struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Kind         string            `json:"kind"`
	Preset       string            `json:"preset"`
	BaseURL      string            `json:"base_url"`
	HasAPIKey    bool              `json:"has_api_key"`
	APIKeyHint   string            `json:"api_key_hint"`
	APIKeyEnv    string            `json:"api_key_env"`
	TierModels   map[string]string `json:"tier_models"`
	Models       []string          `json:"models"`
	IsDefault    bool              `json:"is_default"`
	Enabled      bool              `json:"enabled"`
	Status       string            `json:"status"`
	StatusDetail string            `json:"status_detail"`
	CheckedAt    *time.Time        `json:"checked_at"`
}

func toProviderDTO(p storage.Provider) providerDTO {
	return providerDTO{ID: p.ID, Name: p.Name, Kind: string(p.Kind), Preset: p.Preset, BaseURL: p.BaseURL, HasAPIKey: p.APIKeyEnc != "",
		APIKeyHint: p.APIKeyHint, APIKeyEnv: p.APIKeyEnv, TierModels: p.TierModels, Models: p.Models, IsDefault: p.IsDefault,
		Enabled: p.Enabled, Status: p.Status, StatusDetail: p.StatusDetail, CheckedAt: p.CheckedAt}
}

type kindInfo struct {
	Kind        string            `json:"kind"`
	Label       string            `json:"label"`
	Description string            `json:"description"`
	Common      bool              `json:"common"` // shown as a card; the rest go under "Khác"
	NeedsKey    bool              `json:"needs_key"`
	IsCLI       bool              `json:"is_cli"`
	BaseURLHint string            `json:"base_url_hint"`
	TierModels  map[string]string `json:"tier_models"`
	Detected    *detected         `json:"detected,omitempty"`
}

// detected is what this machine already has for a kind.
type detected struct {
	Installed bool   `json:"installed,omitempty"` // CLI found in PATH
	Version   string `json:"version,omitempty"`
	EnvKey    string `json:"env_key,omitempty"` // API key env var that is set (name only)
}

func detectCLI(bin string) *detected {
	path, err := exec.LookPath(bin)
	if err != nil {
		return &detected{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, path, "--version").Output()
	return &detected{Installed: true, Version: strings.TrimSpace(string(out))}
}

// detectTool asks the CLI manager (same lookup as install/login) when enabled.
func (s *server) detectTool(r *http.Request, id string) *detected {
	if s.cfg.CLITools != nil {
		if st, err := s.cfg.CLITools.Status(r.Context(), id); err == nil {
			return &detected{Installed: st.Installed, Version: st.Version}
		}
	}
	return detectCLI(id)
}

func detectEnv(name string) *detected {
	if os.Getenv(name) != "" {
		return &detected{EnvKey: name}
	}
	return &detected{}
}

func (s *server) providerKinds(w http.ResponseWriter, r *http.Request) {
	k := func(kind storage.ProviderKind, label, desc, hint string, needsKey, common bool, d *detected) kindInfo {
		return kindInfo{Kind: string(kind), Label: label, Description: desc, Common: common, NeedsKey: needsKey, IsCLI: kind.IsCLI(),
			BaseURLHint: hint, TierModels: llm.DefaultTierModels(kind), Detected: d}
	}
	writeJSON(w, http.StatusOK, map[string]any{"presets": provider.Presets(), "kinds": []kindInfo{
		k(storage.ProviderClaudeCLI, "Claude Code trên máy",
			"Dùng tài khoản Claude đã đăng nhập trong Claude Code trên máy này (gói Pro/Max). Không cần API key.",
			"claude", false, true, s.detectTool(r, "claude")),
		k(storage.ProviderAnthropic, "Claude API",
			"Trả tiền theo lượng dùng qua API key lấy tại console.anthropic.com.",
			"https://api.anthropic.com", true, true, detectEnv("ANTHROPIC_API_KEY")),
		k(storage.ProviderOpenAI, "OpenAI (GPT) API",
			"Dùng GPT qua API key lấy tại platform.openai.com.",
			"https://api.openai.com/v1", true, true, detectEnv("OPENAI_API_KEY")),
		k(storage.ProviderCodexCLI, "Codex trên máy",
			"Dùng tài khoản ChatGPT đã đăng nhập trong Codex CLI trên máy này.",
			"codex", false, false, s.detectTool(r, "codex")),
		k(storage.ProviderOpenAICompatible, "API tương thích OpenAI",
			"Model chạy local hoặc cổng trung gian: Ollama, LM Studio, vLLM, OpenRouter, LiteLLM…",
			"http://localhost:11434/v1", false, false, nil),
	}})
}

type providerInput struct {
	Name       string            `json:"name"`
	Kind       string            `json:"kind"`
	Preset     *string           `json:"preset"`
	BaseURL    string            `json:"base_url"`
	APIKey     *string           `json:"api_key"`
	APIKeyEnv  string            `json:"api_key_env"`
	TierModels map[string]string `json:"tier_models"`
	Enabled    *bool             `json:"enabled"`
}

func (in providerInput) toInput() provider.Input {
	return provider.Input{Name: in.Name, Kind: storage.ProviderKind(in.Kind), Preset: in.Preset, BaseURL: in.BaseURL, APIKey: in.APIKey,
		APIKeyEnv: in.APIKeyEnv, TierModels: in.TierModels, Enabled: in.Enabled}
}

func (s *server) listProviders(w http.ResponseWriter, r *http.Request) {
	list, err := s.cfg.Store.Providers().List(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	out := make([]providerDTO, 0, len(list))
	for _, p := range list {
		out = append(out, toProviderDTO(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": out})
}

func (s *server) createProvider(w http.ResponseWriter, r *http.Request) {
	var in providerInput
	if !decode(w, r, &in) {
		return
	}
	p, err := s.cfg.Providers.Create(r.Context(), in.toInput())
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	s.auditAction(r, "provider.create", p.ID, map[string]any{"name": p.Name, "kind": string(p.Kind)})
	writeJSON(w, http.StatusCreated, map[string]any{"provider": toProviderDTO(p)})
}

func (s *server) updateProvider(w http.ResponseWriter, r *http.Request) {
	var in providerInput
	if !decode(w, r, &in) {
		return
	}
	p, err := s.cfg.Providers.Update(r.Context(), r.PathValue("id"), in.toInput())
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	s.auditAction(r, "provider.update", p.ID, map[string]any{"name": p.Name, "key_changed": in.APIKey != nil})
	writeJSON(w, http.StatusOK, map[string]any{"provider": toProviderDTO(p)})
}

func (s *server) deleteProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.cfg.Store.Providers().Delete(r.Context(), id); err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	s.auditAction(r, "provider.delete", id, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) defaultProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.cfg.Store.Providers().SetDefault(r.Context(), id); err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	s.auditAction(r, "provider.set_default", id, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) testProvider(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Prompt string `json:"prompt"`
		Model  string `json:"model"`
	}
	if r.ContentLength != 0 && !decode(w, r, &in) {
		return
	}
	res, err := s.cfg.Providers.Test(r.Context(), r.PathValue("id"), strings.TrimSpace(in.Prompt), in.Model)
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	s.auditAction(r, "provider.test", r.PathValue("id"), map[string]any{"ok": res.OK, "prompt": in.Prompt != ""})
	writeJSON(w, http.StatusOK, res)
}

// ---- org models ----

type agentDTO struct {
	ID           string              `json:"id"`
	OrgModelID   string              `json:"org_model_id"`
	Key          string              `json:"key"`
	Name         string              `json:"name"`
	Tier         string              `json:"tier"`
	Role         string              `json:"role"`
	Description  string              `json:"description"`
	ReportsTo    []string            `json:"reports_to"`
	ProviderID   string              `json:"provider_id"`
	ModelTier    string              `json:"model_tier"`
	LLMModel     string              `json:"llm_model"`
	Instructions string              `json:"instructions"`
	Permissions  storage.Permissions `json:"permissions"`
	Sort         int                 `json:"sort"`
}

func toAgentDTO(a storage.Agent) agentDTO {
	rt := a.ReportsTo
	if rt == nil {
		rt = []string{}
	}
	return agentDTO{ID: a.ID, OrgModelID: a.OrgModelID, Key: a.Key, Name: a.Name, Tier: a.Tier, Role: a.Role,
		Description: a.Description, ReportsTo: rt, ProviderID: a.ProviderID, ModelTier: a.ModelTier, LLMModel: a.LLMModel,
		Instructions: a.Instructions, Permissions: a.Permissions, Sort: a.Sort}
}

type orgDTO struct {
	ID               string             `json:"id"`
	RepoID           string             `json:"repo_id"`
	SourceTemplateID string             `json:"source_template_id"`
	Key              string             `json:"key"`
	Name             string             `json:"name"`
	Description      string             `json:"description"`
	Kind             string             `json:"kind"`
	Governance       storage.Governance `json:"governance"`
	Builtin          bool               `json:"builtin"`
	IsTemplate       bool               `json:"is_template"`
	AgentCount       int                `json:"agent_count"`
	Tiers            map[string]int     `json:"tiers"`
	Agents           []agentDTO         `json:"agents,omitempty"`
	UpdatedAt        time.Time          `json:"updated_at"`
}

func toOrgDTO(m storage.OrgModel, agents []storage.Agent, withAgents bool) orgDTO {
	d := orgDTO{ID: m.ID, RepoID: m.RepoID, SourceTemplateID: m.SourceTemplateID, Key: m.Key, Name: m.Name,
		Description: m.Description, Kind: m.Kind, Governance: m.Governance, Builtin: m.Builtin, IsTemplate: m.IsTemplate(),
		AgentCount: len(agents), Tiers: map[string]int{}, UpdatedAt: m.UpdatedAt}
	for _, a := range agents {
		d.Tiers[a.Tier]++
		if withAgents {
			d.Agents = append(d.Agents, toAgentDTO(a))
		}
	}
	if withAgents && d.Agents == nil {
		d.Agents = []agentDTO{}
	}
	return d
}

func (s *server) loadOrg(r *http.Request, id string, withAgents bool) (orgDTO, error) {
	m, err := s.cfg.Store.OrgModels().Get(r.Context(), id)
	if err != nil {
		return orgDTO{}, err
	}
	agents, err := s.cfg.Store.Agents().List(r.Context(), id)
	if err != nil {
		return orgDTO{}, err
	}
	return toOrgDTO(m, agents, withAgents), nil
}

func (s *server) listTemplates(w http.ResponseWriter, r *http.Request) {
	list, err := s.cfg.Store.OrgModels().ListTemplates(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	out := make([]orgDTO, 0, len(list))
	for _, m := range list {
		agents, err := s.cfg.Store.Agents().List(r.Context(), m.ID)
		if err != nil {
			s.internal(w, r, err)
			return
		}
		out = append(out, toOrgDTO(m, agents, false))
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": out})
}

// createTemplate clones an existing model ({source_id, key, name}) or imports
// a full template ({template: {...}}).
func (s *server) createTemplate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SourceID string             `json:"source_id"`
		Key      string             `json:"key"`
		Name     string             `json:"name"`
		Template *orgmodel.Template `json:"template"`
	}
	if !decode(w, r, &in) {
		return
	}
	var (
		m   storage.OrgModel
		err error
	)
	switch {
	case in.Template != nil:
		m, err = s.cfg.Org.CreateTemplate(r.Context(), *in.Template)
	case in.SourceID != "":
		m, err = s.cfg.Org.CloneTemplate(r.Context(), in.SourceID, strings.TrimSpace(in.Key), strings.TrimSpace(in.Name))
	default:
		err = errBadInput
	}
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	s.auditAction(r, "template.create", m.ID, map[string]any{"key": m.Key, "source": in.SourceID})
	d, _ := s.loadOrg(r, m.ID, true)
	writeJSON(w, http.StatusCreated, map[string]any{"model": d})
}

func (s *server) resetTemplate(w http.ResponseWriter, r *http.Request) {
	m, err := s.cfg.Org.ResetBuiltin(r.Context(), r.PathValue("key"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	s.auditAction(r, "template.reset", m.ID, map[string]any{"key": m.Key})
	d, _ := s.loadOrg(r, m.ID, true)
	writeJSON(w, http.StatusOK, map[string]any{"model": d})
}

func (s *server) getOrgModel(w http.ResponseWriter, r *http.Request) {
	d, err := s.loadOrg(r, r.PathValue("id"), true)
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"model": d})
}

func (s *server) exportOrgModel(w http.ResponseWriter, r *http.Request) {
	t, err := s.cfg.Org.Load(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+t.Key+`.json"`)
	writeJSON(w, http.StatusOK, t)
}

func (s *server) updateOrgModel(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        *string             `json:"name"`
		Description *string             `json:"description"`
		Kind        *string             `json:"kind"`
		Key         *string             `json:"key"`
		Governance  *storage.Governance `json:"governance"`
	}
	if !decode(w, r, &in) {
		return
	}
	m, err := s.cfg.Org.UpdateModel(r.Context(), r.PathValue("id"), func(m *storage.OrgModel) {
		if in.Name != nil {
			m.Name = strings.TrimSpace(*in.Name)
		}
		if in.Description != nil {
			m.Description = *in.Description
		}
		if in.Kind != nil {
			m.Kind = *in.Kind
		}
		if in.Key != nil && m.IsTemplate() && !m.Builtin {
			m.Key = strings.TrimSpace(*in.Key)
		}
		if in.Governance != nil {
			m.Governance = *in.Governance
		}
	})
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	s.auditAction(r, "org_model.update", m.ID, nil)
	d, _ := s.loadOrg(r, m.ID, true)
	writeJSON(w, http.StatusOK, map[string]any{"model": d})
}

func (s *server) deleteOrgModel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	m, err := s.cfg.Store.OrgModels().Get(r.Context(), id)
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	if m.Builtin {
		writeError(w, http.StatusBadRequest, "Không xóa mô hình có sẵn; dùng Khôi phục mặc định để hoàn tác chỉnh sửa")
		return
	}
	if err := s.cfg.Store.OrgModels().Delete(r.Context(), id); err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	s.auditAction(r, "org_model.delete", id, map[string]any{"key": m.Key, "repo": m.RepoID})
	w.WriteHeader(http.StatusNoContent)
}

type agentInput struct {
	Key          string              `json:"key"`
	Name         string              `json:"name"`
	Tier         string              `json:"tier"`
	Role         string              `json:"role"`
	Description  string              `json:"description"`
	ReportsTo    []string            `json:"reports_to"`
	ProviderID   string              `json:"provider_id"`
	ModelTier    string              `json:"model_tier"`
	LLMModel     string              `json:"llm_model"`
	Instructions string              `json:"instructions"`
	Permissions  storage.Permissions `json:"permissions"`
	Sort         *int                `json:"sort"`
}

func (in agentInput) apply(a *storage.Agent) {
	a.Key, a.Name, a.Tier, a.Role = strings.TrimSpace(in.Key), strings.TrimSpace(in.Name), in.Tier, in.Role
	a.Description, a.ReportsTo, a.ProviderID = in.Description, in.ReportsTo, in.ProviderID
	a.ModelTier, a.LLMModel, a.Instructions, a.Permissions = in.ModelTier, strings.TrimSpace(in.LLMModel), in.Instructions, in.Permissions
	if in.Sort != nil {
		a.Sort = *in.Sort
	}
	if a.ModelTier == "" {
		a.ModelTier = storage.TierBalanced
	}
}

func (s *server) checkProvider(r *http.Request, id string) error {
	if id == "" {
		return nil
	}
	_, err := s.cfg.Store.Providers().Get(r.Context(), id)
	if errors.Is(err, storage.ErrNotFound) {
		return errBadInput
	}
	return err
}

func (s *server) createAgent(w http.ResponseWriter, r *http.Request) {
	var in agentInput
	if !decode(w, r, &in) {
		return
	}
	if err := s.checkProvider(r, in.ProviderID); err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	a := storage.Agent{OrgModelID: r.PathValue("id")}
	in.apply(&a)
	a, err := s.cfg.Org.SaveAgent(r.Context(), a)
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	s.auditAction(r, "agent.create", a.ID, map[string]any{"key": a.Key, "org_model": a.OrgModelID})
	writeJSON(w, http.StatusCreated, map[string]any{"agent": toAgentDTO(a)})
}

func (s *server) updateAgent(w http.ResponseWriter, r *http.Request) {
	var in agentInput
	if !decode(w, r, &in) {
		return
	}
	if err := s.checkProvider(r, in.ProviderID); err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	a, err := s.cfg.Store.Agents().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	in.apply(&a)
	a, err = s.cfg.Org.SaveAgent(r.Context(), a)
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	s.auditAction(r, "agent.update", a.ID, map[string]any{"key": a.Key})
	writeJSON(w, http.StatusOK, map[string]any{"agent": toAgentDTO(a)})
}

func (s *server) deleteAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.cfg.Org.DeleteAgent(r.Context(), id); err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	s.auditAction(r, "agent.delete", id, nil)
	w.WriteHeader(http.StatusNoContent)
}

// ---- repos ----

type repoDTO struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	GitRemote   string    `json:"git_remote"`
	Description string    `json:"description"`
	Scope       string    `json:"scope"` // folder | machine (no path: helper for the whole machine)
	Exists      bool      `json:"exists"`
	Model       *orgDTO   `json:"model"`
	CreatedAt   time.Time `json:"created_at"`
}

func (s *server) repoDTO(r *http.Request, x storage.Repo, withAgents bool) (repoDTO, error) {
	d := repoDTO{ID: x.ID, Name: x.Name, Path: x.Path, GitRemote: x.GitRemote, Description: x.Description,
		Scope: "machine", Exists: true, CreatedAt: x.CreatedAt}
	if x.Path != "" {
		_, statErr := os.Stat(x.Path)
		d.Scope, d.Exists = "folder", statErr == nil
	}
	m, err := s.cfg.Store.OrgModels().GetForRepo(r.Context(), x.ID)
	if errors.Is(err, storage.ErrNotFound) {
		return d, nil
	}
	if err != nil {
		return d, err
	}
	od, err := s.loadOrg(r, m.ID, withAgents)
	if err != nil {
		return d, err
	}
	d.Model = &od
	return d, nil
}

func (s *server) listRepos(w http.ResponseWriter, r *http.Request) {
	list, err := s.cfg.Store.Repos().List(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	out := make([]repoDTO, 0, len(list))
	for _, x := range list {
		d, err := s.repoDTO(r, x, false)
		if err != nil {
			s.internal(w, r, err)
			return
		}
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": out})
}

func (s *server) createRepo(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path        string `json:"path"`
		Name        string `json:"name"`
		Description string `json:"description"`
		TemplateID  string `json:"template_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	// No path: a machine-wide helper that is not tied to one folder.
	var info repos.Info
	if p := strings.TrimSpace(in.Path); p != "" {
		var err error
		if info, err = repos.Detect(p); err != nil {
			writeError(w, http.StatusBadRequest, "Đường dẫn không tồn tại hoặc không phải thư mục trên máy chạy office")
			return
		}
	} else if strings.TrimSpace(in.Name) == "" {
		writeError(w, http.StatusBadRequest, "Project không có thư mục cần đặt tên")
		return
	}
	if in.Name != "" {
		info.Name = strings.TrimSpace(in.Name)
	}
	if in.Description != "" {
		info.Description = in.Description
	}
	x, err := s.cfg.Store.Repos().Create(r.Context(), storage.Repo{Name: info.Name, Path: info.Path, GitRemote: info.GitRemote, Description: info.Description})
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	if in.TemplateID != "" {
		if _, err := s.cfg.Org.ApplyToRepo(r.Context(), x.ID, in.TemplateID, false); err != nil {
			_ = s.cfg.Store.Repos().Delete(r.Context(), x.ID)
			s.writeDomainError(w, r, err)
			return
		}
	}
	s.auditAction(r, "project.create", x.ID, map[string]any{"path": x.Path, "template": in.TemplateID})
	d, _ := s.repoDTO(r, x, true)
	writeJSON(w, http.StatusCreated, map[string]any{"project": d})
}

func (s *server) getRepo(w http.ResponseWriter, r *http.Request) {
	x, err := s.cfg.Store.Repos().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	d, err := s.repoDTO(r, x, true)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project": d})
}

func (s *server) updateRepo(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if !decode(w, r, &in) {
		return
	}
	x, err := s.cfg.Store.Repos().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	if in.Name != nil && strings.TrimSpace(*in.Name) != "" {
		x.Name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		x.Description = *in.Description
	}
	if err := s.cfg.Store.Repos().Update(r.Context(), x); err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	s.auditAction(r, "project.update", x.ID, nil)
	d, _ := s.repoDTO(r, x, true)
	writeJSON(w, http.StatusOK, map[string]any{"project": d})
}

func (s *server) deleteRepo(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.cfg.Store.Repos().Delete(r.Context(), id); err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	s.auditAction(r, "project.delete", id, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) applyRepoModel(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TemplateID string `json:"template_id"`
		Replace    bool   `json:"replace"`
	}
	if !decode(w, r, &in) {
		return
	}
	m, err := s.cfg.Org.ApplyToRepo(r.Context(), r.PathValue("id"), in.TemplateID, in.Replace)
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	s.auditAction(r, "project.apply_model", r.PathValue("id"), map[string]any{"template": in.TemplateID, "model": m.ID, "replace": in.Replace})
	x, _ := s.cfg.Store.Repos().Get(r.Context(), r.PathValue("id"))
	d, _ := s.repoDTO(r, x, true)
	writeJSON(w, http.StatusOK, map[string]any{"project": d})
}

func (s *server) auditAction(r *http.Request, action, target string, detail map[string]any) {
	_ = s.cfg.Store.Audit().Append(r.Context(), storage.AuditEntry{Actor: "human:" + userFrom(r).Email, Action: action, Target: target, Detail: detail})
}

// listDirs powers the folder tree in the dashboard. Browsers cannot hand a web
// page an absolute path, but the office server runs on the machine that holds
// the projects, so it lists sub-folder names (never file contents) for admins.
func (s *server) listDirs(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		if s.cfg.System.ProjectRoot != "" {
			path = filepath.Dir(s.cfg.System.ProjectRoot)
		} else if home, err := os.UserHomeDir(); err == nil {
			path = home
		} else {
			path = "/"
		}
	}
	l, err := repos.ListDirs(path, r.URL.Query().Get("hidden") == "1")
	switch {
	case errors.Is(err, os.ErrPermission):
		writeError(w, http.StatusForbidden, "Không có quyền đọc thư mục này")
		return
	case errors.Is(err, os.ErrNotExist), errors.Is(err, repos.ErrNotDir):
		writeError(w, http.StatusNotFound, "Thư mục không tồn tại")
		return
	case err != nil:
		s.internal(w, r, err)
		return
	}
	type entry struct {
		repos.DirEntry
		Registered bool `json:"registered"`
	}
	entries := make([]entry, 0, len(l.Entries))
	for _, e := range l.Entries {
		_, err := s.cfg.Store.Repos().GetByPath(r.Context(), e.Path)
		entries = append(entries, entry{DirEntry: e, Registered: err == nil})
	}
	var extra []repos.Shortcut
	if s.cfg.System.ProjectRoot != "" {
		extra = append(extra, repos.Shortcut{Label: "Project hiện tại", Path: s.cfg.System.ProjectRoot})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path": l.Path, "parent": l.Parent, "entries": entries, "truncated": l.Truncated,
		"shortcuts": repos.Shortcuts(extra...),
	})
}
