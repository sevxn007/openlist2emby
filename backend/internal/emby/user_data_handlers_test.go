package emby_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
)

func TestEmbyUserDataUpdate(t *testing.T) {
	server, _, _, item, _ := newEmbyTestServer(t)
	auth := authenticateEmby(t, server)
	itemID := strconv.FormatInt(item.ID, 10)
	endpoint := "/emby/Users/" + auth.User.Id + "/Items/" + itemID + "/UserData?api_key=" + auth.AccessToken

	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, endpoint,
		bytes.NewBufferString(`{"PlaybackPositionTicks":4200000,"Played":false,"PlayCount":2,"IsFavorite":true}`)))
	if response.Code != http.StatusOK {
		t.Fatalf("update status = %d body = %s", response.Code, response.Body.String())
	}
	var updated struct {
		ItemID                string `json:"ItemId"`
		PlaybackPositionTicks int64  `json:"PlaybackPositionTicks"`
		Played                bool   `json:"Played"`
		PlayCount             int    `json:"PlayCount"`
		IsFavorite            bool   `json:"IsFavorite"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.ItemID != itemID || updated.PlaybackPositionTicks != 4_200_000 || updated.Played || updated.PlayCount != 2 || !updated.IsFavorite {
		t.Fatalf("updated user data = %#v", updated)
	}

	partial := httptest.NewRecorder()
	server.ServeHTTP(partial, httptest.NewRequest(http.MethodPost, endpoint,
		bytes.NewBufferString(`{"PlaybackPositionTicks":5000000}`)))
	if partial.Code != http.StatusOK {
		t.Fatalf("partial update status = %d body = %s", partial.Code, partial.Body.String())
	}
	if err := json.Unmarshal(partial.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.PlaybackPositionTicks != 5_000_000 || updated.PlayCount != 2 || !updated.IsFavorite {
		t.Fatalf("partial update overwrote fields: %#v", updated)
	}
}

func TestEmbyAudioUserDataDoesNotSavePlaybackPosition(t *testing.T) {
	server, store, _, item, _ := newEmbyTestServer(t)
	generation, err := store.BeginLibraryScan(t.Context(), item.LibraryID)
	if err != nil {
		t.Fatal(err)
	}
	audio, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: item.LibraryID, Kind: medialibrary.KindAudio,
		Path: "/Movies/track.flac", MediaPath: "/Movies/track.flac",
		Name: "Track", SortName: "Track", Container: "flac", RuntimeTicks: 10_000_000,
		ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	auth := authenticateEmby(t, server)
	itemID := strconv.FormatInt(audio.ID, 10)
	endpoint := "/emby/Users/" + auth.User.Id + "/Items/" + itemID + "/UserData?api_key=" + auth.AccessToken
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, endpoint,
		bytes.NewBufferString(`{"PlaybackPositionTicks":4200000,"Played":false,"PlayCount":2,"IsFavorite":true}`)))
	if response.Code != http.StatusOK {
		t.Fatalf("update status = %d body = %s", response.Code, response.Body.String())
	}
	var updated struct {
		PlaybackPositionTicks int64 `json:"PlaybackPositionTicks"`
		PlayCount             int   `json:"PlayCount"`
		IsFavorite            bool  `json:"IsFavorite"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.PlaybackPositionTicks != 0 || updated.PlayCount != 2 || !updated.IsFavorite {
		t.Fatalf("audio user data = %#v", updated)
	}
	stored, err := store.GetMediaItemForUser(t.Context(), 1, audio.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.UserData.PlaybackPositionTicks != 0 {
		t.Fatalf("audio playback position persisted = %d", stored.UserData.PlaybackPositionTicks)
	}
	progress := httptest.NewRecorder()
	server.ServeHTTP(progress, httptest.NewRequest(http.MethodPost,
		"/emby/Sessions/Playing/Progress?api_key="+auth.AccessToken,
		bytes.NewBufferString(`{"ItemId":"`+itemID+`","PositionTicks":5000000}`)))
	if progress.Code != http.StatusNoContent {
		t.Fatalf("progress status = %d body = %s", progress.Code, progress.Body.String())
	}
	stored, err = store.GetMediaItemForUser(t.Context(), 1, audio.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.UserData.PlaybackPositionTicks != 0 {
		t.Fatalf("audio progress position persisted = %d", stored.UserData.PlaybackPositionTicks)
	}
}

func TestEmbyUserDataUpdateValidatesScopeAndValues(t *testing.T) {
	server, _, _, item, _ := newEmbyTestServer(t)
	auth := authenticateEmby(t, server)
	itemID := strconv.FormatInt(item.ID, 10)

	tests := []struct {
		name   string
		path   string
		body   string
		status int
	}{
		{name: "wrong user", path: "/emby/Users/999/Items/" + itemID + "/UserData", body: `{}`, status: http.StatusNotFound},
		{name: "negative position", path: "/emby/Users/" + auth.User.Id + "/Items/" + itemID + "/UserData", body: `{"PlaybackPositionTicks":-1}`, status: http.StatusBadRequest},
		{name: "invalid body", path: "/emby/Users/" + auth.User.Id + "/Items/" + itemID + "/UserData", body: `{`, status: http.StatusBadRequest},
		{name: "missing item", path: "/emby/Users/" + auth.User.Id + "/Items/999999/UserData", body: `{}`, status: http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, test.path+"?api_key="+auth.AccessToken, bytes.NewBufferString(test.body))
			server.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d, body = %s", response.Code, test.status, response.Body.String())
			}
		})
	}
}
