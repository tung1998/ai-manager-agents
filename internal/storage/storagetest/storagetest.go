// Package storagetest is the contract every storage driver must pass.
package storagetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Run executes the contract suite. newStore must return a fresh, migrated store.
func Run(t *testing.T, newStore func(t *testing.T) storage.Store) {
	t.Run("users", func(t *testing.T) { testUsers(t, newStore(t)) })
	t.Run("sessions", func(t *testing.T) { testSessions(t, newStore(t)) })
	t.Run("audit", func(t *testing.T) { testAudit(t, newStore(t)) })
	t.Run("providers", func(t *testing.T) { testProviders(t, newStore(t)) })
	t.Run("org", func(t *testing.T) { testOrg(t, newStore(t)) })
	t.Run("tx", func(t *testing.T) { testTx(t, newStore(t)) })
	t.Run("helper projects", func(t *testing.T) { testHelperProjects(t, newStore(t)) })
}

func testUsers(t *testing.T, s storage.Store) {
	ctx := context.Background()
	users := s.Users()

	n, err := users.Count(ctx)
	if err != nil || n != 0 {
		t.Fatalf("Count on empty = %d, %v", n, err)
	}
	u, err := users.Create(ctx, storage.User{Email: "Admin@Example.com", Name: "Admin", Role: storage.RoleAdmin, PasswordHash: "h1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if u.ID == "" || u.CreatedAt.IsZero() {
		t.Fatalf("Create did not fill ID/CreatedAt: %+v", u)
	}
	if _, err := users.Create(ctx, storage.User{Email: "admin@example.com", Role: storage.RoleMember, PasswordHash: "x"}); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("duplicate email (case-insensitive) err = %v, want ErrConflict", err)
	}
	got, err := users.GetByEmail(ctx, "ADMIN@example.com")
	if err != nil || got.ID != u.ID || got.Role != storage.RoleAdmin {
		t.Fatalf("GetByEmail = %+v, %v", got, err)
	}
	if _, err := users.GetByEmail(ctx, "nobody@example.com"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("GetByEmail missing err = %v", err)
	}
	if err := users.UpdatePassword(ctx, u.ID, "h2"); err != nil {
		t.Fatalf("UpdatePassword: %v", err)
	}
	if err := users.SetDisabled(ctx, u.ID, true); err != nil {
		t.Fatalf("SetDisabled: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := users.TouchLogin(ctx, u.ID, now); err != nil {
		t.Fatalf("TouchLogin: %v", err)
	}
	got, err = users.GetByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.PasswordHash != "h2" || !got.Disabled || got.LastLoginAt == nil || !got.LastLoginAt.Equal(now) {
		t.Fatalf("after updates = %+v", got)
	}
	if err := users.UpdatePassword(ctx, "usr_missing", "x"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("UpdatePassword missing err = %v", err)
	}
	list, err := users.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %d, %v", len(list), err)
	}
}

func testSessions(t *testing.T, s storage.Store) {
	ctx := context.Background()
	u, err := s.Users().Create(ctx, storage.User{Email: "a@b.c", Role: storage.RoleMember, PasswordHash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	sess, err := s.Sessions().Create(ctx, storage.Session{
		UserID: u.ID, TokenHash: "th1", CreatedAt: now, ExpiresAt: now.Add(time.Hour), LastSeenAt: now, UserAgent: "ua", IP: "1.2.3.4",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Sessions().GetByTokenHash(ctx, "th1")
	if err != nil || got.ID != sess.ID || got.UserID != u.ID || !got.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("GetByTokenHash = %+v, %v", got, err)
	}
	if _, err := s.Sessions().GetByTokenHash(ctx, "nope"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("missing err = %v", err)
	}
	if err := s.Sessions().Touch(ctx, sess.ID, now.Add(time.Minute)); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	// expired session
	if _, err := s.Sessions().Create(ctx, storage.Session{UserID: u.ID, TokenHash: "th2", CreatedAt: now, ExpiresAt: now.Add(-time.Second), LastSeenAt: now}); err != nil {
		t.Fatal(err)
	}
	n, err := s.Sessions().DeleteExpired(ctx, now)
	if err != nil || n != 1 {
		t.Fatalf("DeleteExpired = %d, %v", n, err)
	}
	keep, _ := s.Sessions().Create(ctx, storage.Session{UserID: u.ID, TokenHash: "th3", CreatedAt: now, ExpiresAt: now.Add(time.Hour), LastSeenAt: now})
	if err := s.Sessions().DeleteForUserExcept(ctx, u.ID, keep.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sessions().GetByTokenHash(ctx, "th1"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("DeleteForUserExcept kept th1: %v", err)
	}
	if _, err := s.Sessions().GetByTokenHash(ctx, "th3"); err != nil {
		t.Fatalf("DeleteForUserExcept removed kept session: %v", err)
	}
	if err := s.Sessions().DeleteForUser(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sessions().GetByTokenHash(ctx, "th3"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("after DeleteForUser err = %v", err)
	}
}

func testAudit(t *testing.T, s storage.Store) {
	ctx := context.Background()
	if err := s.Audit().Append(ctx, storage.AuditEntry{Actor: "system", Action: "user.create", Target: "usr_1", Detail: map[string]any{"role": "admin"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Audit().Append(ctx, storage.AuditEntry{Actor: "human:a", Action: "auth.login"}); err != nil {
		t.Fatal(err)
	}
	list, err := s.Audit().List(ctx, 10)
	if err != nil || len(list) != 2 {
		t.Fatalf("List = %d, %v", len(list), err)
	}
	if list[0].Action != "auth.login" || list[1].Detail["role"] != "admin" {
		t.Fatalf("order/detail wrong: %+v", list)
	}
}
