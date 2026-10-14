package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/repository/sqlite"
)

type MediaSearchResult = domain.MediaSearchResult
type MediaDetail = domain.MediaDetail
type MediaEpisode = domain.MediaEpisode

type fakeMediaError struct {
	status  int
	message string
	err     error
}

func (e *fakeMediaError) Error() string         { return e.message }
func (e *fakeMediaError) Unwrap() error         { return e.err }
func (e *fakeMediaError) StatusCode() int       { return e.status }
func (e *fakeMediaError) PublicMessage() string { return e.message }

type fakeMediaService struct {
	testFn   func(context.Context, string) error
	searchFn func(context.Context, string, string, string) ([]MediaSearchResult, error)
	detailFn func(context.Context, string, string, int64, *int, string) (MediaDetail, error)
}

func (f *fakeMediaService) Test(ctx context.Context, apiKey string) error {
	if f.testFn == nil {
		return nil
	}
	return f.testFn(ctx, apiKey)
}

func (f *fakeMediaService) Search(ctx context.Context, apiKey, query, language string) ([]MediaSearchResult, error) {
	if f.searchFn == nil {
		return []MediaSearchResult{}, nil
	}
	return f.searchFn(ctx, apiKey, query, language)
}

func (f *fakeMediaService) Detail(ctx context.Context, apiKey, mediaType string, id int64, season *int, language string) (MediaDetail, error) {
	if f.detailFn == nil {
		return MediaDetail{}, nil
	}
	return f.detailFn(ctx, apiKey, mediaType, id, season, language)
}

func newAuthenticatedMediaServer(t *testing.T, tmdbKey string) (*Server, *sqlite.Store, *http.Cookie, *fakeMediaService) {
	t.Helper()
	media := &fakeMediaService{}
	server, store := newTestServerWithMedia(t, media)
	if err := store.SaveInitialSettings(
		t.Context(),
		"http://openlist.local",
		"admin-key",
		tmdbKey,
	); err != nil {
		t.Fatal(err)
	}
	session, err := createTestSession(t, store, t.Context(), "alice", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return server, store, &http.Cookie{Name: sessionCookieName, Value: session.Token}, media
}

func TestMediaEndpointsRequireAuthentication(t *testing.T) {
	server, _ := newTestServer(t)
	paths := []string{
		"/api/manager/media/search?query=Dune",
		"/api/manager/media/detail?type=movie&id=1",
	}
	for _, path := range paths {
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want %d", path, recorder.Code, http.StatusUnauthorized)
		}
	}

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, requestJSON(http.MethodPost, "/api/manager/media/test", testMediaRequest{}))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("media test status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestMediaConnectionUsesSubmittedKeyWithoutPersistingIt(t *testing.T) {
	server, store, cookie, media := newAuthenticatedMediaServer(t, "stored-tmdb-key")
	var testedKey string
	media.testFn = func(_ context.Context, apiKey string) error {
		testedKey = apiKey
		return nil
	}

	recorder := httptest.NewRecorder()
	req := requestJSON(http.MethodPost, "/api/manager/media/test", testMediaRequest{TMDBAPIKey: " submitted-tmdb-key "})
	req.AddCookie(cookie)
	server.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("media test status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	if testedKey != "submitted-tmdb-key" {
		t.Fatalf("tested key = %q", testedKey)
	}
	settings, err := store.GetSettings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if settings.TMDBAPIKey != "stored-tmdb-key" {
		t.Fatalf("temporary key was persisted: %#v", settings)
	}
}

