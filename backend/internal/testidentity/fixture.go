// Package testidentity supplies deterministic identity fixtures for workflow tests.
// Authentication security tests use independent upstream HTTP servers instead.
package testidentity

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/client/openlistidentity"
	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/repository/sqlite"
)

type Client struct{ Store *sqlite.Store }

func Bind(ctx context.Context, store *sqlite.Store, id int64) error {
	settings, err := store.GetSettings(ctx)
	if err != nil {
		return err
	}
	instance, err := store.IdentityInstance(ctx, settings.OpenListBaseURL)
	if err != nil {
		instance = "fixture-provider"
		if err := store.DB().Exec("INSERT INTO identity_provider(id,base_url) VALUES(?,?)", instance, settings.OpenListBaseURL).Error; err != nil {
			return err
		}
	}
	return store.DB().Exec("UPDATE user_authorizations SET instance_id=?,open_list_user_id=? WHERE id=?", instance, id, id).Error
}
func Session(ctx context.Context, store *sqlite.Store, id int64, ttl time.Duration) (domain.Session, error) {
	if err := Bind(ctx, store, id); err != nil {
		return domain.Session{}, err
	}
	session, err := store.CreateSessionForUser(ctx, id, ttl)
	if err != nil {
		return session, err
	}
	err = store.SetSessionType(ctx, session.Token, false)
	return session, err
}
func (c *Client) Login(ctx context.Context, base, username, password, otp string) (string, domain.OpenListIdentity, error) {
	if password != "password-123" && password != username+"-password" {
		return "", domain.OpenListIdentity{}, &openlistidentity.Error{Code: 401, Message: "invalid credentials"}
	}
	user, err := c.Store.GetLocalUserByUsername(ctx, username)
	if err != nil {
		return "1", domain.OpenListIdentity{ID: 1, Username: username, Role: 2}, nil
	}
	if err := Bind(ctx, c.Store, user.ID); err != nil {
		return "", domain.OpenListIdentity{}, err
	}
	identity := domain.OpenListIdentity{ID: user.ID, Username: user.Username, Role: 0, Disabled: !user.Enabled}
	return strconv.FormatInt(user.ID, 10), identity, nil
}
func (c *Client) Current(ctx context.Context, base, token string) (domain.OpenListIdentity, error) {
	if token == "admin-key" {
		return domain.OpenListIdentity{ID: 1, Username: "admin", Role: 2}, nil
	}
	id, err := strconv.ParseInt(token, 10, 64)
	if err != nil {
		return domain.OpenListIdentity{}, fmt.Errorf("invalid fixture token")
	}
	user, err := c.Store.GetLocalUserByID(ctx, id)
	return domain.OpenListIdentity{ID: id, Username: user.Username, Disabled: !user.Enabled}, err
}
func (c *Client) Users(ctx context.Context, base, key string, page, size int) (domain.OpenListIdentityPage, error) {
	users, err := c.Store.ListLocalUsers(ctx)
	result := domain.OpenListIdentityPage{}
	for _, u := range users {
		result.Content = append(result.Content, domain.OpenListIdentity{ID: u.ID, Username: u.Username, Disabled: !u.Enabled})
	}
	result.Total = len(result.Content)
	return result, err
}
func (c *Client) User(ctx context.Context, base, key string, id int64) (domain.OpenListIdentity, error) {
	return domain.OpenListIdentity{ID: id, Username: strings.TrimSpace(fmt.Sprintf("upstream-%d", id)), Role: 0}, nil
}
