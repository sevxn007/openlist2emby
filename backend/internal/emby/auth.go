package emby

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/sevxn007/openlist2emby/backend/internal/app"
	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

var authorizationValuePattern = regexp.MustCompile(`(?i)([A-Za-z][A-Za-z0-9]*)\s*=\s*"([^"]*)"|([A-Za-z][A-Za-z0-9]*)\s*=\s*([^,\s]+)`)

type LocalAccountStore interface {
	GetLocalUserByID(context.Context, int64) (domain.LocalUser, error)
	ListLocalUsers(context.Context) ([]domain.LocalUser, error)
}

type CredentialVerifier interface {
	LoginEmby(context.Context, string, string) (app.LoginResult, error)
	AuthenticateEmby(context.Context, string) (domain.Session, error)
	Logout(context.Context, string) error
}

type clientAuthorization struct {
	Client   string
	Device   string
	DeviceID string
	Version  string
	Token    string
}

func parseClientAuthorization(r *http.Request) clientAuthorization {
	values := map[string]string{}
	for _, headerName := range []string{"X-Emby-Authorization", "Authorization"} {
		for _, match := range authorizationValuePattern.FindAllStringSubmatch(r.Header.Get(headerName), -1) {
			key, value := match[1], match[2]
			if key == "" {
				key, value = match[3], match[4]
			}
			values[strings.ToLower(key)] = value
		}
	}
	token := firstNonEmpty(
		r.Header.Get("X-Emby-Token"),
		r.Header.Get("X-MediaBrowser-Token"),
		values["token"],
		queryValueFold(r, "api_key", "apikey", "x-emby-token"),
	)
	return clientAuthorization{
		Client: values["client"], Device: values["device"], DeviceID: values["deviceid"],
		Version: values["version"], Token: strings.TrimSpace(token),
	}
}

func queryValueFold(r *http.Request, names ...string) string {
	for key, values := range r.URL.Query() {
		for _, name := range names {
			if strings.EqualFold(key, name) && len(values) > 0 {
				return values[0]
			}
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func newAccessToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate Emby access token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func newServerID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate Emby server ID: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func tokenHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}
