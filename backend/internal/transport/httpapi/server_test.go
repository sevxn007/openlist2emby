package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	glebarezsqlite "github.com/glebarez/sqlite"
	"github.com/sevxn007/openlist2emby/backend/internal/app"
	"github.com/sevxn007/openlist2emby/backend/internal/audio/tagger"
	musicclient "github.com/sevxn007/openlist2emby/backend/internal/client/music"
	"github.com/sevxn007/openlist2emby/backend/internal/client/openlist"
	tmdbclient "github.com/sevxn007/openlist2emby/backend/internal/client/tmdb"
	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
	"github.com/sevxn007/openlist2emby/backend/internal/repository/sqlite"
	"github.com/sevxn007/openlist2emby/backend/internal/task"
	"github.com/sevxn007/openlist2emby/backend/internal/testidentity"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

const openListAdminRole = 2

type openListCurrentUserResponse struct {
	Username string `json:"username"`
	Role     int    `json:"role"`
}

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

type updateSettingsRequest = settingsRequest

func newTestServer(t *testing.T) (*Server, *sqlite.Store) {
	return newTestServerWithOpenListClient(t, openlist.NewClient(nil))
}

func newTestServerWithOpenListClient(t *testing.T, openList app.OpenListClient) (*Server, *sqlite.Store) {
	return newTestServerWithDependencies(t, openList, tmdbclient.NewService())
}

func newTestServerWithMedia(t *testing.T, media app.MediaService) (*Server, *sqlite.Store) {
	return newTestServerWithDependencies(t, openlist.NewClient(nil), media)
}

func newTestServerWithDependencies(t *testing.T, openList app.OpenListClient, media app.MediaService) (*Server, *sqlite.Store) {
	return newTestServerWithAllDependencies(
		t,
		openList,
		media,
		musicclient.NewClient(nil),
		tagger.New(),
	)
}

func newTestServerWithAllDependencies(
	t *testing.T,
	openList app.OpenListClient,
	media app.MediaService,
	music app.MusicService,
	audio app.AudioTagger,
) (*Server, *sqlite.Store) {
	t.Helper()
	db, err := gorm.Open(glebarezsqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Silent),
		TranslateError: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	store := sqlite.NewStoreWithClock(db, func() time.Time {
		return time.Date(2026, 7, 6, 8, 0, 0, 0, time.UTC)
	})
	if err := store.InitializeSchema(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	application := app.NewService(
		store,
		openList,
		media,
		music,
		audio,
	)
	application.SetIdentityClient(&testidentity.Client{Store: store})
	mediaScanHandler := medialibrary.NewTaskHandler(store, func(string, string) medialibrary.OpenList { return nil }, nil)
	registry, err := task.NewRegistry(app.NewMusicEmbedTaskHandler(application), mediaScanHandler)
	if err != nil {
		t.Fatal(err)
	}
	options := task.DefaultOptions()
	options.PollInterval = 5 * time.Millisecond
	manager, err := task.NewManager(store, registry, options)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = manager.Shutdown(ctx)
	})
	return NewServer(application, "", manager), store
}

func decodeEnvelope(t *testing.T, recorder *httptest.ResponseRecorder) envelope {
	t.Helper()
	var out envelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode envelope: %v\nbody: %s", err, recorder.Body.String())
	}
	return out
}

func requestJSON(method, path string, body any) *http.Request {
	var payload bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&payload).Encode(body)
	}
	req := httptest.NewRequest(method, path, &payload)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func createTestSession(t *testing.T, store *sqlite.Store, ctx context.Context, username string, ttl time.Duration) (domain.Session, error) {
	t.Helper()
	if _, err := store.GetSettings(ctx); errors.Is(err, domain.ErrNotSetup) {
		if err := store.SaveInitialSettings(ctx, "http://openlist.local", "admin-key"); err != nil {
			return domain.Session{}, err
		}
	}
	user, err := store.GetLocalUserByUsername(ctx, username)
	if errors.Is(err, domain.ErrNotFound) {
		user, err = store.CreateUser(ctx, domain.LocalUser{OpenListUserID: 101, InstanceID: "test-instance",
			Username: username, Role: domain.LocalUserRoleAdmin, Enabled: true,
		})
	}
	if err != nil {
		return domain.Session{}, err
	}
	return testidentity.Session(ctx, store, user.ID, ttl)
}

