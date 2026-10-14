package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/client/openlistidentity"
	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

func TestEmbySessionSlidingExpiry(t *testing.T) {
	service, store, probe, now := identityService(t)
	login, err := service.LoginEmby(t.Context(), "alice", "synthetic-password")
	if err != nil {
		t.Fatal(err)
	}
	if !login.Session.ExpiresAt.Equal(now.Add(30 * 24 * time.Hour)) {
		t.Fatal("incorrect initial Emby lifetime")
	}
	originalExpiry := login.Session.ExpiresAt
	for _, advance := range []time.Duration{25 * time.Hour, 30 * time.Second, 29 * 24 * time.Hour} {
		*now = now.Add(advance)
		calls := probe.calls
		session, err := service.AuthenticateEmby(t.Context(), login.Session.Token)
		if err != nil {
			t.Fatal(err)
		}
		if !session.ExpiresAt.Equal(now.Add(30 * 24 * time.Hour)) {
			t.Fatal("request did not renew session")
		}
		if probe.calls != calls {
			t.Fatal("local authentication contacted upstream")
		}
		stored, err := store.GetSession(t.Context(), login.Session.Token)
		if err != nil || !stored.ExpiresAt.Equal(session.ExpiresAt) {
			t.Fatalf("renewal not persisted: %v", err)
		}
	}
	if !now.After(originalExpiry) {
		t.Fatal("test did not cross initial expiry")
	}
	*now = now.Add(30 * 24 * time.Hour)
	if _, err := service.AuthenticateEmby(t.Context(), login.Session.Token); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("idle session accepted: %v", err)
	}
}

func TestBrowserSessionSlidingExpiry(t *testing.T) {
	service, store, _, now := identityService(t)
	login := loginIdentityTest(t, service)
	if !login.Session.ExpiresAt.Equal(now.Add(24 * time.Hour)) {
		t.Fatal("incorrect browser lifetime")
	}
	for i := 0; i < 3; i++ {
		*now = now.Add(23 * time.Hour)
		session, err := service.Authenticate(t.Context(), login.Session.Token)
		if err != nil || !session.ExpiresAt.Equal(now.Add(24*time.Hour)) {
			t.Fatal("browser renewal failed", err)
		}
		if _, err := service.AuthenticateEmby(t.Context(), login.Session.Token); err == nil {
			t.Fatal("browser accepted as Emby")
		}
		stored, err := store.GetSession(t.Context(), login.Session.Token)
		if err != nil || !stored.ExpiresAt.Equal(session.ExpiresAt) {
			t.Fatal("renewal not persisted", err)
		}
	}
	*now = now.Add(24 * time.Hour)
	if _, err := service.Authenticate(t.Context(), login.Session.Token); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("idle browser accepted", err)
	}
}

func TestLocalSessionsIgnoreUpstreamFailure(t *testing.T) {
	for _, emby := range []bool{false, true} {
		for _, status := range []int{401, 403, 503} {
			service, store, probe, now := identityService(t)
			login, err := service.loginIdentity(t.Context(), "alice", "synthetic-password", "", emby)
			if err != nil {
				t.Fatal(err)
			}
			// Existing deployments may have a retained, expired upstream token.
			if err := store.DB().Exec("UPDATE sessions SET upstream_token=? WHERE token=?", "expired-synthetic-token", login.Session.Token).Error; err != nil {
				t.Fatal(err)
			}
			probe.failure = &openlistidentity.Error{Code: status, Message: "synthetic upstream failure"}
			*now = now.Add(23 * time.Hour)
			session, err := service.authenticateIdentity(t.Context(), login.Session.Token, emby)
			ttl := 24 * time.Hour
			if emby {
				ttl = 30 * 24 * time.Hour
			}
			if err != nil || !session.ExpiresAt.Equal(now.Add(ttl)) || probe.calls != 0 {
				t.Fatalf("local auth failed: emby=%v status=%d err=%v", emby, status, err)
			}
			if _, err := service.loginIdentity(t.Context(), "alice", "synthetic-password", "", emby); err == nil {
				t.Fatal("new login bypassed upstream failure")
			}
		}
	}
}

func TestEmbySessionFailureDoesNotRenew(t *testing.T) {
	for _, scenario := range []string{"logout", "revoked", "local-disabled"} {
		t.Run(scenario, func(t *testing.T) {
			service, store, _, now := identityService(t)
			login, err := service.LoginEmby(t.Context(), "alice", "synthetic-password")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.AuthenticateEmby(t.Context(), login.Session.Token); err != nil {
				t.Fatal(err)
			}
			*now = now.Add(time.Minute)
			switch scenario {
			case "logout":
				err = service.Logout(t.Context(), login.Session.Token)
			case "revoked":
				err = store.RevokeUserSessions(t.Context(), login.User.ID)
			case "local-disabled":
				err = store.DB().Exec("UPDATE user_authorizations SET enabled=0 WHERE id=?", login.User.ID).Error
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.AuthenticateEmby(t.Context(), login.Session.Token); err == nil {
				t.Fatal("failed authentication accepted")
			}
			if _, err := store.GetSession(t.Context(), login.Session.Token); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("revoked session still exists: %v", err)
			}
		})
	}
}

type failingEmbyRenewalStore struct{ Store }

func (s failingEmbyRenewalStore) RenewSession(context.Context, string, bool, time.Duration) (domain.Session, error) {
	return domain.Session{}, errors.New("synthetic renewal failure")
}

func TestEmbySessionRenewalFailureFailsAuthentication(t *testing.T) {
	service, store, _, _ := identityService(t)
	login, err := service.LoginEmby(t.Context(), "alice", "synthetic-password")
	if err != nil {
		t.Fatal(err)
	}
	service.store = failingEmbyRenewalStore{Store: store}
	if _, err := service.AuthenticateEmby(t.Context(), login.Session.Token); err == nil {
		t.Fatal("renewal failure ignored")
	}
}

type interruptedRenewalStore struct {
	Store
	beforeRenew func()
}

func (s interruptedRenewalStore) RenewSession(ctx context.Context, token string, emby bool, ttl time.Duration) (domain.Session, error) {
	s.beforeRenew()
	return s.Store.RenewSession(ctx, token, emby, ttl)
}

func TestEmbySessionInvalidatedBeforeRenewal(t *testing.T) {
	for _, scenario := range []string{"logout", "revocation", "expiry", "disabled"} {
		t.Run(scenario, func(t *testing.T) {
			service, store, _, now := identityService(t)
			login, err := service.LoginEmby(t.Context(), "alice", "synthetic-password")
			if err != nil {
				t.Fatal(err)
			}
			service.store = interruptedRenewalStore{Store: store, beforeRenew: func() {
				var err error
				switch scenario {
				case "logout":
					err = service.Logout(t.Context(), login.Session.Token)
				case "revocation":
					err = store.RevokeUserSessions(t.Context(), login.User.ID)
				case "expiry":
					*now = login.Session.ExpiresAt
				case "disabled":
					err = store.DB().Exec("UPDATE user_authorizations SET enabled=0 WHERE id=?", login.User.ID).Error
				}
				if err != nil {
					t.Fatal(err)
				}
			}}
			if _, err := service.AuthenticateEmby(t.Context(), login.Session.Token); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("invalidated session authenticated: %v", err)
			}
			if _, err := store.GetSession(t.Context(), login.Session.Token); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("invalidated session restored: %v", err)
			}
		})
	}
}
