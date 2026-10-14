package emby_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
)

func TestEmbyLatestTVSeriesSortsByNewestImportedEpisode(t *testing.T) {
	server, store, _, _, _ := newEmbyTestServer(t)
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "TV", ContentType: medialibrary.ContentTVShows, RootPath: "/TV",
	})
	if err != nil {
		t.Fatal(err)
	}
	createSeries := func(name string) (medialibrary.Item, medialibrary.Item) {
		t.Helper()
		series, _, createErr := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
			LibraryID: library.ID, Kind: medialibrary.KindSeries, Path: "/TV/" + name,
			Name: name, SortName: name, ScanGeneration: 1,
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		seasonNumber := 1
		season, _, createErr := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
			LibraryID: library.ID, ParentID: &series.ID, Kind: medialibrary.KindSeason,
			Path: "/TV/" + name + "/Season 01", Name: "Season 1", SortName: "0001",
			SeasonNumber: &seasonNumber, ScanGeneration: 1,
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		return series, season
	}
	newestSeries, newestSeason := createSeries("Newest Import")
	olderSeries, olderSeason := createSeries("Older Import")
	createEpisode := func(series, season medialibrary.Item, importedAt time.Time) {
		t.Helper()
		seasonNumber, episodeNumber := 1, 1
		episode, _, createErr := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
			LibraryID: library.ID, ParentID: &season.ID, Kind: medialibrary.KindEpisode,
			Path:      "/TV/" + series.Name + "/Season 01/S01E01.mkv",
			MediaPath: "/TV/" + series.Name + "/Season 01/S01E01.mkv",
			Name:      "Episode 1", SortName: "0001", SeasonNumber: &seasonNumber,
			EpisodeNumber: &episodeNumber, ScanGeneration: 1,
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		if result := store.DB().Exec("UPDATE media_items SET created_at = ? WHERE id = ?", importedAt.UTC().Format(time.RFC3339Nano), episode.ID); result.Error != nil {
			t.Fatal(result.Error)
		}
	}
	createEpisode(newestSeries, newestSeason, time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC))
	createEpisode(olderSeries, olderSeason, time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC))

	auth := authenticateEmby(t, server)
	viewID := strconv.FormatInt(8_000_000_000_000_000_000+library.ID, 10)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet,
		"/emby/Users/1/Items/Latest?ParentId="+viewID+"&IncludeItemTypes=Episode&Limit=20&api_key="+auth.AccessToken, nil))
	var items []struct {
		ID   string `json:"Id"`
		Type string `json:"Type"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode latest items: %v body = %s", err, recorder.Body.String())
	}
	if recorder.Code != http.StatusOK || len(items) != 2 || items[0].ID != strconv.FormatInt(newestSeries.ID, 10) ||
		items[1].ID != strconv.FormatInt(olderSeries.ID, 10) || items[0].Type != "Series" || items[1].Type != "Series" {
		t.Fatalf("latest TV series status = %d items = %#v body = %s", recorder.Code, items, recorder.Body.String())
	}
}
