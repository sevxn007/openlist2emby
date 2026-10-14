package tmdbclient

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/metadata"
)

func TestImagesUsesCorrectEndpointAndAllLanguages(t *testing.T) {
	season, episode := 0, 2
	for _, tc := range []struct {
		name, path, kind, imageType string
		season, episode             *int
		count                       int
	}{
		{"movie", "/movie/42/images", "movie", "primary", nil, nil, 2},
		{"background", "/tv/42/images", "tv", "backdrop", nil, nil, 1},
		{"specials", "/tv/42/season/0/images", "tv", "primary", &season, nil, 2},
		{"episode", "/tv/42/season/0/episode/2/images", "tv", "primary", &season, &episode, 1},
		{"no season background", "", "tv", "backdrop", &season, nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path || r.URL.Query().Get("api_key") != "synthetic" || r.URL.Query().Has("language") {
					t.Errorf("unexpected request path or parameters")
				}
				_, _ = w.Write([]byte(`{"posters":[{"file_path":"/zh.jpg","iso_639_1":"zh","width":800,"height":1200},{"file_path":"/en.jpg","iso_639_1":"en"}],"backdrops":[{"file_path":"/background.jpg"}],"stills":[{"file_path":"/still.jpg"}]}`))
			}))
			defer server.Close()
			client := NewService()
			client.imageAPIBaseURL = server.URL
			got, err := client.Images(t.Context(), "synthetic", domain.MediaImageQuery{Type: tc.kind, ID: 42, ImageType: tc.imageType, Season: tc.season, Episode: tc.episode})
			if err != nil || len(got) != tc.count {
				t.Fatalf("images=%#v err=%v", got, err)
			}
			if tc.count == 2 && (got[0].Language != "zh" || got[0].Width != 800 || got[1].URL != "https://image.tmdb.org/t/p/original/en.jpg") {
				t.Fatalf("mapping=%#v", got)
			}
		})
	}
}

func TestImageErrorsDoNotExposeCredentials(t *testing.T) {
	for _, tc := range []struct {
		status int
		err    error
	}{{401, metadata.ErrAuthentication}, {429, metadata.ErrRateLimited}, {500, metadata.ErrUnavailable}, {404, domain.ErrNotFound}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte("synthetic-secret"))
		}))
		client := NewService()
		client.imageAPIBaseURL = server.URL
		_, err := client.Images(t.Context(), "synthetic-secret", domain.MediaImageQuery{Type: "movie", ID: 1, ImageType: "primary"})
		server.Close()
		if !errors.Is(err, tc.err) {
			t.Fatalf("status=%d error=%v", tc.status, err)
		}
	}
}
