package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
	"github.com/sevxn007/openlist2emby/backend/internal/testidentity"
)

func TestMkdirRequiresAdmin(t *testing.T) {
	server, store := newTestServer(t)
	if err := store.SaveInitialSettings(t.Context(), "http://openlist.local", "synthetic-integration-key"); err != nil {
		t.Fatal(err)
	}
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Movies", ContentType: medialibrary.ContentMovies, RootPath: "/Movies",
	})
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.CreateUser(t.Context(), domain.LocalUser{
		OpenListUserID: 102, InstanceID: "fixture-provider", Username: "media-viewer",
		Role: domain.LocalUserRoleMedia, Enabled: true,
		LibraryGrants: []domain.LibraryGrant{{LibraryID: library.ID, Access: domain.LibraryAccessView}},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := testidentity.Session(t.Context(), store, user.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	req := requestJSON(http.MethodPost, "/api/manager/openlist/fs/mkdir", map[string]string{"path": "/New"})
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestMkdirPreservesUpstreamErrorEnvelope(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(envelope{Code: http.StatusForbidden, Message: "permission denied"})
	}))
	t.Cleanup(upstream.Close)
	server, store := newTestServer(t)
	if err := store.SaveInitialSettings(t.Context(), upstream.URL, "synthetic-integration-key"); err != nil {
		t.Fatal(err)
	}
	session, err := createTestSession(t, store, t.Context(), "alice", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	req := requestJSON(http.MethodPost, "/api/manager/openlist/fs/mkdir", map[string]string{"path": "/New"})
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("proxy status = %d", recorder.Code)
	}
	result := decodeEnvelope(t, recorder)
	if result.Code != http.StatusForbidden || result.Message != "permission denied" {
		t.Fatalf("response = %#v", result)
	}
	if strings.Contains(recorder.Body.String(), "synthetic-integration-key") || strings.Contains(recorder.Body.String(), session.Token) {
		t.Fatal("response exposed credentials")
	}
}
