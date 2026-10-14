package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
)

func TestManagerMediaContinuationWorkflow(t *testing.T) {
	server, store, cookie, _ := newAuthenticatedMediaLibraryServer(t)
	library, err := server.mediaLibraries.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "TV", ContentType: medialibrary.ContentTVShows, RootPath: "/TV",
	})
	if err != nil {
		t.Fatal(err)
	}
	series, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, Kind: medialibrary.KindSeries, Path: "/TV/Show", Name: "Show", SortName: "Show", ScanGeneration: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	seasonNumber := 1
	season, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, ParentID: &series.ID, Kind: medialibrary.KindSeason, Path: "/TV/Show/S1", Name: "Season 1", SortName: "1", SeasonNumber: &seasonNumber, ScanGeneration: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	episodes := make([]medialibrary.Item, 0, 2)
	for number := 1; number <= 2; number++ {
		episode, _, createErr := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
			LibraryID: library.ID, ParentID: &season.ID, Kind: medialibrary.KindEpisode,
			Path: fmt.Sprintf("/TV/Show/E%d.mp4", number), MediaPath: fmt.Sprintf("/TV/Show/E%d.mp4", number),
			Name: fmt.Sprintf("Episode %d", number), SortName: fmt.Sprint(number), Container: "mp4",
			SeasonNumber: &seasonNumber, EpisodeNumber: &number, RuntimeTicks: 10_000_000, ScanGeneration: 1,
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		episodes = append(episodes, episode)
	}

	request := requestJSON(http.MethodPut,
		"/api/manager/media-library/items/"+strconv.FormatInt(episodes[0].ID, 10)+"/played",
		playedUpdateRequest{Played: true},
	)
	request.AddCookie(cookie)
	marked := httptest.NewRecorder()
	server.ServeHTTP(marked, request)
	if marked.Code != http.StatusOK {
		t.Fatalf("mark played status = %d body = %s", marked.Code, marked.Body.String())
	}

	continueRequest := httptest.NewRequest(http.MethodGet, "/api/manager/media-library/continue-watching?start=0&limit=10", nil)
	continueRequest.AddCookie(cookie)
	continued := httptest.NewRecorder()
	server.ServeHTTP(continued, continueRequest)
	var continueResponse struct {
		Data medialibrary.ContinueWatchingPage `json:"data"`
	}
	if err := json.Unmarshal(continued.Body.Bytes(), &continueResponse); err != nil {
		t.Fatal(err)
	}
	if continued.Code != http.StatusOK || continueResponse.Data.Total != 1 || len(continueResponse.Data.Items) != 1 ||
		continueResponse.Data.Items[0].Item.ID != episodes[1].ID || continueResponse.Data.Items[0].Source != medialibrary.ContinueWatchingNextUp {
		t.Fatalf("continue watching status = %d response = %#v body = %s", continued.Code, continueResponse, continued.Body.String())
	}

	nextRequest := httptest.NewRequest(http.MethodGet,
		"/api/manager/media-library/items/"+strconv.FormatInt(episodes[0].ID, 10)+"/next-episode", nil)
	nextRequest.AddCookie(cookie)
	next := httptest.NewRecorder()
	server.ServeHTTP(next, nextRequest)
	var nextResponse struct {
		Data struct {
			Item *medialibrary.Item `json:"item"`
		} `json:"data"`
	}
	if err := json.Unmarshal(next.Body.Bytes(), &nextResponse); err != nil {
		t.Fatal(err)
	}
	if next.Code != http.StatusOK || nextResponse.Data.Item == nil || nextResponse.Data.Item.ID != episodes[1].ID {
		t.Fatalf("next episode status = %d response = %#v body = %s", next.Code, nextResponse, next.Body.String())
	}

	historical := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	if _, err := store.UpdateMediaUserDataForUser(t.Context(), 1, episodes[1].ID, medialibrary.UserData{
		PlaybackPositionTicks: 4_000_000, PlayCount: 3, Favorite: true, LastPlayedAt: &historical,
	}); err != nil {
		t.Fatal(err)
	}
	clearRequest := httptest.NewRequest(http.MethodDelete,
		"/api/manager/media-library/items/"+strconv.FormatInt(episodes[1].ID, 10)+"/resume", nil)
	clearRequest.AddCookie(cookie)
	cleared := httptest.NewRecorder()
	server.ServeHTTP(cleared, clearRequest)
	stored, err := store.GetMediaItemForUser(t.Context(), 1, episodes[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Code != http.StatusOK || stored.UserData.PlaybackPositionTicks != 0 || stored.UserData.PlayCount != 3 || !stored.UserData.Favorite || stored.UserData.LastPlayedAt == nil || !stored.UserData.LastPlayedAt.Equal(historical) {
		t.Fatalf("cleared resume status = %d data = %#v body = %s", cleared.Code, stored.UserData, cleared.Body.String())
	}
}
