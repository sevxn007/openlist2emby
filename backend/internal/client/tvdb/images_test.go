package tvdb

import (
	"net/http"
	"testing"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/metadata"
)

func TestImageCandidatesUseOfficialSeasonAndEpisode(t *testing.T) {
	client := clientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			respond(w, map[string]string{"token": "synthetic-token"})
		case "/series/10/extended", "/movies/10/extended":
			_, _ = w.Write([]byte(`{"status":"success","data":{"id":10,"artworks":[{"type":2,"image":"https://images.example/zh.jpg","language":"zho"},{"type":2,"image":"https://images.example/en.jpg","language":"eng"},{"type":3,"image":"https://images.example/back.jpg"}],"seasons":[{"id":99,"number":1,"type":{"type":"dvd"}},{"id":20,"number":1,"type":{"type":"official"}}]}}`))
		case "/artwork/types":
			respond(w, []map[string]any{{"id": 2, "name": "Poster"}, {"id": 3, "name": "Background"}, {"id": 4, "name": "Banner"}})
		case "/seasons/20/extended":
			_, _ = w.Write([]byte(`{"status":"success","data":{"id":20,"image":"https://images.example/season.jpg","imageType":2,"artwork":[{"type":2,"image":"https://images.example/season-alt.jpg"},{"type":4,"image":"https://images.example/banner.jpg"}],"episodes":[{"id":30,"number":2}]}}`))
		case "/episodes/30/extended":
			respond(w, map[string]any{"id": 30, "image": "https://images.example/episode.jpg"})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	season, episode := 1, 2
	for _, tc := range []struct {
		kind, imageType string
		season, episode *int
		count           int
		first           string
	}{
		{"movie", "primary", nil, nil, 2, "https://images.example/zh.jpg"},
		{"tv", "backdrop", nil, nil, 1, "https://images.example/back.jpg"},
		{"tv", "primary", &season, nil, 2, "https://images.example/season-alt.jpg"},
		{"tv", "backdrop", &season, nil, 0, ""},
		{"tv", "primary", &season, &episode, 1, "https://images.example/episode.jpg"},
		{"tv", "backdrop", &season, &episode, 0, ""},
	} {
		images, err := client.Images(t.Context(), metadata.Credentials{Key: "synthetic"}, domain.MediaImageQuery{Type: tc.kind, ID: 10, ImageType: tc.imageType, Season: tc.season, Episode: tc.episode})
		if err != nil || len(images) != tc.count || tc.count > 0 && images[0].URL != tc.first {
			t.Fatalf("query=%#v images=%#v error=%v", tc, images, err)
		}
	}
}