func TestManagerAPIRootReturnsJSONNotFound(t *testing.T) {
	server, _ := newTestServer(t)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/manager", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}
	response := decodeEnvelope(t, recorder)
	if response.Code != http.StatusNotFound || response.Message != "not found" {
		t.Fatalf("response = %#v", response)
	}
}

func TestStatusRequiresSetupBeforeSettingsExist(t *testing.T) {
	server, _ := newTestServer(t)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/manager/status", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	out := decodeEnvelope(t, recorder)
	data := out.Data.(map[string]any)
	if data["setup_required"] != true {
		t.Fatalf("setup_required = %#v", data["setup_required"])
	}
}

func TestSetupSucceedsOnce(t *testing.T) {
	server, _ := newTestServer(t)
	body := setupRequest{
		OpenListBaseURL:  "http://openlist.local/",
		OpenListAdminKey: "admin-key",
		Username:         "alice",
		Password:         "password-123",
	}

	first := httptest.NewRecorder()
	server.ServeHTTP(first, requestJSON(http.MethodPost, "/api/manager/setup", body))
	if first.Code != http.StatusOK {
		t.Fatalf("first setup status = %d body = %s", first.Code, first.Body.String())
	}

	second := httptest.NewRecorder()
	server.ServeHTTP(second, requestJSON(http.MethodPost, "/api/manager/setup", body))
	if second.Code != http.StatusConflict {
		t.Fatalf("second setup status = %d body = %s", second.Code, second.Body.String())
	}
}

