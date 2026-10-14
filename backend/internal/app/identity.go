package app

import (
	"context"
	"errors"
	"strings"

	"github.com/sevxn007/openlist2emby/backend/internal/client/openlistidentity"
	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

func (s *Service) SetIdentityClient(client IdentityClient) {
	s.identityMu.Lock()
	defer s.identityMu.Unlock()
	s.identity = client
}
func (s *Service) identityClient() IdentityClient {
	s.identityMu.Lock()
	defer s.identityMu.Unlock()
	if s.identity == nil {
		s.identity = openlistidentity.New()
	}
	return s.identity
}
func (s *Service) Login(ctx context.Context, username, password, otp string) (LoginResult, error) {
	return s.loginIdentity(ctx, username, password, otp, false)
}
func (s *Service) LoginEmby(ctx context.Context, username, password string) (LoginResult, error) {
	return s.loginIdentity(ctx, username, password, "", true)
}
func (s *Service) loginIdentity(ctx context.Context, username, password, otp string, emby bool) (LoginResult, error) {
	if strings.TrimSpace(username) == "" || password == "" {
		return LoginResult{}, unauthorized("OpenList authentication failed")
	}
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		return LoginResult{}, err
	}
	_, identity, err := s.identityClient().Login(ctx, settings.OpenListBaseURL, strings.TrimSpace(username), password, otp)
	if err != nil {
		return LoginResult{}, err
	}
	if identity.ID <= 0 || identity.Disabled || identity.Role == 1 {
		return LoginResult{}, unauthorized("OpenList account unavailable")
	}
	if emby && identity.OTP {
		return LoginResult{}, unauthorized("Emby login does not support OpenList two-factor accounts")
	}
	instance, err := s.store.IdentityInstance(ctx, settings.OpenListBaseURL)
	if err != nil {
		return LoginResult{}, err
	}
	user, err := s.store.FindIdentity(ctx, instance, identity.ID)
	if errors.Is(err, domain.ErrNotFound) || (err == nil && !user.Enabled) {
		return LoginResult{}, &openlistidentity.Error{Code: 403, Message: "openlist2emby access has not been granted"}
	}
	if err != nil {
		return LoginResult{}, err
	}
	if err := s.store.UpdateIdentityName(ctx, user.ID, identity.Username); err != nil {
		return LoginResult{}, err
	}
	user.Username = identity.Username
	ttl := s.sessionTTL
	if emby {
		ttl = embySessionTTL
	}
	session, err := s.store.CreateSessionForUser(ctx, user.ID, ttl)
	if err != nil {
		return LoginResult{}, err
	}
	if err := s.store.SetSessionType(ctx, session.Token, emby); err != nil {
		_ = s.store.DeleteSession(ctx, session.Token)
		return LoginResult{}, err
	}
	return LoginResult{User: user, Session: session}, nil
}
func (s *Service) Authenticate(ctx context.Context, token string) (domain.Session, error) {
	return s.authenticateIdentity(ctx, token, false)
}
func (s *Service) AuthenticateEmby(ctx context.Context, token string) (domain.Session, error) {
	return s.authenticateIdentity(ctx, token, true)
}
func (s *Service) authenticateIdentity(ctx context.Context, token string, emby bool) (domain.Session, error) {
	session, err := s.store.GetSession(ctx, token)
	if err != nil {
		return domain.Session{}, err
	}
	isEmby, err := s.store.SessionType(ctx, token)
	if err != nil {
		return domain.Session{}, err
	}
	if isEmby != emby {
		return domain.Session{}, unauthorized("login required")
	}
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		return domain.Session{}, err
	}
	user, err := s.store.GetLocalUserByID(ctx, session.UserID)
	if err != nil {
		return domain.Session{}, err
	}
	instance, err := s.store.IdentityInstance(ctx, settings.OpenListBaseURL)
	if err != nil {
		return domain.Session{}, err
	}
	if user.InstanceID != instance || user.OpenListUserID <= 0 {
		return s.rejectIdentitySession(ctx, token, unauthorized("identity binding invalid"))
	}
	ttl := s.sessionTTL
	if emby {
		ttl = embySessionTTL
	}
	return s.store.RenewSession(ctx, token, emby, ttl)
}

// Invalid local identity bindings require a fresh login.
func (s *Service) rejectIdentitySession(ctx context.Context, token string, reason error) (domain.Session, error) {
	if err := s.Logout(ctx, token); err != nil {
		return domain.Session{}, err
	}
	return domain.Session{}, reason
}

func (s *Service) OpenListUsers(ctx context.Context, page, perPage int) (domain.OpenListIdentityPage, error) {
	if page < 1 || perPage < 1 || perPage > 100 {
		return domain.OpenListIdentityPage{}, badRequest("invalid pagination")
	}
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		return domain.OpenListIdentityPage{}, err
	}
	return s.identityClient().Users(ctx, settings.OpenListBaseURL, settings.OpenListAdminKey, page, perPage)
}

type IdentityGrantInput struct {
	OpenListUserID int64
	Role           domain.LocalUserRole
	Enabled        bool
	LibraryGrants  []domain.LibraryGrant
}

func (s *Service) GrantIdentity(ctx context.Context, input IdentityGrantInput) (domain.LocalUser, error) {
	if input.OpenListUserID <= 0 || !input.Role.Valid() {
		return domain.LocalUser{}, badRequest("valid OpenList user and role required")
	}
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		return domain.LocalUser{}, err
	}
	identity, err := s.identityClient().User(ctx, settings.OpenListBaseURL, settings.OpenListAdminKey, input.OpenListUserID)
	if err != nil {
		return domain.LocalUser{}, err
	}
	instance, err := s.store.IdentityInstance(ctx, settings.OpenListBaseURL)
	if err != nil {
		return domain.LocalUser{}, err
	}
	user, err := s.store.CreateUser(ctx, domain.LocalUser{Username: identity.Username, InstanceID: instance, OpenListUserID: identity.ID, Role: input.Role, Enabled: input.Enabled, LibraryGrants: input.LibraryGrants, LibraryGrantsConfigured: true})
	if isUniqueUserError(err) {
		return domain.LocalUser{}, unprocessable("OpenList user is already bound")
	}
	user.UpstreamStatus = "active"
	return user, normalizeUserMutationError(err)
}

func (s *Service) validateBoundIdentity(ctx context.Context, user domain.LocalUser) error {
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		return err
	}
	instance, err := s.store.IdentityInstance(ctx, settings.OpenListBaseURL)
	if err != nil {
		return err
	}
	if user.OpenListUserID <= 0 || user.InstanceID != instance {
		return unprocessable("bind an OpenList identity before enabling access")
	}
	_, err = s.identityClient().User(ctx, settings.OpenListBaseURL, settings.OpenListAdminKey, user.OpenListUserID)
	return err
}

func (s *Service) requireOtherAdministrator(ctx context.Context, excluding int64) error {
	users, err := s.store.ListLocalUsers(ctx)
	if err != nil {
		return err
	}
	for _, user := range users {
		if user.ID == excluding || !user.Enabled || !user.Role.IsAdmin() {
			continue
		}
		err := s.validateBoundIdentity(ctx, user)
		if err == nil {
			return nil
		}
		if StatusCode(err, 500) >= 500 {
			return err
		}
	}
	return unprocessable("at least one available bound administrator is required")
}
