package emby_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/emby"
)

func TestEmbyOriginalTokenBrowsesAfterOneDayAndRenews(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	server, store, remote, item, _ := newEmbyTestServerWithClock(t, func() time.Time { return now })
	auth := authenticateEmby(t, server)
	tokens, err := emby.NewSQLiteStore(t.Context(), store.DB())
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(auth.AccessToken))
	identity, valid, err := tokens.GetEmbyTokenIdentity(t.Context(), hex.EncodeToString(hash[:]))
	if err != nil || !valid {
		t.Fatalf("device token invalid: %v", err)
	}
	if err := store.DB().Exec("UPDATE sessions SET upstream_token=? WHERE token=?", "expired-synthetic-token", identity.IdentitySession).Error; err != nil {
		t.Fatal(err)
	}
	original, err := store.GetSession(t.Context(), identity.IdentitySession)
	if err != nil {
		t.Fatal(err)
	}
	if !original.ExpiresAt.Equal(now.Add(30 * 24 * time.Hour)) {
		t.Fatal("incorrect initial lifetime")
	}
	for _, advance := range []time.Duration{25 * time.Hour, 29 * 24 * time.Hour} {
		now = now.Add(advance)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/emby/Users/1/Views?api_key="+auth.AccessToken, nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"CollectionType":"movies"`) {
			t.Fatalf("browse failed: status=%d", response.Code)
		}
		items := httptest.NewRecorder()
		server.ServeHTTP(items, httptest.NewRequest(http.MethodGet, "/emby/Users/1/Items?Recursive=true&IncludeItemTypes=Movie&api_key="+auth.AccessToken, nil))
		if items.Code != http.StatusOK || !strings.Contains(items.Body.String(), `"Name":"Dune"`) {
			t.Fatalf("media list failed: status=%d", items.Code)
		}
		for _, path := range []string{"/emby/Items/" + strconv.FormatInt(item.ID, 10), "/emby/Items/" + strconv.FormatInt(item.ID, 10) + "/PlaybackInfo"} {
			detail := httptest.NewRecorder()
			server.ServeHTTP(detail, httptest.NewRequest(http.MethodGet, path+"?api_key="+auth.AccessToken, nil))
			if detail.Code != http.StatusOK {
				t.Fatalf("detail/playback status=%d", detail.Code)
			}
		}
		stream := httptest.NewRecorder()
		server.ServeHTTP(stream, httptest.NewRequest(http.MethodGet, "/emby/Videos/"+strconv.FormatInt(item.ID, 10)+"/stream.mkv?api_key="+auth.AccessToken, nil))
		if stream.Code != http.StatusTemporaryRedirect || stream.Header().Get("Location") != remote.downloadURL {
			t.Fatalf("stream status=%d", stream.Code)
		}

		session, err := store.GetSession(t.Context(), identity.IdentitySession)
		if err != nil || !session.ExpiresAt.Equal(now.Add(30*24*time.Hour)) {
			t.Fatalf("expiry not renewed: %v", err)
		}
	}
	now = now.Add(30 * 24 * time.Hour)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/emby/Users/1/Views?api_key="+auth.AccessToken, nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expired token status=%d", response.Code)
	}
}