func TestLoginStoresSessionAndSetsCookie(t *testing.T) {
	server, store := newTestServer(t)
	if err := store.SaveInitialSettings(t.Context(), "http://openlist.local", "admin-key"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateUser(t.Context(), domain.LocalUser{OpenListUserID: 102, InstanceID: "test-instance", Username: "alice", Role: domain.LocalUserRoleAdmin, Enabled: true}); err != nil {
		t.Fatal(err)
	}

	if err := testidentity.Bind(t.Context(), store, 1); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, requestJSON(http.MethodPost, "/api/manager/auth/login", loginRequest{
		Username: "alice",
		Password: "password-123",
	}))

	if recorder.Code != http.StatusOK {
		t.Fatalf("login status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "openlist2emby_session" || !cookies[0].HttpOnly {
		t.Fatalf("unexpected cookies: %#v", cookies)
	}
	session, err := store.GetSession(t.Context(), cookies[0].Value)
	if err != nil {
		t.Fatal(err)
	}
	if session.Username != "alice" {
		t.Fatalf("unexpected session: %#v", session)
	}
}

func TestLoginSetsSecureCookieBehindHTTPSProxy(t *testing.T) {
	server, store := newTestServer(t)
	if err := store.SaveInitialSettings(t.Context(), "http://openlist.local", "admin-key"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateUser(t.Context(), domain.LocalUser{OpenListUserID: 103, InstanceID: "test-instance", Username: "alice", Role: domain.LocalUserRoleAdmin, Enabled: true}); err != nil {
		t.Fatal(err)
	}

	if err := testidentity.Bind(t.Context(), store, 1); err != nil {
		t.Fatal(err)
	}
	request := requestJSON(http.MethodPost, "/api/manager/auth/login", loginRequest{
		Username: "alice",
		Password: "password-123",
	})
	request.Header.Set("X-Forwarded-Proto", "https")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("login status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookieName || !cookies[0].Secure {
		t.Fatalf("expected secure session cookie, got %#v", cookies)
	}
}

func TestFileListProxyUsesStoredAdminKey(t *testing.T) {
	openList := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/fs/list" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "admin-key" {
			t.Fatalf("Authorization = %q", got)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req["path"] != "/Movies" {
			t.Fatalf("path = %#v", req["path"])
		}
		_ = json.NewEncoder(w).Encode(envelope{
			Code:    http.StatusOK,
			Message: "success",
			Data: map[string]any{
				"content": []any{},
				"total":   0,
			},
		})
	}))
	t.Cleanup(openList.Close)

	server, store := newTestServer(t)
	if err := store.SaveInitialSettings(t.Context(), openList.URL, "admin-key"); err != nil {
		t.Fatal(err)
	}
	session, err := createTestSession(t, store, t.Context(), "alice", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	req := requestJSON(http.MethodPost, "/api/manager/openlist/fs/list", map[string]any{"path": "/Movies"})
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("proxy status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	out := decodeEnvelope(t, recorder)
	if out.Code != http.StatusOK {
		t.Fatalf("proxied envelope code = %d", out.Code)
	}
}

func TestFileOperationProxyForwardsPayloadAndToken(t *testing.T) {
	openList := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/fs/move" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "admin-key" {
			t.Fatalf("Authorization = %q", got)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req["src_dir"] != "/" || req["dst_dir"] != "/Archive" {
			t.Fatalf("unexpected move request: %#v", req)
		}
		_ = json.NewEncoder(w).Encode(envelope{
			Code:    http.StatusOK,
			Message: "success",
			Data:    map[string]any{"message": "created"},
		})
	}))
	t.Cleanup(openList.Close)

	server, store := newTestServer(t)
	if err := store.SaveInitialSettings(t.Context(), openList.URL, "admin-key"); err != nil {
		t.Fatal(err)
	}
	session, err := createTestSession(t, store, t.Context(), "alice", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	req := requestJSON(http.MethodPost, "/api/manager/openlist/fs/move", map[string]any{
		"src_dir": "/",
		"dst_dir": "/Archive",
		"names":   []string{"Movie.mkv"},
	})
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("proxy status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	out := decodeEnvelope(t, recorder)
	if out.Code != http.StatusOK {
		t.Fatalf("proxied envelope code = %d", out.Code)
	}
}

func TestSettingsNeverExposeRawAdminKey(t *testing.T) {
	server, store := newTestServer(t)
	if err := store.SaveInitialSettings(
		t.Context(),
		"http://openlist.local",
		"super-secret-key",
		"tmdb-secret-key",
	); err != nil {
		t.Fatal(err)
	}
	session, err := createTestSession(t, store, t.Context(), "alice", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/manager/settings", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("settings status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("super-secret-key")) {
		t.Fatalf("settings exposed raw admin key: %s", recorder.Body.String())
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("tmdb-secret-key")) {
		t.Fatalf("settings exposed raw tmdb key: %s", recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte("********")) {
		t.Fatalf("settings did not include masked key: %s", recorder.Body.String())
	}
}

func TestSettingsUpdatePersistsAndValidatesEmbyPort(t *testing.T) {
	server, store := newTestServer(t)
	if err := store.SaveInitialSettings(t.Context(), "http://openlist.local", "admin-key"); err != nil {
		t.Fatal(err)
	}
	session, err := createTestSession(t, store, t.Context(), "alice", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	enabled := true
	port := 18096
	update := requestJSON(http.MethodPut, "/api/manager/settings", settingsRequest{
		OpenListBaseURL: "http://openlist.local", MediaLibraryEnabled: &enabled, EmbyPort: &port,
	})
	update.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
	updated := httptest.NewRecorder()
	server.ServeHTTP(updated, update)
	if updated.Code != http.StatusOK || !bytes.Contains(updated.Body.Bytes(), []byte(`"emby_port":18096`)) {
		t.Fatalf("update status = %d body = %s", updated.Code, updated.Body.String())
	}
	settings, err := store.GetSettings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !settings.MediaLibraryEnabled || settings.EmbyPort != port {
		t.Fatalf("stored settings = %#v", settings)
	}

	invalidPort := 70000
	invalid := requestJSON(http.MethodPut, "/api/manager/settings", settingsRequest{
		OpenListBaseURL: "http://openlist.local", EmbyPort: &invalidPort,
	})
	invalid.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
	rejected := httptest.NewRecorder()
	server.ServeHTTP(rejected, invalid)
	if rejected.Code != http.StatusBadRequest {
		t.Fatalf("invalid port status = %d body = %s", rejected.Code, rejected.Body.String())
	}
	settings, err = store.GetSettings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if settings.EmbyPort != port {
		t.Fatalf("invalid update changed port to %d", settings.EmbyPort)
	}
}

func TestSettingsConnectionRequiresAuthentication(t *testing.T) {
	server, _ := newTestServer(t)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, requestJSON(http.MethodPost, "/api/manager/settings/test", updateSettingsRequest{
		OpenListBaseURL:  "http://openlist.local",
		OpenListAdminKey: "admin-key",
	}))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("test connection status = %d body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestSettingsConnectionUsesSubmittedValuesWithoutPersistingThem(t *testing.T) {
	openList := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/me" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "submitted-key" {
			t.Fatalf("Authorization = %q", got)
		}
		_ = json.NewEncoder(w).Encode(envelope{
			Code:    http.StatusOK,
			Message: "success",
			Data: openListCurrentUserResponse{
				Username: "admin",
				Role:     openListAdminRole,
			},
		})
	}))
	t.Cleanup(openList.Close)

	server, store := newTestServer(t)
	if err := store.SaveInitialSettings(t.Context(), "http://stored.openlist.local", "stored-key"); err != nil {
		t.Fatal(err)
	}
	session, err := createTestSession(t, store, t.Context(), "alice", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	req := requestJSON(http.MethodPost, "/api/manager/settings/test", updateSettingsRequest{
		OpenListBaseURL:  openList.URL + "/",
		OpenListAdminKey: " submitted-key ",
	})
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("test connection status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	settings, err := store.GetSettings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if settings.OpenListBaseURL != "http://stored.openlist.local" || settings.OpenListAdminKey != "stored-key" {
		t.Fatalf("test connection persisted settings: %#v", settings)
	}
}

