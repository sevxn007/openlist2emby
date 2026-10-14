package emby_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
)

func TestPlayingItemEndpointsUpdateOnlyCurrentUserPlayback(t *testing.T) {
	server, store, _, item, _ := newEmbyTestServer(t)
	if _, err := store.UpdateMediaUserDataForUser(t.Context(), 1, item.ID, medialibrary.UserData{
		PlaybackPositionTicks: 2_000_000,
		PlayCount:             2,
		Favorite:              true,
	}); err != nil {
		t.Fatal(err)
	}
	auth := authenticateEmby(t, server)
	basePath := "/emby/Users/1/PlayingItems/" + strconv.FormatInt(item.ID, 10)

	request := func(method, suffix, contentType string, body []byte) {
		t.Helper()
		target := basePath + suffix
		separator := "?"
		if parsed, err := url.Parse(target); err == nil && parsed.RawQuery != "" {
			separator = "&"
		}
		httpRequest := httptest.NewRequest(method, target+separator+"api_key="+auth.AccessToken, bytes.NewReader(body))
		if contentType != "" {
			httpRequest.Header.Set("Content-Type", contentType)
		}
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httpRequest)
		if recorder.Code != http.StatusOK || recorder.Body.Len() != 0 {
			t.Fatalf("%s %s status = %d body = %s", method, suffix, recorder.Code, recorder.Body.String())
		}
	}
	readUserData := func() medialibrary.UserData {
		t.Helper()
		stored, err := store.GetMediaItemForUser(t.Context(), 1, item.ID)
		if err != nil {
			t.Fatal(err)
		}
		return stored.UserData
	}

	request(http.MethodPost, "", "", nil)
	if data := readUserData(); data.PlaybackPositionTicks != 2_000_000 || data.PlayCount != 2 || !data.Favorite || data.LastPlayedAt == nil {
		t.Fatalf("start state = %#v", data)
	}

	request(http.MethodPost, "/Progress", "application/json", []byte(`{"PositionTicks":5000000,"IsPaused":false}`))
	if data := readUserData(); data.PlaybackPositionTicks != 5_000_000 || data.PlayCount != 2 || !data.Favorite {
		t.Fatalf("JSON progress state = %#v", data)
	}

	form := url.Values{"PositionTicks": {"6000000"}}.Encode()
	request(http.MethodPost, "/Progress", "application/x-www-form-urlencoded", []byte(form))
	if data := readUserData(); data.PlaybackPositionTicks != 6_000_000 {
		t.Fatalf("form progress state = %#v", data)
	}

	request(http.MethodDelete, "?PositionTicks=9100000", "", nil)
	completed := readUserData()
	if completed.PlaybackPositionTicks != 0 || !completed.Played || completed.PlayCount != 3 || !completed.Favorite {
		t.Fatalf("completed state = %#v", completed)
	}

	request(http.MethodPost, "/Delete?PositionTicks=9500000", "", nil)
	repeated := readUserData()
	if repeated.PlaybackPositionTicks != 0 || !repeated.Played || repeated.PlayCount != 3 || !repeated.Favorite {
		t.Fatalf("repeated completion state = %#v", repeated)
	}
}

func TestPlayingItemEndpointsValidateUserPositionAndLibraryAccess(t *testing.T) {
	server, store, _, item, _ := newEmbyTestServer(t)
	privateLibrary, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Private", ContentType: medialibrary.ContentMovies, RootPath: "/Private",
	})
	if err != nil {
		t.Fatal(err)
	}
	generation, err := store.BeginLibraryScan(t.Context(), privateLibrary.ID)
	if err != nil {
		t.Fatal(err)
	}
	privateItem, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: privateLibrary.ID, Kind: medialibrary.KindMovie, Path: "/Private/Secret.mkv",
		MediaPath: "/Private/Secret.mkv", Name: "Secret", SortName: "Secret", Container: "mkv",
		RuntimeTicks: 10_000_000, Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishLibraryScan(t.Context(), privateLibrary.ID, generation); err != nil {
		t.Fatal(err)
	}
	bob, err := store.CreateUser(t.Context(), domain.LocalUser{OpenListUserID: 101, InstanceID: "test-instance",
		Username: "bob", Role: domain.LocalUserRoleMedia, Enabled: true,
		LibraryGrants: []domain.LibraryGrant{{LibraryID: item.LibraryID, Access: domain.LibraryAccessView}},
	})
	if err != nil {
		t.Fatal(err)
	}
	auth := authenticateEmbyAs(t, server, "bob", "bob-password", "bob-playing-items")
	userID := strconv.FormatInt(bob.ID, 10)

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{name: "invalid item ID", path: "/emby/Users/" + userID + "/PlayingItems/not-an-id", wantStatus: http.StatusBadRequest},
		{name: "invalid position", path: "/emby/Users/" + userID + "/PlayingItems/" + strconv.FormatInt(item.ID, 10) + "/Progress?PositionTicks=bad", wantStatus: http.StatusBadRequest},
		{name: "other user path", path: "/emby/Users/1/PlayingItems/" + strconv.FormatInt(item.ID, 10), wantStatus: http.StatusNotFound},
		{name: "unauthorized item", path: "/emby/Users/" + userID + "/PlayingItems/" + strconv.FormatInt(privateItem.ID, 10), wantStatus: http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			separator := "?"
			if parsed, parseErr := url.Parse(test.path); parseErr == nil && parsed.RawQuery != "" {
				separator = "&"
			}
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost,
				test.path+separator+"api_key="+auth.AccessToken, nil))
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d body = %s, want %d", recorder.Code, recorder.Body.String(), test.wantStatus)
			}
			if recorder.Code != http.StatusOK && recorder.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("error content type = %q", recorder.Header().Get("Content-Type"))
			}
		})
	}

	stored, err := store.GetMediaItemForUser(t.Context(), bob.ID, privateItem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if encoded, err := json.Marshal(stored.UserData); err != nil || string(encoded) != `{"playback_position_ticks":0,"played":false,"play_count":0,"favorite":false}` {
		t.Fatalf("unauthorized item state = %s err = %v", encoded, err)
	}
}
