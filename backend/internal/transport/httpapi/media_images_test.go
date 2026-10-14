package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/app"
	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
	"github.com/sevxn007/openlist2emby/backend/internal/testidentity"
)

func TestRemoteSelectionReplacesUploadedImageCache(t *testing.T) {
	server, store, _, item := newAuthenticatedMediaLibraryServer(t)
	var original, replacement bytes.Buffer
	if err := png.Encode(&original, image.NewRGBA(image.Rect(0, 0, 10, 20))); err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(&replacement, image.NewRGBA(image.Rect(0, 0, 30, 40))); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(replacement.Bytes())
	}))
	defer upstream.Close()
	if err := store.UpdateSettings(t.Context(), upstream.URL, "", ""); err != nil {
		t.Fatal(err)
	}
	assets, err := medialibrary.NewAssetFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server.mediaLibraries.SetAssetFiles(assets)
	access := medialibrary.UserAccess{IsAdmin: true, UserID: 1}
	if _, err := server.mediaLibraries.UploadItemImageForUser(t.Context(), access, item.ID, medialibrary.ImagePrimary, bytes.NewReader(original.Bytes())); err != nil {
		t.Fatal(err)
	}
	if _, err := server.mediaLibraries.SetItemRemoteImageForUser(t.Context(), access, item.ID, "primary", upstream.URL+"/selected.png"); err != nil {
		t.Fatal(err)
	}
	stored, err := store.GetMediaItemImage(t.Context(), item.ID, medialibrary.ImagePrimary)
	if err != nil {
		t.Fatal(err)
	}
	if stored.SourceURL != upstream.URL+"/selected.png" || stored.SourceType != medialibrary.ImageSourceRemote || stored.Width != 30 || stored.Height != 40 || !assets.Exists(stored.RelativePath) {
		t.Fatalf("stored image=%#v", stored)
	}
}

type imageMediaService struct {
	fakeMediaService
	query    domain.MediaImageQuery
	imageErr error
}

func (m *imageMediaService) Images(_ context.Context, _ string, q domain.MediaImageQuery) ([]domain.MediaImage, error) {
	m.query = q
	return []domain.MediaImage{
		{URL: "https://image.tmdb.org/t/p/original/a.jpg", ThumbnailURL: "https://image.tmdb.org/t/p/w500/a.jpg"},
		{URL: "https://image.tmdb.org/t/p/original/b.jpg"},
	}, m.imageErr
}

func TestManagerImageCandidatesAndValidation(t *testing.T) {
	media := &imageMediaService{}
	server, store := newTestServerWithMedia(t, media)
	if err := store.SaveInitialSettings(t.Context(), "http://openlist.local", "admin-key", "synthetic"); err != nil {
		t.Fatal(err)
	}
	session, err := createTestSession(t, store, t.Context(), "alice", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: sessionCookieName, Value: session.Token}
	for _, tc := range []struct {
		query  string
		status int
	}{
		{"source=tmdb&type=tv&id=42&image_type=primary&season=0&episode=2", 200},
		{"source=tmdb&type=movie&id=42&image_type=primary&season=1", 400},
		{"source=tmdb&type=tv&id=42&image_type=primary&episode=2", 400},
		{"source=tmdb&type=tv&id=42&image_type=logo", 400},
		{"source=unknown&type=tv&id=42&image_type=primary", 400},
		{"source=tmdb&type=tv&id=0&image_type=primary", 400},
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/manager/media/images?"+tc.query, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Fatalf("query=%s status=%d body=%s", tc.query, rec.Code, rec.Body.String())
		}
		if tc.status == http.StatusOK {
			var response struct {
				Data struct{ Images []domain.MediaImage }
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if len(response.Data.Images) != 2 || media.query.Season == nil || *media.query.Season != 0 || media.query.Episode == nil || *media.query.Episode != 2 {
				t.Fatalf("response=%#v query=%#v", response, media.query)
			}
		}
	}
	media.imageErr = errors.New("synthetic-secret")
	req := httptest.NewRequest(http.MethodGet, "/api/manager/media/images?source=tmdb&type=movie&id=1&image_type=primary", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("upstream status %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "synthetic-secret") {
		t.Fatal("response exposed upstream error")
	}
}

func TestSelectedImagePreservesMetadataAndOtherImage(t *testing.T) {
	server, store, cookie, item := newAuthenticatedMediaLibraryServer(t)
	if err := store.UpdateSettings(t.Context(), "http://openlist.local", "", "synthetic"); err != nil {
		t.Fatal(err)
	}
	server.application = app.NewService(store, nil, &imageMediaService{}, nil, nil)
	tmdbID, tvdbID := int64(101), int64(202)
	if err := store.UpdateMediaItemMetadata(t.Context(), item.ID, medialibrary.MetadataInput{
		Name: "Original title", SortName: "Original sort", TMDBID: &tmdbID, TVDBID: &tvdbID,
		MetadataSource: "tmdb", ResolvedMetadataSource: "tmdb", Overview: "Original overview",
		BackdropURL: "https://images.example/backdrop.jpg", BackdropSourceURL: "https://images.example/backdrop.jpg",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().Table("media_items").Where("id = ?", item.ID).Update("metadata_locked", false).Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/manager/media-library/items/" + strconv.FormatInt(item.ID, 10) + "/images/primary/select"
	for _, tc := range []struct {
		url    string
		status int
	}{
		{"https://images.example/unlisted.jpg", http.StatusBadRequest},
		{"https://image.tmdb.org/t/p/original/a.jpg", http.StatusOK},
	} {
		req := requestJSON(http.MethodPost, path, map[string]any{"source": "tmdb", "type": "movie", "id": 999, "url": tc.url})
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
	stored, err := store.GetMediaItem(t.Context(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Name != "Original title" || stored.SortName != "Original sort" || stored.TMDBID == nil || *stored.TMDBID != 101 || stored.TVDBID == nil || *stored.TVDBID != 202 || stored.Overview != "Original overview" || stored.BackdropURL != "https://images.example/backdrop.jpg" || stored.ImageURL != "https://image.tmdb.org/t/p/original/a.jpg" || stored.MetadataLocked {
		t.Fatalf("unexpected saved item: %#v", stored)
	}
}

func TestImageSearchRequiresAdministrator(t *testing.T) {
	server, store, _, item := newAuthenticatedMediaLibraryServer(t)
	user, err := store.CreateUser(t.Context(), domain.LocalUser{
		OpenListUserID: 88, InstanceID: "test-instance", Username: "viewer", Role: domain.LocalUserRoleMedia,
		Enabled: true, LibraryGrants: []domain.LibraryGrant{{LibraryID: item.LibraryID, Access: domain.LibraryAccessEdit}},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := testidentity.Session(t.Context(), store, user.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/api/manager/media/images?source=tmdb&type=movie&id=1&image_type=primary",
		"/api/manager/media/image-sources",
	} {
		for _, authenticated := range []bool{false, true} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			if authenticated {
				req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
			}
			rec := httptest.NewRecorder()
			server.ServeHTTP(rec, req)
			expected := http.StatusUnauthorized
			if authenticated {
				expected = http.StatusForbidden
			}
			if rec.Code != expected {
				t.Fatalf("path=%s status=%d expected=%d", path, rec.Code, expected)
			}
		}
	}
	req := requestJSON(http.MethodPost, "/api/manager/media-library/items/1/images/primary/select", map[string]any{"source": "tmdb", "type": "movie", "id": 1, "url": "https://images.example/a.jpg"})
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("select status=%d", rec.Code)
	}
}
