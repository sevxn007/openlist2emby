package sqlite

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

func TestFreshIdentitySchemaAndSetup(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "identity.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.InitializeSchema(t.Context()); err != nil {
		t.Fatal(err)
	}
	if store.DB().Migrator().HasTable("local_users") {
		t.Fatal("local credential table exists")
	}
	for _, table := range []string{"user_authorizations", "sessions", "identity_provider"} {
		columns, err := store.DB().Migrator().ColumnTypes(table)
		if err != nil {
			t.Fatal(err)
		}
		for _, col := range columns {
			switch col.Name() {
			case "password", "password_hash", "salt", "version":
				t.Fatalf("unexpected credential/compatibility column %s.%s", table, col.Name())
			}
		}
	}
	upstream := domain.OpenListIdentity{ID: 42, Username: "admin", Role: 2}
	if err := store.SaveIdentitySetup(t.Context(), "http://openlist.local", "synthetic-key", upstream); err != nil {
		t.Fatal(err)
	}
	instance, err := store.IdentityInstance(t.Context(), "http://openlist.local")
	if err != nil || instance == "" {
		t.Fatalf("instance=%q err=%v", instance, err)
	}
	user, err := store.FindIdentity(t.Context(), instance, 42)
	if err != nil || user.ID != 1 || !user.Enabled || !user.Role.IsAdmin() {
		t.Fatalf("user=%+v err=%v", user, err)
	}
	if _, err := store.FindIdentity(t.Context(), "other-instance", 42); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("instance isolation: %v", err)
	}
	if err := store.SaveIdentitySetup(t.Context(), "http://other.local", "synthetic-key", upstream); !errors.Is(err, domain.ErrAlreadySetup) {
		t.Fatalf("repeat setup: %v", err)
	}
	if _, err := store.CreateUser(t.Context(), domain.LocalUser{Username: "admin", Role: domain.LocalUserRoleAdmin, Enabled: true}); err == nil {
		t.Fatal("unbound authorization accepted")
	}
	if _, err := store.CreateUser(t.Context(), domain.LocalUser{Username: "admin", Role: domain.LocalUserRoleAdmin, Enabled: true, InstanceID: instance, OpenListUserID: 43}); err != nil {
		t.Fatalf("display name incorrectly acts as identity: %v", err)
	}
	if _, err := store.CreateUser(t.Context(), user); err == nil {
		t.Fatal("duplicate identity accepted")
	}
}