func TestSettingsUpdateSavesAndMasksTMDBKey(t *testing.T) {
	server, store, cookie, _ := newAuthenticatedMediaServer(t, "old-tmdb-key")
	recorder := httptest.NewRecorder()
	req := requestJSON(http.MethodPut, "/api/manager/settings", updateSettingsRequest{
		OpenListBaseURL: "http://openlist.local",
		TMDBAPIKey:      "replacement-tmdb-key",
	})
	req.AddCookie(cookie)
	server.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("settings update status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("replacement-tmdb-key")) {
		t.Fatalf("settings update exposed raw tmdb key: %s", recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"tmdb_api_key_set":true`)) || !bytes.Contains(recorder.Body.Bytes(), []byte(`"tmdb_api_key":"********"`)) {
		t.Fatalf("settings update did not return masked tmdb state: %s", recorder.Body.String())
	}
	settings, err := store.GetSettings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if settings.TMDBAPIKey != "replacement-tmdb-key" || settings.OpenListAdminKey != "admin-key" {
		t.Fatalf("settings were not persisted correctly: %#v", settings)
	}
}

func TestMediaConnectionFallsBackToStoredKey(t *testing.T) {
	server, _, cookie, media := newAuthenticatedMediaServer(t, "stored-tmdb-key")
	var testedKey string
	media.testFn = func(_ context.Context, apiKey string) error {
		testedKey = apiKey
		return nil
	}

	recorder := httptest.NewRecorder()
	req := requestJSON(http.MethodPost, "/api/manager/media/test", testMediaRequest{})
	req.AddCookie(cookie)
	server.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("media test status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	if testedKey != "stored-tmdb-key" {
		t.Fatalf("tested key = %q", testedKey)
	}
}

func TestMediaSearchForwardsQueryLanguageAndReturnsNormalizedResults(t *testing.T) {
	server, _, cookie, media := newAuthenticatedMediaServer(t, "stored-tmdb-key")
	media.searchFn = func(_ context.Context, apiKey, query, language string) ([]MediaSearchResult, error) {
		if apiKey != "stored-tmdb-key" || query != "沙丘" || language != "zh-CN" {
			t.Fatalf("unexpected search arguments: key=%q query=%q language=%q", apiKey, query, language)
		}
		return []MediaSearchResult{{
			ID:          438631,
			MediaType:   "movie",
			Title:       "沙丘",
			ReleaseDate: "2021-10-22",
			VoteAverage: 8.1,
		}}, nil
	}

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/manager/media/search?query=%E6%B2%99%E4%B8%98&language=zh", nil)
	req.AddCookie(cookie)
	server.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("media search status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"title":"沙丘"`)) {
		t.Fatalf("unexpected search response: %s", recorder.Body.String())
	}
}

func TestMediaDetailForwardsTVSeason(t *testing.T) {
	server, _, cookie, media := newAuthenticatedMediaServer(t, "stored-tmdb-key")
	media.detailFn = func(_ context.Context, apiKey, mediaType string, id int64, season *int, language string) (MediaDetail, error) {
		if apiKey != "stored-tmdb-key" || mediaType != "tv" || id != 95396 || season == nil || *season != 1 || language != "zh-CN" {
			t.Fatalf("unexpected detail arguments: key=%q type=%q id=%d season=%v language=%q", apiKey, mediaType, id, season, language)
		}
		seasonNumber := 1
		return MediaDetail{
			ID:           id,
			MediaType:    "tv",
			SeasonNumber: &seasonNumber,
			Episodes: []MediaEpisode{{
				ID:            1,
				Name:          "Good News About Hell",
				SeasonNumber:  1,
				EpisodeNumber: 1,
			}},
		}, nil
	}

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/manager/media/detail?type=tv&id=95396&season=1&language=en-US", nil)
	req.AddCookie(cookie)
	server.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("media detail status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"episode_number":1`)) {
		t.Fatalf("unexpected detail response: %s", recorder.Body.String())
	}
}

func TestMediaErrorsUseStableStatusesAndRedactSecrets(t *testing.T) {
	server, _, cookie, media := newAuthenticatedMediaServer(t, "stored-tmdb-key")
	media.testFn = func(_ context.Context, apiKey string) error {
		return &fakeMediaError{
			status:  http.StatusTooManyRequests,
			message: "rate limit for " + apiKey,
			err:     errors.New("rate limited"),
		}
	}

	recorder := httptest.NewRecorder()
	req := requestJSON(http.MethodPost, "/api/manager/media/test", testMediaRequest{})
	req.AddCookie(cookie)
	server.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("media test status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("stored-tmdb-key")) || !bytes.Contains(recorder.Body.Bytes(), []byte("[redacted]")) {
		t.Fatalf("media error did not redact the key: %s", recorder.Body.String())
	}
}

func TestMediaSearchRequiresConfiguredKey(t *testing.T) {
	server, _, cookie, media := newAuthenticatedMediaServer(t, "")
	media.searchFn = func(context.Context, string, string, string) ([]MediaSearchResult, error) {
		t.Fatal("media service should not be called without a key")
		return nil, nil
	}

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/manager/media/search?query=Dune", nil)
	req.AddCookie(cookie)
	server.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("media search status = %d body = %s", recorder.Code, recorder.Body.String())
	}
}
