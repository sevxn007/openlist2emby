package openlistidentity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLoginOTPAndIdentityWhitelist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			if payload["username"] != "alice" || payload["password"] != "synthetic-password" || payload["otp_code"] != "123456" {
				t.Errorf("unexpected login payload")
			}
			if r.Header.Get("Authorization") != "" {
				t.Error("login used administrator key")
			}
			_, _ = w.Write([]byte(`{"code":200,"data":{"token":"synthetic-user-token"}}`))
		case "/api/me":
			if r.Header.Get("Authorization") != "synthetic-user-token" {
				t.Error("wrong user token")
			}
			_, _ = w.Write([]byte(`{"code":200,"data":{"id":42,"username":"alice","role":0,"otp":true,"password":"secret-field","salt":"secret-salt","base_path":"private-path"}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	token, user, err := New().Login(t.Context(), server.URL, "alice", "synthetic-password", "123456")
	if err != nil || token != "synthetic-user-token" || user.ID != 42 || !user.OTP {
		t.Fatalf("login failed: %v", err)
	}
	encoded, _ := json.Marshal(user)
	for _, secret := range []string{"secret-field", "secret-salt", "private-path", "synthetic-user-token", "password"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("identity leaks private fields")
		}
	}
}
func TestIdentityFailureMapping(t *testing.T) {
	cases := []struct {
		name, body string
		want       int
	}{
		{"password", `{"code":401,"message":"password is incorrect: synthetic-secret"}`, 401},
		{"otp", `{"code":402,"message":"synthetic-secret"}`, 401},
		{"invalid token", `{"code":401,"message":"synthetic-secret"}`, 401},
		{"rate limit", `{"code":429}`, 429},
		{"upstream", `{"code":500,"message":"synthetic-secret"}`, 503},
		{"malformed", `not-json synthetic-secret`, 503},
		{"guest", `{"code":200,"data":{"id":2,"username":"guest","role":1}}`, 401},
		{"disabled", `{"code":200,"data":{"id":2,"username":"alice","disabled":true}}`, 401},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tc.body)) }))
			defer server.Close()
			var err error
			if tc.name == "password" {
				_, _, err = New().Login(t.Context(), server.URL, "alice", "synthetic-password", "")
			} else {
				_, err = New().Current(t.Context(), server.URL, "synthetic-token")
			}
			var mapped *Error
			if !errors.As(err, &mapped) || mapped.Code != tc.want {
				t.Fatalf("got %v, want %d", err, tc.want)
			}
			if strings.Contains(err.Error(), "synthetic") {
				t.Fatal("error leaked secret")
			}
		})
	}
}
func TestTimeoutRedirectAndIDMismatch(t *testing.T) {
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("redirect leaked credentials") }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 302) }))
	defer server.Close()
	if _, err := New().Current(t.Context(), server.URL, "synthetic-token"); err == nil {
		t.Fatal("redirect accepted")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Nanosecond)
	defer cancel()
	if _, err := New().Current(ctx, server.URL, "synthetic-token"); err == nil {
		t.Fatal("expired context accepted")
	}
	mismatch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":200,"data":{"id":9,"username":"wrong"}}`))
	}))
	defer mismatch.Close()
	if _, err := New().User(t.Context(), mismatch.URL, "synthetic-key", 8); err == nil {
		t.Fatal("mismatched identity accepted")
	}
}

func TestAdminUserPaginationWhitelist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/admin/user/list" || r.URL.Query().Get("page") != "2" || r.URL.Query().Get("per_page") != "10" || r.Header.Get("Authorization") != "synthetic-admin-key" {
			t.Error("incorrect administrator pagination request")
		}
		_, _ = w.Write([]byte(`{"code":200,"data":{"content":[{"id":42,"username":"alice","role":0,"disabled":true,"password":"private-password","otp_secret":"private-otp","token":"private-token"}],"total":21}}`))
	}))
	defer server.Close()
	page, err := New().Users(t.Context(), server.URL, "synthetic-admin-key", 2, 10)
	if err != nil || page.Total != 21 || len(page.Content) != 1 || !page.Content[0].Disabled {
		t.Fatalf("pagination: %+v %v", page, err)
	}
	encoded, _ := json.Marshal(page)
	if strings.Contains(string(encoded), "private-") || strings.Contains(string(encoded), "password") || strings.Contains(string(encoded), "token") {
		t.Fatal("administrator lookup leaked upstream secrets")
	}
}
