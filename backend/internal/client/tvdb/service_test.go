package tvdb

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/metadata"
)

func clientForTest(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &Service{client: server.Client(), baseURL: server.URL, now: time.Now}
}
func respond(w http.ResponseWriter, data any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": data})
}
func TestTokenCacheAndOneUnauthorizedRetry(t *testing.T) {
	logins, requests := 0, 0
	c := clientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			logins++
			respond(w, map[string]string{"token": "synthetic-token"})
			return
		}
		requests++
		if requests == 1 {
			w.WriteHeader(401)
			return
		}
		respond(w, []any{})
	})
	creds := metadata.Credentials{Key: "synthetic-key", PIN: "synthetic-pin"}
	for range 2 {
		if _, err := c.Search(t.Context(), creds, "Show", "zh-CN"); err != nil {
			t.Fatal(err)
		}
	}
	if logins != 2 || requests != 3 {
		t.Fatalf("logins=%d requests=%d", logins, requests)
	}
}
func TestFailuresAreSanitizedAndBounded(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			c := clientForTest(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path == "/login" {
					respond(w, map[string]string{"token": "secret-token"})
					return
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte("secret-key secret-pin secret-token"))
			})
			_, err := c.Search(t.Context(), metadata.Credentials{Key: "secret-key", PIN: "secret-pin"}, "Show", "zh-CN")
			if err == nil || strings.Contains(err.Error(), "secret-") {
				t.Fatalf("unsafe error: %v", err)
			}
			if calls > 4 {
				t.Fatalf("unbounded retry %d", calls)
			}
			if status == 429 && !errors.Is(err, metadata.ErrRateLimited) {
				t.Fatal(err)
			}
		})
	}
}
func TestAiredOrderPaginationAndChineseFallback(t *testing.T) {
	pages := 0
	c := clientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			respond(w, map[string]string{"token": "token"})
		case "/series/72860/extended":
			respond(w, map[string]any{"id": 72860, "name": "Tom and Jerry", "firstAired": "1940-02-10", "seasons": []any{map[string]any{"id": 123, "number": 1940, "type": map[string]string{"type": "official"}}, map[string]any{"id": 999, "number": 1940, "type": map[string]string{"type": "dvd"}}}})
		case "/seasons/123/extended":
			respond(w, map[string]any{"id": 123, "number": 1940, "name": "Season 1940"})
		case "/series/72860/episodes/official":
			pages++
			if r.URL.Query().Get("season") != "1940" {
				t.Error("wrong season")
			}
			page := r.URL.Query().Get("page")
			ep := map[string]any{"id": 10, "seriesId": 72860, "seasonNumber": 1940, "number": 1, "name": "Original", "overview": "Original overview", "nameTranslations": []string{"zho"}}
			next := ""
			if page == "0" {
				next = "/untrusted/ignored?page=1"
			} else {
				ep = map[string]any{"id": 11, "seriesId": 72860, "seasonNumber": 1940, "number": 2, "name": "Second original"}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"episodes": []any{ep}}, "links": map[string]string{"next": next}})
		case "/episodes/10/translations/zho":
			respond(w, map[string]any{"name": "中文标题"})
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	number := 1940
	d, err := c.Detail(t.Context(), metadata.Credentials{Key: "synthetic"}, "tv", 72860, &number, "zh-CN")
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != 123 || pages != 2 || len(d.Episodes) != 2 || d.Episodes[0].Name != "中文标题" || d.Episodes[0].Overview != "Original overview" || d.Episodes[1].Name != "Second original" {
		t.Fatalf("detail=%#v pages=%d", d, pages)
	}
}
func TestMovieTranslationDoesNotTreatPopularityAsRating(t *testing.T) {
	c := clientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			respond(w, map[string]string{"token": "token"})
			return
		}
		respond(w, map[string]any{"id": 12, "name": "Original", "originalLanguage": "eng", "score": 123456, "translations": map[string]any{"nameTranslations": []any{map[string]string{"language": "zho", "name": "中文"}}, "overviewTranslations": []any{map[string]string{"language": "eng", "overview": "Original overview"}}}})
	})
	d, err := c.Detail(t.Context(), metadata.Credentials{Key: "synthetic"}, "movie", 12, nil, "zh-CN")
	if err != nil || d.Title != "中文" || d.Overview != "Original overview" || d.VoteAverage != 0 {
		t.Fatalf("%#v %v", d, err)
	}
}