func TestSettingsConnectionUsesStoredAdminKeyWhenSubmittedKeyIsBlank(t *testing.T) {
	openList := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "stored-key" {
			t.Fatalf("Authorization = %q", got)
		}
		_ = json.NewEncoder(w).Encode(envelope{
			Code: http.StatusOK,
			Data: openListCurrentUserResponse{Username: "admin", Role: openListAdminRole},
		})
	}))
	t.Cleanup(openList.Close)

	server, store := newTestServer(t)
	if err := store.SaveInitialSettings(t.Context(), openList.URL, "stored-key"); err != nil {
		t.Fatal(err)
	}
	session, err := createTestSession(t, store, t.Context(), "alice", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	req := requestJSON(http.MethodPost, "/api/manager/settings/test", updateSettingsRequest{
		OpenListBaseURL: openList.URL,
	})
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("test connection status = %d body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestSettingsConnectionRejectsNonAdminKey(t *testing.T) {
	openList := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(envelope{
			Code: http.StatusOK,
			Data: openListCurrentUserResponse{Username: "regular-user", Role: 0},
		})
	}))
	t.Cleanup(openList.Close)

	server, store := newTestServer(t)
	if err := store.SaveInitialSettings(t.Context(), openList.URL, "stored-key"); err != nil {
		t.Fatal(err)
	}
	session, err := createTestSession(t, store, t.Context(), "alice", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	req := requestJSON(http.MethodPost, "/api/manager/settings/test", updateSettingsRequest{
		OpenListBaseURL: openList.URL,
	})
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("test connection status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte("administrator key is required")) {
		t.Fatalf("unexpected error: %s", recorder.Body.String())
	}
}

func TestSettingsConnectionReturnsOpenListRejection(t *testing.T) {
	openList := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(envelope{
			Code:    http.StatusUnauthorized,
			Message: "invalid token",
		})
	}))
	t.Cleanup(openList.Close)

	server, store := newTestServer(t)
	if err := store.SaveInitialSettings(t.Context(), openList.URL, "stored-key"); err != nil {
		t.Fatal(err)
	}
	session, err := createTestSession(t, store, t.Context(), "alice", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	req := requestJSON(http.MethodPost, "/api/manager/settings/test", updateSettingsRequest{
		OpenListBaseURL: openList.URL,
	})
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("test connection status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte("invalid token")) {
		t.Fatalf("unexpected error: %s", recorder.Body.String())
	}
}

