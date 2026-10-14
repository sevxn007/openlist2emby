package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/app"
	"github.com/sevxn007/openlist2emby/backend/internal/client/openlist"
	tmdbclient "github.com/sevxn007/openlist2emby/backend/internal/client/tmdb"
	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

type fakeMusicService struct {
	searchFn  func(context.Context, string, string, int) ([]domain.MusicTrack, error)
	detailFn  func(context.Context, string, string, string) (domain.MusicDetail, error)
	resolveFn func(context.Context, string, string, string) (domain.MusicAssets, error)
}

func (f *fakeMusicService) Search(ctx context.Context, query, source string, limit int) ([]domain.MusicTrack, error) {
	if f.searchFn == nil {
		return []domain.MusicTrack{}, nil
	}
	return f.searchFn(ctx, query, source, limit)
}

func (f *fakeMusicService) Resolve(ctx context.Context, source, pictureID, lyricID string) (domain.MusicAssets, error) {
	if f.resolveFn == nil {
		return domain.MusicAssets{}, nil
	}
	return f.resolveFn(ctx, source, pictureID, lyricID)
}

func (f *fakeMusicService) Detail(ctx context.Context, source, pictureID, lyricID string) (domain.MusicDetail, error) {
	if f.detailFn == nil {
		return domain.MusicDetail{}, nil
	}
	return f.detailFn(ctx, source, pictureID, lyricID)
}

type fakeAudioTagger struct {
	supported bool
	embedFn   func(string, domain.AudioMetadata) error
}

func (f *fakeAudioTagger) Supports(string) bool {
	return f.supported
}

func (f *fakeAudioTagger) Embed(path string, metadata domain.AudioMetadata) error {
	if f.embedFn == nil {
		return nil
	}
	return f.embedFn(path, metadata)
}

func TestMusicEndpointsRequireAuthentication(t *testing.T) {
	server, _ := newTestServer(t)
	requests := []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/manager/music/search?query=song", nil),
		httptest.NewRequest(http.MethodGet, "/api/manager/music/detail?picture_id=1", nil),
		requestJSON(http.MethodPost, "/api/manager/music/embed", musicEmbedRequest{}),
	}
	for _, request := range requests {
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d", request.URL.Path, recorder.Code)
		}
	}
}

func TestMusicSearchAndDetailUseMusicService(t *testing.T) {
	music := &fakeMusicService{}
	music.searchFn = func(_ context.Context, query, source string, limit int) ([]domain.MusicTrack, error) {
		if query != "晴天" || source != "netease" || limit != 20 {
			t.Fatalf("unexpected search: query=%q source=%q limit=%d", query, source, limit)
		}
		return []domain.MusicTrack{{
			ID:        "1",
			Name:      "晴天",
			Artists:   []string{"周杰伦"},
			PictureID: "pic-1",
			LyricID:   "lyric-1",
			Source:    "netease",
		}}, nil
	}
	music.detailFn = func(_ context.Context, source, pictureID, lyricID string) (domain.MusicDetail, error) {
		if source != "netease" || pictureID != "pic-1" || lyricID != "lyric-1" {
			t.Fatalf("unexpected detail: source=%q picture=%q lyric=%q", source, pictureID, lyricID)
		}
		return domain.MusicDetail{
			CoverURL: "https://images.example/cover.jpg",
			Lyrics:   "[00:01.00]Lyrics",
		}, nil
	}
	server, store := newTestServerWithAllDependencies(
		t,
		openlist.NewClient(nil),
		tmdbclient.NewService(),
		music,
		&fakeAudioTagger{supported: true},
	)
	if err := store.SaveInitialSettings(t.Context(), "http://openlist.local", "admin-key"); err != nil {
		t.Fatal(err)
	}
	session, err := createTestSession(t, store, t.Context(), "alice", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: sessionCookieName, Value: session.Token}

	searchRecorder := httptest.NewRecorder()
	searchRequest := httptest.NewRequest(http.MethodGet, "/api/manager/music/search?query=%E6%99%B4%E5%A4%A9&source=netease", nil)
	searchRequest.AddCookie(cookie)
	server.ServeHTTP(searchRecorder, searchRequest)
	if searchRecorder.Code != http.StatusOK || !bytes.Contains(searchRecorder.Body.Bytes(), []byte(`"name":"晴天"`)) {
		t.Fatalf("search response = %d %s", searchRecorder.Code, searchRecorder.Body.String())
	}

	detailRecorder := httptest.NewRecorder()
	detailRequest := httptest.NewRequest(http.MethodGet, "/api/manager/music/detail?source=netease&picture_id=pic-1&lyric_id=lyric-1", nil)
	detailRequest.AddCookie(cookie)
	server.ServeHTTP(detailRecorder, detailRequest)
	if detailRecorder.Code != http.StatusOK || !bytes.Contains(detailRecorder.Body.Bytes(), []byte(`"lyrics":"[00:01.00]Lyrics"`)) {
		t.Fatalf("detail response = %d %s", detailRecorder.Code, detailRecorder.Body.String())
	}
}

