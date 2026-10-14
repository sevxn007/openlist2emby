// Package openlistidentity implements the identity-only OpenList API boundary.
// It never exposes upstream response bodies or transport errors containing URLs.
package openlistidentity

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

type Error struct {
	Code    int
	Message string
}

func (e *Error) Error() string         { return e.Message }
func (e *Error) PublicMessage() string { return e.Message }
func (e *Error) StatusCode() int       { return e.Code }

type Client struct{ http *http.Client }

func New() *Client {
	return &Client{http: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *Client) request(ctx context.Context, base, method, path, token string, input, output any) error {
	var body io.Reader
	if input != nil {
		b, err := json.Marshal(input)
		if err != nil {
			return &Error{503, "OpenList identity service unavailable"}
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, body)
	if err != nil {
		return &Error{503, "OpenList identity service unavailable"}
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return &Error{503, "OpenList identity service unavailable"}
	}
	defer resp.Body.Close()
	var envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&envelope); err != nil {
		return &Error{503, "OpenList identity service unavailable"}
	}
	code := envelope.Code
	if resp.StatusCode != 200 {
		code = resp.StatusCode
	}
	switch code {
	case 200:
	case 401, 403:
		return &Error{401, "OpenList authentication failed"}
	case 402:
		return &Error{401, "OpenList verification code required or invalid"}
	case 429:
		return &Error{429, "OpenList login temporarily rate limited"}
	default:
		return &Error{503, "OpenList identity service unavailable"}
	}
	if err := json.Unmarshal(envelope.Data, output); err != nil {
		return &Error{503, "OpenList identity service unavailable"}
	}
	return nil
}
func (c *Client) Login(ctx context.Context, base, username, password, otp string) (string, domain.OpenListIdentity, error) {
	var result struct {
		Token string `json:"token"`
	}
	err := c.request(ctx, base, http.MethodPost, "/api/auth/login", "", map[string]string{"username": username, "password": password, "otp_code": otp}, &result)
	if err != nil {
		return "", domain.OpenListIdentity{}, err
	}
	if result.Token == "" {
		return "", domain.OpenListIdentity{}, &Error{503, "OpenList returned an invalid identity"}
	}
	user, err := c.Current(ctx, base, result.Token)
	return result.Token, user, err
}
func (c *Client) Current(ctx context.Context, base, token string) (domain.OpenListIdentity, error) {
	var user domain.OpenListIdentity
	if token == "" {
		return user, &Error{401, "OpenList authentication required"}
	}
	err := c.request(ctx, base, http.MethodGet, "/api/me", token, nil, &user)
	if err == nil && (user.ID <= 0 || user.Username == "" || user.Role == 1 || user.Disabled) {
		err = &Error{401, "OpenList account unavailable"}
	}
	return user, err
}
func (c *Client) Users(ctx context.Context, base, key string, page, perPage int) (domain.OpenListIdentityPage, error) {
	var result domain.OpenListIdentityPage
	err := c.request(ctx, base, http.MethodGet, "/api/admin/user/list?page="+strconv.Itoa(page)+"&per_page="+strconv.Itoa(perPage), key, nil, &result)
	return result, err
}
func (c *Client) User(ctx context.Context, base, key string, id int64) (domain.OpenListIdentity, error) {
	var user domain.OpenListIdentity
	err := c.request(ctx, base, http.MethodGet, "/api/admin/user/get?id="+url.QueryEscape(strconv.FormatInt(id, 10)), key, nil, &user)
	if err == nil && (user.ID != id || user.Role == 1 || user.Disabled || user.Username == "") {
		err = &Error{403, "OpenList account unavailable"}
	}
	return user, err
}