func TestSettingsConnectionReturnsBadGatewayForTransportFailure(t *testing.T) {
	server, store := newTestServerWithOpenListClient(t, openlist.NewClient(&http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network unavailable")
		}),
	}))
	if err := store.SaveInitialSettings(t.Context(), "http://openlist.local", "stored-key"); err != nil {
		t.Fatal(err)
	}
	session, err := createTestSession(t, store, t.Context(), "alice", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	req := requestJSON(http.MethodPost, "/api/manager/settings/test", updateSettingsRequest{
		OpenListBaseURL: "http://openlist.local",
	})
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("test connection status = %d body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestBulkFileOperationProxiesRequireAuthentication(t *testing.T) {
	server, _ := newTestServer(t)
	paths := []string{
		"/api/manager/openlist/fs/mkdir",
		"/api/manager/openlist/fs/remove",
		"/api/manager/openlist/fs/batch_rename",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, requestJSON(http.MethodPost, path, map[string]any{}))

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestBulkFileOperationProxiesForwardPayloadAndToken(t *testing.T) {
	tests := []struct {
		name         string
		managerPath  string
		openListPath string
		payload      map[string]any
		assertBody   func(*testing.T, map[string]any)
	}{
		{
			name:         "mkdir",
			managerPath:  "/api/manager/openlist/fs/mkdir",
			openListPath: "/api/fs/mkdir",
			payload:      map[string]any{"path": "/Movies/New folder"},
			assertBody: func(t *testing.T, body map[string]any) {
				t.Helper()
				if len(body) != 1 || body["path"] != "/Movies/New folder" {
					t.Fatalf("mkdir body = %#v", body)
				}
			},
		},
		{
			name:         "remove",
			managerPath:  "/api/manager/openlist/fs/remove",
			openListPath: "/api/fs/remove",
			payload: map[string]any{
				"dir":   "/Movies",
				"names": []string{"One.mkv", "Two.mkv"},
			},
			assertBody: func(t *testing.T, body map[string]any) {
				t.Helper()
				if body["dir"] != "/Movies" {
					t.Fatalf("dir = %#v", body["dir"])
				}
				names, ok := body["names"].([]any)
				if !ok || len(names) != 2 || names[0] != "One.mkv" || names[1] != "Two.mkv" {
					t.Fatalf("names = %#v", body["names"])
				}
			},
		},
		{
			name:         "batch rename",
			managerPath:  "/api/manager/openlist/fs/batch_rename",
			openListPath: "/api/fs/batch_rename",
			payload: map[string]any{
				"src_dir": "/Movies",
				"rename_objects": []map[string]string{
					{"src_name": "One.mkv", "new_name": "Episode-01.mkv"},
					{"src_name": "Two.mkv", "new_name": "Episode-02.mkv"},
				},
			},
			assertBody: func(t *testing.T, body map[string]any) {
				t.Helper()
				if body["src_dir"] != "/Movies" {
					t.Fatalf("src_dir = %#v", body["src_dir"])
				}
				objects, ok := body["rename_objects"].([]any)
				if !ok || len(objects) != 2 {
					t.Fatalf("rename_objects = %#v", body["rename_objects"])
				}
				first, ok := objects[0].(map[string]any)
				if !ok || first["src_name"] != "One.mkv" || first["new_name"] != "Episode-01.mkv" {
					t.Fatalf("rename object = %#v", objects[0])
				}
				second, ok := objects[1].(map[string]any)
				if !ok || second["src_name"] != "Two.mkv" || second["new_name"] != "Episode-02.mkv" {
					t.Fatalf("rename object = %#v", objects[1])
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			openList := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != test.openListPath {
					t.Fatalf("path = %s", r.URL.Path)
				}
				if got := r.Header.Get("Authorization"); got != "admin-key" {
					t.Fatalf("Authorization = %q", got)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				test.assertBody(t, body)
				_ = json.NewEncoder(w).Encode(envelope{
					Code:    http.StatusOK,
					Message: "success",
					Data:    nil,
				})
			}))
			t.Cleanup(openList.Close)

			server, store := newTestServer(t)
			if err := store.SaveInitialSettings(t.Context(), openList.URL, "admin-key"); err != nil {
				t.Fatal(err)
			}
			session, err := createTestSession(t, store, t.Context(), "alice", time.Hour)
			if err != nil {
				t.Fatal(err)
			}

			recorder := httptest.NewRecorder()
			req := requestJSON(http.MethodPost, test.managerPath, test.payload)
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
			server.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}