func TestEmbedMusicDownloadsTagsAndOverwritesOpenListFile(t *testing.T) {
	originalAudio := []byte("original-audio")
	var uploaded []byte
	var openListServer *httptest.Server
	openListServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "admin-key" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/api/fs/get":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200,
				"data": map[string]any{
					"name":    "song.mp3",
					"size":    len(originalAudio),
					"is_dir":  false,
					"raw_url": openListServer.URL + "/raw/song.mp3",
				},
			})
		case "/raw/song.mp3":
			_, _ = w.Write(originalAudio)
		case "/api/fs/put":
			if r.Header.Get("Overwrite") != "true" {
				t.Fatalf("Overwrite = %q", r.Header.Get("Overwrite"))
			}
			var err error
			uploaded, err = io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": nil})
		default:
			http.NotFound(w, r)
		}
	}))
	defer openListServer.Close()

	music := &fakeMusicService{resolveFn: func(_ context.Context, source, pictureID, lyricID string) (domain.MusicAssets, error) {
		if source != "netease" || pictureID != "picture-1" || lyricID != "lyric-1" {
			t.Fatalf("unexpected music assets request: %q %q %q", source, pictureID, lyricID)
		}
		return domain.MusicAssets{
			CoverData: []byte{0xff, 0xd8, 0xff, 0xdb},
			CoverMIME: "image/jpeg",
			Detail: domain.MusicDetail{
				Lyrics: "[00:01.00]Lyrics",
			},
		}, nil
	}}
	audio := &fakeAudioTagger{
		supported: true,
		embedFn: func(path string, metadata domain.AudioMetadata) error {
			if string(metadata.Cover) != string([]byte{0xff, 0xd8, 0xff, 0xdb}) || metadata.Lyrics != "[00:01.00]Lyrics" {
				t.Fatalf("metadata = %#v", metadata)
			}
			file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				return err
			}
			defer file.Close()
			_, err = file.WriteString("-embedded")
			return err
		},
	}
	server, store := newTestServerWithAllDependencies(
		t,
		openlist.NewClient(openListServer.Client()),
		tmdbclient.NewService(),
		music,
		audio,
	)
	if err := store.SaveInitialSettings(t.Context(), openListServer.URL, "admin-key"); err != nil {
		t.Fatal(err)
	}
	session, err := createTestSession(t, store, t.Context(), "alice", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	request := requestJSON(http.MethodPost, "/api/manager/music/embed", musicEmbedRequest{
		Path:        "/Music/song.mp3",
		Source:      "netease",
		PictureID:   "picture-1",
		LyricID:     "lyric-1",
		EmbedCover:  true,
		EmbedLyrics: true,
	})
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("embed response = %d %s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Equal(uploaded, append(originalAudio, []byte("-embedded")...)) {
		t.Fatalf("uploaded = %q", uploaded)
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"embedded_cover":true`)) ||
		!bytes.Contains(recorder.Body.Bytes(), []byte(`"embedded_lyrics":true`)) {
		t.Fatalf("embed response = %s", recorder.Body.String())
	}

	streamRecorder := httptest.NewRecorder()
	streamRequest := requestJSON(http.MethodPost, "/api/manager/music/embed", musicEmbedRequest{
		Path:        "/Music/song.mp3",
		Source:      "netease",
		PictureID:   "picture-1",
		LyricID:     "lyric-1",
		EmbedCover:  true,
		EmbedLyrics: true,
	})
	streamRequest.Header.Set("Accept", "text/event-stream")
	streamRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
	server.ServeHTTP(streamRecorder, streamRequest)

	if streamRecorder.Code != http.StatusOK {
		t.Fatalf("stream response = %d %s", streamRecorder.Code, streamRecorder.Body.String())
	}
	if contentType := streamRecorder.Header().Get("Content-Type"); contentType != "text/event-stream" {
		t.Fatalf("Content-Type = %q", contentType)
	}
	streamBody := streamRecorder.Body.String()
	for _, expected := range []string{
		`event: progress`,
		`: connected`,
		`"stage":"downloading"`,
		`"stage":"embedding"`,
		`"stage":"uploading"`,
		`"stage":"completed","percent":100`,
		`event: complete`,
		`"embedded_cover":true`,
	} {
		if !strings.Contains(streamBody, expected) {
			t.Fatalf("stream response missing %q: %s", expected, streamBody)
		}
	}
}

func TestEmbedMusicPreservesExecutionErrorStatus(t *testing.T) {
	music := &fakeMusicService{resolveFn: func(context.Context, string, string, string) (domain.MusicAssets, error) {
		return domain.MusicAssets{}, nil
	}}
	server, store := newTestServerWithAllDependencies(
		t,
		openlist.NewClient(nil),
		tmdbclient.NewService(),
		music,
		&fakeAudioTagger{supported: true},
	)
	if err := store.SaveInitialSettings(t.Context(), "http://openlist.local", "admin-key"); err != nil {
		t.Fatal(err)
	}
	session, err := createTestSession(t, store, t.Context(), "local-admin", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	request := requestJSON(http.MethodPost, "/api/manager/music/embed", musicEmbedRequest{
		Path:       "/Music/song.mp3",
		Source:     "netease",
		PictureID:  "missing-cover",
		EmbedCover: true,
	})
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte("cover image is unavailable")) {
		t.Fatalf("response = %s", recorder.Body.String())
	}
}

func TestEmbedMusicRejectsUnsupportedFileBeforeDownload(t *testing.T) {
	music := &fakeMusicService{resolveFn: func(context.Context, string, string, string) (domain.MusicAssets, error) {
		t.Fatal("music service should not be called")
		return domain.MusicAssets{}, nil
	}}
	server, store := newTestServerWithAllDependencies(
		t,
		openlist.NewClient(nil),
		tmdbclient.NewService(),
		music,
		&fakeAudioTagger{supported: false},
	)
	if err := store.SaveInitialSettings(t.Context(), "http://openlist.local", "admin-key"); err != nil {
		t.Fatal(err)
	}
	session, err := createTestSession(t, store, t.Context(), "alice", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	request := requestJSON(http.MethodPost, "/api/manager/music/embed", musicEmbedRequest{
		Path:       "/video.mkv",
		PictureID:  "picture-1",
		EmbedCover: true,
	})
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
}

var _ app.MusicService = (*fakeMusicService)(nil)
var _ app.AudioTagger = (*fakeAudioTagger)(nil)
