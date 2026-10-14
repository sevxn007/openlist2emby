package emby_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
)

func TestEmbyShowEpisodesReturnAllWhenLimitOmitted(t *testing.T) {
	server, store, _, _, _ := newEmbyTestServer(t)
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "TV", ContentType: medialibrary.ContentTVShows, RootPath: "/TV",
	})
	if err != nil {
		t.Fatal(err)
	}
	generation, err := store.BeginLibraryScan(t.Context(), library.ID)
	if err != nil {
		t.Fatal(err)
	}
	series, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, Kind: medialibrary.KindSeries, Path: "/TV/Show",
		Name: "Show", SortName: "Show", ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	seasonNumber := 1
	season, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, ParentID: &series.ID, Kind: medialibrary.KindSeason,
		Path: "/TV/Show/Season 01", Name: "Season 1", SortName: "0001",
		SeasonNumber: &seasonNumber, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	const episodeCount = 101
	for number := 1; number <= episodeCount; number++ {
		episodeNumber := number
		if _, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
			LibraryID: library.ID, ParentID: &season.ID, Kind: medialibrary.KindEpisode,
			Path:      fmt.Sprintf("/TV/Show/Season 01/S01E%03d.mkv", number),
			MediaPath: fmt.Sprintf("/TV/Show/Season 01/S01E%03d.mkv", number),
			Name:      fmt.Sprintf("Episode %d", number), SortName: fmt.Sprintf("%04d", number),
			SeasonNumber: &seasonNumber, EpisodeNumber: &episodeNumber, ScanGeneration: generation,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.FinishLibraryScan(t.Context(), library.ID, generation); err != nil {
		t.Fatal(err)
	}

	auth := authenticateEmby(t, server)
	basePath := "/emby/Shows/" + strconv.FormatInt(series.ID, 10) + "/Episodes?SeasonId=" +
		strconv.FormatInt(season.ID, 10) + "&api_key=" + auth.AccessToken
	type episodePage struct {
		Items []struct {
			ID string `json:"Id"`
		} `json:"Items"`
		TotalRecordCount int `json:"TotalRecordCount"`
	}
	list := func(path string) episodePage {
		t.Helper()
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		var page episodePage
		if recorder.Code != http.StatusOK || json.Unmarshal(recorder.Body.Bytes(), &page) != nil {
			t.Fatalf("episodes status = %d body = %s", recorder.Code, recorder.Body.String())
		}
		return page
	}

	all := list(basePath)
	if all.TotalRecordCount != episodeCount || len(all.Items) != episodeCount {
		t.Fatalf("omitted limit = total %d items %d, want %d", all.TotalRecordCount, len(all.Items), episodeCount)
	}

	limited := list(basePath + "&Limit=10")
	if limited.TotalRecordCount != episodeCount || len(limited.Items) != 10 {
		t.Fatalf("explicit limit = total %d items %d", limited.TotalRecordCount, len(limited.Items))
	}
}
