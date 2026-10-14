package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/client/openlistidentity"
	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/repository/sqlite"
)

type identityProbe struct {
	identity domain.OpenListIdentity
	failure  error
	calls    int
	otp      string
}

func (p *identityProbe) Login(_ context.Context, base, name, password, otp string) (string, domain.OpenListIdentity, error) {
	p.otp = otp
	if password != "synthetic-password" {
		return "", domain.OpenListIdentity{}, &openlistidentity.Error{Code: 401, Message: "authentication failed"}
	}
	return "synthetic-token", p.identity, p.failure
}
func (p *identityProbe) Current(_ context.Context, base, token string) (domain.OpenListIdentity, error) {
	p.calls++
	return p.identity, p.failure
}
func (p *identityProbe) User(_ context.Context, base, key string, id int64) (domain.OpenListIdentity, error) {
	if p.failure != nil {
		return domain.OpenListIdentity{}, p.failure
	}
	if id != p.identity.ID || p.identity.Disabled || p.identity.Role == 1 {
		return domain.OpenListIdentity{}, &openlistidentity.Error{Code: 403, Message: "unavailable"}
	}
	return p.identity, nil
}
func (p *identityProbe) Users(context.Context, string, string, int, int) (domain.OpenListIdentityPage, error) {
	return domain.OpenListIdentityPage{Content: []domain.OpenListIdentity{p.identity}, Total: 1}, p.failure
}
func identityService(t *testing.T) (*Service, *sqlite.Store, *identityProbe, *time.Time) {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "identity.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store = sqlite.NewStoreWithClock(store.DB(), func() time.Time { return now })
	probe := &identityProbe{identity: domain.OpenListIdentity{ID: 42, Username: "alice", Role: 2}}
	service := NewService(store, nil, nil, nil, nil)
	service.SetIdentityClient(probe)
	if err := service.Setup(t.Context(), SetupInput{OpenListBaseURL: "http://openlist.local", OpenListAdminKey: "synthetic-key", Username: "alice", Password: "synthetic-password"}); err != nil {
		t.Fatal(err)
	}
	probe.calls = 0
	probe.identity.Role = 0 // Upstream role does not overwrite Manager role.
	return service, store, probe, &now
}
func loginIdentityTest(t *testing.T, service *Service) LoginResult {
	t.Helper()
	result, err := service.Login(t.Context(), "alice", "synthetic-password", "123456")
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestIdentityLocalAuthenticationAndRenameOnLogin(t *testing.T) {
	service, store, probe, now := identityService(t)
	login := loginIdentityTest(t, service)
	if probe.otp != "123456" || login.User.Role != domain.LocalUserRoleAdmin {
		t.Fatal("OTP or independent role not preserved")
	}
	var upstream string
	if err := store.DB().Raw("SELECT upstream_token FROM sessions WHERE token=?", login.Session.Token).Scan(&upstream).Error; err != nil || upstream != "" {
		t.Fatal("upstream token persisted", err)
	}
	probe.identity.Username = "renamed"
	*now = now.Add(time.Hour)
	session, err := service.Authenticate(t.Context(), login.Session.Token)
	if err != nil || session.Username != "alice" || probe.calls != 0 {
		t.Fatal("local authentication contacted upstream or renamed user", err)
	}
	relogin := loginIdentityTest(t, service)
	if relogin.User.Username != "renamed" {
		t.Fatal("login did not refresh username")
	}
	if err := store.DB().Exec("UPDATE user_authorizations SET enabled=0 WHERE id=?", login.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(t.Context(), login.Session.Token); err == nil {
		t.Fatal("disabled local access accepted")
	}
}
func TestIdentityDenialsAndEmbyOTP(t *testing.T) {
	for _, scenario := range []string{"wrong-password", "guest", "disabled", "unbound", "local-disabled", "emby-otp"} {
		t.Run(scenario, func(t *testing.T) {
			service, store, probe, _ := identityService(t)
			password := "synthetic-password"
			emby := false
			want := 401
			switch scenario {
			case "wrong-password":
				password = "wrong"
			case "guest":
				probe.identity.Role = 1
			case "disabled":
				probe.identity.Disabled = true
			case "unbound":
				probe.identity.ID = 999
				want = 403
			case "local-disabled":
				store.DB().Exec("UPDATE user_authorizations SET enabled=0")
				want = 403
			case "emby-otp":
				probe.identity.OTP = true
				emby = true
			}
			var err error
			if emby {
				_, err = service.LoginEmby(t.Context(), "alice", password)
			} else {
				_, err = service.Login(t.Context(), "alice", password, "")
			}
			if StatusCode(err, 500) != want {
				t.Fatalf("got %v, want %d", err, want)
			}
		})
	}
	service, store, probe, now := identityService(t)
	login, err := service.LoginEmby(t.Context(), "alice", "synthetic-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(t.Context(), login.Session.Token); err == nil {
		t.Fatal("Emby identity session accepted as browser session")
	}
	if _, err := service.AuthenticateEmby(t.Context(), login.Session.Token); err != nil {
		t.Fatal(err)
	}
	probe.identity.OTP = true
	*now = now.Add(time.Minute)
	if _, err := service.AuthenticateEmby(t.Context(), login.Session.Token); err != nil {
		t.Fatalf("upstream OTP change affected existing session: %v", err)
	}
	if err := service.Logout(t.Context(), login.Session.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetSession(t.Context(), login.Session.Token); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("logout did not revoke session")
	}
}
func TestUpstreamChangesDoNotRevokeLocalSessionAndRoleRefresh(t *testing.T) {
	for _, scenario := range []string{"disabled", "guest", "expired", "deleted"} {
		t.Run(scenario, func(t *testing.T) {
			service, store, probe, now := identityService(t)
			login := loginIdentityTest(t, service)
			if _, err := service.Authenticate(t.Context(), login.Session.Token); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "disabled":
				probe.identity.Disabled = true
			case "guest":
				probe.identity.Role = 1
			default:
				probe.failure = &openlistidentity.Error{Code: 401, Message: "authentication failed"}
			}
			*now = now.Add(time.Minute)
			if _, err := service.Authenticate(t.Context(), login.Session.Token); err != nil || probe.calls != 0 {
				t.Fatalf("upstream change affected local authentication: %v", err)
			}
			if _, err := store.GetSession(t.Context(), login.Session.Token); err != nil {
				t.Fatal("upstream change deleted local session")
			}
		})
	}
	service, store, _, _ := identityService(t)
	login := loginIdentityTest(t, service)
	if _, err := service.Authenticate(t.Context(), login.Session.Token); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().Exec("UPDATE user_authorizations SET role='media' WHERE id=?", login.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	session, err := service.Authenticate(t.Context(), login.Session.Token)
	if err != nil || session.Role != domain.LocalUserRoleMedia {
		t.Fatal("local authentication hid role change")
	}
}
func TestInstanceSwitchAndLastAdministratorProtection(t *testing.T) {
	service, store, probe, _ := identityService(t)
	login := loginIdentityTest(t, service)
	if _, err := service.UpdateSettings(t.Context(), SettingsInput{OpenListBaseURL: "http://other.local"}); StatusCode(err, 500) != 400 {
		t.Fatalf("online instance switch accepted: %v", err)
	}
	role := domain.LocalUserRoleMedia
	if _, err := service.UpdateUser(t.Context(), login.Session, login.User.ID, UserUpdateInput{Role: &role}); StatusCode(err, 500) != 422 {
		t.Fatalf("last administrator demoted: %v", err)
	}
	instance, err := store.IdentityInstance(t.Context(), "http://openlist.local")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.CreateUser(t.Context(), domain.LocalUser{Username: "other", OpenListUserID: 43, InstanceID: instance, Role: domain.LocalUserRoleAdmin, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	// A disabled/deleted upstream administrator cannot satisfy the safeguard.
	if _, err := service.UpdateUser(t.Context(), login.Session, login.User.ID, UserUpdateInput{Role: &role}); StatusCode(err, 500) != 422 {
		t.Fatalf("unavailable administrator counted: %v", err)
	}
	probe.identity.ID = 43
	if _, err := service.GrantIdentity(t.Context(), IdentityGrantInput{OpenListUserID: 43, Role: domain.LocalUserRoleAdmin, Enabled: true}); StatusCode(err, 500) != 422 {
		t.Fatalf("duplicate identity binding accepted: %v", err)
	}
}

func TestConnectionTestDoesNotForwardSavedKeyToDifferentInstance(t *testing.T) {
	service, _, _, _ := identityService(t)
	_, err := service.TestSettings(t.Context(), SettingsInput{OpenListBaseURL: "http://different.local"})
	if StatusCode(err, 500) != 400 {
		t.Fatalf("saved key may be sent to a different instance: %v", err)
	}
}

func TestSetupRequiresOpenListAdministrator(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "setup.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	probe := &identityProbe{identity: domain.OpenListIdentity{ID: 42, Username: "alice", Role: 0}}
	service := NewService(store, nil, nil, nil, nil)
	service.SetIdentityClient(probe)
	input := SetupInput{OpenListBaseURL: "http://openlist.local", OpenListAdminKey: "synthetic-key", Username: "alice", Password: "synthetic-password", OTPCode: "123456"}
	if err := service.Setup(t.Context(), input); StatusCode(err, 500) != 401 {
		t.Fatalf("non-admin setup accepted: %v", err)
	}
	if _, err := store.GetSettings(t.Context()); !errors.Is(err, domain.ErrNotSetup) {
		t.Fatalf("failed setup persisted state: %v", err)
	}
	probe.identity.Role = 2
	if err := service.Setup(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if probe.otp != "123456" {
		t.Fatal("setup verification code not forwarded")
	}
	if err := service.Setup(t.Context(), input); !errors.Is(err, domain.ErrAlreadySetup) {
		t.Fatalf("repeat setup: %v", err)
	}
}

func TestLocalSessionRejectsInvalidIdentityBinding(t *testing.T) {
	for _, emby := range []bool{false, true} {
		service, store, _, _ := identityService(t)
		login, err := service.loginIdentity(t.Context(), "alice", "synthetic-password", "", emby)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.DB().Exec("UPDATE user_authorizations SET instance_id='different-instance' WHERE id=?", login.User.ID).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := service.authenticateIdentity(t.Context(), login.Session.Token, emby); StatusCode(err, 500) != 401 {
			t.Fatalf("invalid binding accepted: %v", err)
		}
		if _, err := store.GetSession(t.Context(), login.Session.Token); !errors.Is(err, domain.ErrNotFound) {
			t.Fatal("invalid binding session retained", err)
		}
	}
}
