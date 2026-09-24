package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"bitbucket.org/senprints/agent-office/internal/auth"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

type userDTO struct {
	ID          string     `json:"id"`
	Email       string     `json:"email"`
	Name        string     `json:"name"`
	Role        string     `json:"role"`
	Disabled    bool       `json:"disabled"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at"`
}

func toDTO(u storage.User) userDTO {
	return userDTO{ID: u.ID, Email: u.Email, Name: u.Name, Role: string(u.Role), Disabled: u.Disabled, CreatedAt: u.CreatedAt, LastLoginAt: u.LastLoginAt}
}

func (s *server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.cfg.Version})
}

// authStatus is public: the login page uses it to explain how to create the
// first admin when the office has no account yet.
func (s *server) authStatus(w http.ResponseWriter, r *http.Request) {
	n, err := s.cfg.Store.Users().Count(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"has_users": n > 0})
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	res, err := s.cfg.Auth.Login(r.Context(), in.Email, in.Password, auth.ClientMeta{IP: s.clientIP(r), UserAgent: r.UserAgent()})
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "Email hoặc mật khẩu không đúng")
		return
	case errors.Is(err, auth.ErrThrottled):
		w.Header().Set("Retry-After", "900")
		writeError(w, http.StatusTooManyRequests, "Đăng nhập sai quá nhiều lần, thử lại sau 15 phút")
		return
	case err != nil:
		s.internal(w, r, err)
		return
	}
	s.setCookie(w, r, res.Token, s.cfg.Auth.SessionTTL())
	writeJSON(w, http.StatusOK, map[string]any{"user": toDTO(res.User)})
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if ck, err := r.Cookie(SessionCookie); err == nil {
		if err := s.cfg.Auth.Logout(r.Context(), ck.Value); err != nil {
			s.internal(w, r, err)
			return
		}
	}
	s.clearCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"user": toDTO(userFrom(r))})
}

func (s *server) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if !decode(w, r, &in) {
		return
	}
	err := s.cfg.Auth.ChangePassword(r.Context(), userFrom(r).ID, in.OldPassword, in.NewPassword, sessionFrom(r).ID)
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusBadRequest, "Mật khẩu hiện tại không đúng")
	case errors.Is(err, auth.ErrWeakPassword):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		s.internal(w, r, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *server) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.cfg.Store.Users().List(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	out := make([]userDTO, 0, len(users))
	for _, u := range users {
		out = append(out, toDTO(u))
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

func (s *server) createUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Role     string `json:"role"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	u, err := s.cfg.Auth.CreateUser(r.Context(), auth.NewUser{Email: in.Email, Name: in.Name, Role: storage.Role(in.Role), Password: in.Password}, "human:"+userFrom(r).Email)
	switch {
	case errors.Is(err, auth.ErrEmailTaken):
		writeError(w, http.StatusConflict, "Email đã tồn tại")
	case errors.Is(err, auth.ErrInvalidEmail), errors.Is(err, auth.ErrInvalidRole), errors.Is(err, auth.ErrWeakPassword):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		s.internal(w, r, err)
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"user": toDTO(u)})
	}
}

func (s *server) updateUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in struct {
		Disabled *bool `json:"disabled"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Disabled != nil {
		if *in.Disabled && id == userFrom(r).ID {
			writeError(w, http.StatusBadRequest, "Không thể tự vô hiệu hóa tài khoản của mình")
			return
		}
		err := s.cfg.Auth.SetDisabled(r.Context(), id, *in.Disabled, "human:"+userFrom(r).Email)
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		if err != nil {
			s.internal(w, r, err)
			return
		}
	}
	u, err := s.cfg.Store.Users().GetByID(r.Context(), id)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": toDTO(u)})
}

func (s *server) resetPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	err := s.cfg.Auth.ResetPassword(r.Context(), r.PathValue("id"), in.Password, "human:"+userFrom(r).Email)
	switch {
	case errors.Is(err, storage.ErrNotFound):
		writeError(w, http.StatusNotFound, "user not found")
	case errors.Is(err, auth.ErrWeakPassword):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		s.internal(w, r, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *server) listAudit(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	entries, err := s.cfg.Store.Audit().List(r.Context(), limit)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	type dto struct {
		ID     string         `json:"id"`
		Actor  string         `json:"actor"`
		Action string         `json:"action"`
		Target string         `json:"target"`
		Detail map[string]any `json:"detail"`
		At     time.Time      `json:"at"`
	}
	out := make([]dto, 0, len(entries))
	for _, e := range entries {
		out = append(out, dto{e.ID, e.Actor, e.Action, e.Target, e.Detail, e.At})
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out})
}
