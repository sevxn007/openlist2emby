package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/app"
	"github.com/sevxn007/openlist2emby/backend/internal/client/openlist"
	"github.com/sevxn007/openlist2emby/backend/internal/client/openlistidentity"
	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/repository/sqlite"
	"github.com/sevxn007/openlist2emby/backend/internal/testidentity"
)

func TestSetupRequiresOpenListCredentials(t *testing.T) {
	for _, missing := range []string{"username", "password"} {
		t.Run(missing, func(t *testing.T) {
			server, _ := newTestServer(t)
			body := setupRequest{OpenListBaseURL: "http://openlist.local", OpenListAdminKey: "admin-key", Username: "admin", Password: "password-123"}
			if missing == "username" {
				body.Username = ""
			} else {
				body.Password = ""
			}
			result := httptest.NewRecorder()
			server.ServeHTTP(result, requestJSON("POST", "/api/manager/setup", body))
			if result.Code != 400 {
				t.Fatalf("status=%d body=%s", result.Code, result.Body.String())
			}
		})
	}
}

func TestOpenListLoginKeepsUserTokenServerSideAndProxyUsesAdminKey(t *testing.T) {
	var loginCalls, meCalls, proxyCalls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data any
		switch r.URL.Path {
		case "/api/auth/login":
			loginCalls++
			var payload map[string]string
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if payload["username"] != "alice" || payload["password"] != "synthetic-password" || payload["otp_code"] != "123456" {
				t.Error("wrong login payload")
			}
			data = map[string]any{"token": "synthetic-user-token"}
		case "/api/me":
			meCalls++
			if r.Header.Get("Authorization") != "synthetic-user-token" {
				t.Error("identity request did not use user token")
			}
			data = map[string]any{"id": 42, "username": "alice", "role": 0, "otp": true, "password": "private-upstream-field"}
		case "/api/fs/list":
			proxyCalls++
			if r.Header.Get("Authorization") != "synthetic-admin-key" {
				t.Error("directory request did not use administrator key")
			}
			data = map[string]any{"content": []any{}, "total": 0}
		default:
			t.Errorf("unexpected upstream path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(envelope{Code: 200, Message: "success", Data: data})
	}))
	defer upstream.Close()
	server, store := newTestServerWithOpenListClient(t, openlist.NewClient(upstream.Client()))
	server.application.SetIdentityClient(openlistidentity.New())
	if err := store.SaveIdentitySetup(t.Context(), upstream.URL, "synthetic-admin-key", domain.OpenListIdentity{ID: 42, Username: "previous-name", Role: 2}); err != nil {
		t.Fatal(err)
	}
	result := httptest.NewRecorder()
	server.ServeHTTP(result, requestJSON("POST", "/api/manager/auth/login", loginRequest{Username: "alice", Password: "synthetic-password", OTPCode: "123456"}))
	if result.Code != 200 {
		t.Fatalf("login=%d %s", result.Code, result.Body.String())
	}
	for _, secret := range []string{"synthetic-password", "synthetic-user-token", "synthetic-admin-key", "private-upstream-field"} {
		if strings.Contains(result.Body.String()+result.Header().Get("Set-Cookie"), secret) {
			t.Fatal("secret exposed")
		}
	}
	cookie := result.Result().Cookies()[0]
	if !cookie.HttpOnly {
		t.Fatal("session cookie is not HTTP-only")
	}
	request := requestJSON("POST", "/api/manager/openlist/fs/list", map[string]any{"path": "/"})
	request.AddCookie(cookie)
	result = httptest.NewRecorder()
	server.ServeHTTP(result, request)
	if result.Code != 200 || loginCalls != 1 || meCalls != 1 || proxyCalls != 1 {
		t.Fatalf("proxy=%d login=%d me=%d fs=%d", result.Code, loginCalls, meCalls, proxyCalls)
	}
}

func TestBrowserSlidingCookieAndLogout(t *testing.T) {
	server, originalStore := newTestServer(t)
	now := time.Now().UTC().Truncate(time.Second).Add(-23 * time.Hour)
	store := sqlite.NewStoreWithClock(originalStore.DB(), func() time.Time { return now })
	service := app.NewService(store, nil, nil, nil, nil)
	service.SetIdentityClient(&testidentity.Client{Store: store})
	server.application = service
	if err := store.SaveIdentitySetup(t.Context(), "http://openlist.local", "synthetic-key", domain.OpenListIdentity{ID: 1, Username: "alice", Role: 2}); err != nil {
		t.Fatal(err)
	}
	login := httptest.NewRecorder()
	req := requestJSON("POST", "/api/manager/auth/login", loginRequest{Username: "alice", Password: "password-123"})
	req.Header.Set("X-Forwarded-Proto", "https")
	server.ServeHTTP(login, req)
	if login.Code != 200 {
		t.Fatalf("login status=%d", login.Code)
	}
	cookie := login.Result().Cookies()[0]
	initialExpiry := cookie.Expires
	now = now.Add(23 * time.Hour)
	response := httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/manager/auth/me", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.AddCookie(cookie)
	server.ServeHTTP(response, req)
	if response.Code != 200 {
		t.Fatalf("auth status=%d", response.Code)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected refreshed cookie, got %d", len(cookies))
	}
	cookie = cookies[0]
	session, err := store.GetSession(t.Context(), cookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	if !cookie.Expires.Equal(session.ExpiresAt) || !cookie.Expires.After(initialExpiry) || cookie.MaxAge < 86390 || cookie.MaxAge > 86400 {
		t.Fatal("cookie expiry does not match renewed session")
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
		t.Fatal("cookie security attributes changed")
	}
	response = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/manager/auth/logout", nil)
	req.AddCookie(cookie)
	server.ServeHTTP(response, req)
	if response.Code != 200 || response.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout did not clear cookie")
	}
	if _, err := store.GetSession(t.Context(), cookie.Value); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("logout retained session", err)
	}
	response = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/manager/auth/me", nil)
	req.AddCookie(cookie)
	server.ServeHTTP(response, req)
	if response.Code != 401 || response.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("revoked cookie renewed")
	}
}
