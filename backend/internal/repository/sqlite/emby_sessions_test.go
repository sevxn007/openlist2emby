package sqlite

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

func embySessionStore(t *testing.T) (*Store, *time.Time, domain.Session) {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store = NewStoreWithClock(store.DB(), func() time.Time { return now })
	if err := store.SaveIdentitySetup(t.Context(), "http://openlist.local", "synthetic-key", domain.OpenListIdentity{ID: 1, Username: "alice", Role: 2}); err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateSessionForUser(t.Context(), 1, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetSessionType(t.Context(), session.Token, true); err != nil {
		t.Fatal(err)
	}
	return store, &now, session
}

func TestRenewSessionGuards(t *testing.T) {
	for _, emby := range []bool{false, true} {
		t.Run(fmt.Sprintf("emby=%v", emby), func(t *testing.T) {
			for _, scenario := range []string{"existing", "expired", "deleted", "wrong-type", "disabled", "revoked"} {
				t.Run(scenario, func(t *testing.T) {
					store, now, session := embySessionStore(t)
					if err := store.SetSessionType(t.Context(), session.Token, emby); err != nil {
						t.Fatal(err)
					}
					*now = now.Add(23 * time.Hour)
					var err error
					switch scenario {
					case "expired":
						*now = session.ExpiresAt
					case "deleted":
						err = store.DeleteSession(t.Context(), session.Token)
					case "wrong-type":
						err = store.SetSessionType(t.Context(), session.Token, !emby)
					case "disabled":
						err = store.DB().Exec("UPDATE user_authorizations SET enabled=0 WHERE id=1").Error
					case "revoked":
						err = store.RevokeUserSessions(t.Context(), 1)
					}
					if err != nil {
						t.Fatal(err)
					}
					renewed, err := store.RenewSession(t.Context(), session.Token, emby, 30*24*time.Hour)
					if scenario == "existing" {
						if err != nil || !renewed.ExpiresAt.Equal(now.Add(30*24*time.Hour)) {
							t.Fatalf("existing session not extended: %v", err)
						}
						if !renewed.CreatedAt.Equal(session.CreatedAt) || !renewed.UpdatedAt.Equal(*now) {
							t.Fatal("incorrect renewal timestamps")
						}
					} else if !errors.Is(err, domain.ErrNotFound) {
						t.Fatalf("invalid session renewed: %v", err)
					}
				})
			}

		})
	}
}

func TestRenewSessionConcurrentUpdatesDoNotShortenExpiry(t *testing.T) {
	for _, emby := range []bool{false, true} {
		t.Run(fmt.Sprintf("emby=%v", emby), func(t *testing.T) {
			store, now, session := embySessionStore(t)
			if err := store.SetSessionType(t.Context(), session.Token, emby); err != nil {
				t.Fatal(err)
			}
			*now = now.Add(time.Hour)
			var wg sync.WaitGroup
			for i := 0; i < 16; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					ttl := time.Duration(30+i) * 24 * time.Hour
					if _, err := store.RenewSession(t.Context(), session.Token, emby, ttl); err != nil {
						t.Errorf("renewal failed: %v", err)
					}
				}(i)
			}
			wg.Wait()
			stored, err := store.GetSession(t.Context(), session.Token)
			if err != nil || !stored.ExpiresAt.Equal(now.Add(45*24*time.Hour)) {
				t.Fatalf("concurrent renewal lost longest expiry: %v", err)
			}
			// An older request must return the persisted expiry rather than its earlier candidate.
			renewed, err := store.RenewSession(t.Context(), session.Token, emby, 30*24*time.Hour)
			if err != nil || !renewed.ExpiresAt.Equal(stored.ExpiresAt) {
				t.Fatalf("expiry shortened: %v", err)
			}

		})
	}
}

func TestRenewSessionNanosecondBoundary(t *testing.T) {
	for _, emby := range []bool{false, true} {
		t.Run(fmt.Sprintf("emby=%v", emby), func(t *testing.T) {
			store, now, session := embySessionStore(t)
			if err := store.SetSessionType(t.Context(), session.Token, emby); err != nil {
				t.Fatal(err)
			}
			*now = session.ExpiresAt.Add(-time.Nanosecond)
			if _, err := store.RenewSession(t.Context(), session.Token, emby, 30*24*time.Hour); err != nil {
				t.Fatal(err)
			}
			*now = now.Add(30 * 24 * time.Hour)
			if _, err := store.RenewSession(t.Context(), session.Token, emby, 30*24*time.Hour); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("exact expiry accepted: %v", err)
			}

		})
	}
}
