package emby_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
)

func TestEmbyPlayedItemsCascadeSeriesAndSeason(t *testing.T) {
	server, store, _, _, _ := newEmbyTestServer(t)
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{Name: "TV", ContentType: medialibrary.ContentTVShows, RootPaths: []string{"/TV"}})
	if err != nil {
		t.Fatal(err)
	}
	generation, err := store.BeginLibraryScan(t.Context(), library.ID)
	if err != nil {
		t.Fatal(err)
	}
	series, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{LibraryID: library.ID, Kind: medialibrary.KindSeries, Path: "/TV/Show", Name: "Show", SortName: "Show", ScanGeneration: generation})
	if err != nil {
		t.Fatal(err)
	}
	seasons := make([]medialibrary.Item, 0, 2)
	episodes := make([]medialibrary.Item, 0, 2)
	for number := 1; number <= 2; number++ {
		season, _, createErr := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{LibraryID: library.ID, ParentID: &series.ID, Kind: medialibrary.KindSeason, Path: fmt.Sprintf("/TV/Show/S%d", number), Name: fmt.Sprintf("Season %d", number), SortName: fmt.Sprintf("%04d", number), SeasonNumber: &number, ScanGeneration: generation})
		if createErr != nil {
			t.Fatal(createErr)
		}
		episodeNumber := 1
		episode, _, episodeErr := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{LibraryID: library.ID, ParentID: &season.ID, Kind: medialibrary.KindEpisode, Path: fmt.Sprintf("/TV/Show/S%d/E1.mkv", number), Name: "Episode 1", SortName: "0001", SeasonNumber: &number, EpisodeNumber: &episodeNumber, ScanGeneration: generation})
		if episodeErr != nil {
			t.Fatal(episodeErr)
		}
		seasons, episodes = append(seasons, season), append(episodes, episode)
	}
	if _, err := store.FinishLibraryScan(t.Context(), library.ID, generation); err != nil {
		t.Fatal(err)
	}
	auth := authenticateEmby(t, server)
	requestAs := func(method, path string, body *bytes.Reader, userAgent string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		separator := "?"
		if strings.Contains(path, "?") {
			separator = "&"
		}
		var request *http.Request
		if body == nil {
			request = httptest.NewRequest(method, path+separator+"api_key="+auth.AccessToken, nil)
		} else {
			request = httptest.NewRequest(method, path+separator+"api_key="+auth.AccessToken, body)
		}
		request.Header.Set("User-Agent", userAgent)
		server.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s %s status = %d body = %s", method, path, recorder.Code, recorder.Body.String())
		}
		return recorder
	}
	request := func(method, path string, body *bytes.Reader) *httptest.ResponseRecorder {
		return requestAs(method, path, body, "")
	}
	vidHubRequest := func(method, path string, body *bytes.Reader) *httptest.ResponseRecorder {
		return requestAs(method, path, body, "VidHub/2.3.5")
	}
	playedPath := func(id int64) string {
		return "/emby/Users/" + auth.User.Id + "/PlayedItems/" + strconv.FormatInt(id, 10)
	}
	seasonsResponse := request(http.MethodGet, "/emby/Shows/"+strconv.FormatInt(series.ID, 10)+"/Seasons", nil)
	var listed struct {
		Items []struct {
			ID       string `json:"Id"`
			SeriesID string `json:"SeriesId"`
			UserData struct {
				Played bool `json:"Played"`
			} `json:"UserData"`
		} `json:"Items"`
	}
	if err := json.Unmarshal(seasonsResponse.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Items) != 2 || listed.Items[0].ID == listed.Items[1].ID {
		t.Fatalf("season identities = %#v", listed.Items)
	}
	for _, season := range listed.Items {
		if season.SeriesID != strconv.FormatInt(series.ID, 10) {
			t.Fatalf("season hierarchy = %#v", listed.Items)
		}
	}
	episodesResponse := request(http.MethodGet, "/emby/Shows/"+strconv.FormatInt(series.ID, 10)+"/Episodes?SeasonId="+strconv.FormatInt(seasons[0].ID, 10), nil)
	var listedEpisodes struct {
		Items []struct {
			SeasonID string `json:"SeasonId"`
			SeriesID string `json:"SeriesId"`
		} `json:"Items"`
	}
	if err := json.Unmarshal(episodesResponse.Body.Bytes(), &listedEpisodes); err != nil {
		t.Fatal(err)
	}
	if len(listedEpisodes.Items) != 1 || listedEpisodes.Items[0].SeasonID != strconv.FormatInt(seasons[0].ID, 10) || listedEpisodes.Items[0].SeriesID != strconv.FormatInt(series.ID, 10) {
		t.Fatalf("episode hierarchy = %#v", listedEpisodes.Items)
	}

	request(http.MethodPost, playedPath(series.ID)+"?SeasonId="+strconv.FormatInt(seasons[0].ID, 10), nil)
	seasonStatesResponse := request(http.MethodGet, "/emby/Shows/"+strconv.FormatInt(series.ID, 10)+"/Seasons", nil)
	if err := json.Unmarshal(seasonStatesResponse.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	playedBySeason := make(map[string]bool, len(listed.Items))
	for _, season := range listed.Items {
		playedBySeason[season.ID] = season.UserData.Played
	}
	if !playedBySeason[strconv.FormatInt(seasons[0].ID, 10)] || playedBySeason[strconv.FormatInt(seasons[1].ID, 10)] {
		t.Fatalf("season response played states = %#v", playedBySeason)
	}
	seasonEpisode, _ := store.GetMediaItemForUser(t.Context(), 1, episodes[0].ID)
	otherEpisode, _ := store.GetMediaItemForUser(t.Context(), 1, episodes[1].ID)
	seriesAfterSeason, _ := store.GetMediaItemForUser(t.Context(), 1, series.ID)
	if !seasonEpisode.UserData.Played || otherEpisode.UserData.Played || seriesAfterSeason.UserData.Played {
		t.Fatalf("season cascade states: first=%#v second=%#v series=%#v", seasonEpisode.UserData, otherEpisode.UserData, seriesAfterSeason.UserData)
	}

	request(http.MethodPost, "/emby/Users/"+auth.User.Id+"/Items/"+strconv.FormatInt(series.ID, 10)+"/UserData?SeasonId="+strconv.FormatInt(seasons[0].ID, 10), bytes.NewReader([]byte(`{"Played":false}`)))
	seasonAfterUserData, _ := store.GetMediaItemForUser(t.Context(), 1, episodes[0].ID)
	otherAfterUserData, _ := store.GetMediaItemForUser(t.Context(), 1, episodes[1].ID)
	if seasonAfterUserData.UserData.Played || otherAfterUserData.UserData.Played {
		t.Fatalf("VidHub UserData current-season states: first=%#v other=%#v", seasonAfterUserData.UserData, otherAfterUserData.UserData)
	}

	vidHubRequest(http.MethodGet, "/emby/Shows/"+strconv.FormatInt(series.ID, 10)+"/Episodes?SeasonId="+strconv.FormatInt(seasons[1].ID, 10), nil)
	vidHubRequest(http.MethodPost, playedPath(series.ID), nil)
	seasonOneFromVidHub, _ := store.GetMediaItemForUser(t.Context(), 1, episodes[0].ID)
	seasonTwoFromVidHub, _ := store.GetMediaItemForUser(t.Context(), 1, episodes[1].ID)
	if seasonOneFromVidHub.UserData.Played || !seasonTwoFromVidHub.UserData.Played {
		t.Fatalf("VidHub current-season states: first=%#v second=%#v", seasonOneFromVidHub.UserData, seasonTwoFromVidHub.UserData)
	}
	vidHubRequest(http.MethodGet, "/emby/Shows/"+strconv.FormatInt(series.ID, 10)+"/Episodes?SeasonId="+strconv.FormatInt(seasons[1].ID, 10), nil)
	vidHubRequest(http.MethodPost, playedPath(series.ID)+"/Delete", nil)
	seasonTwoUnplayed, _ := store.GetMediaItemForUser(t.Context(), 1, episodes[1].ID)
	if seasonTwoUnplayed.UserData.Played {
		t.Fatalf("VidHub current-season delete state: %#v", seasonTwoUnplayed.UserData)
	}

	vidHubRequest(http.MethodGet, "/emby/Shows/"+strconv.FormatInt(series.ID, 10)+"/Episodes?SeasonId="+strconv.FormatInt(seasons[0].ID, 10), nil)
	request(http.MethodPost, playedPath(series.ID), nil)
	for _, item := range append(append([]medialibrary.Item{}, seasons...), episodes...) {
		stored, _ := store.GetMediaItemForUser(t.Context(), 1, item.ID)
		if !stored.UserData.Played {
			t.Fatalf("series cascade did not mark item %d", item.ID)
		}
	}

	request(http.MethodPost, playedPath(series.ID)+"/Delete?SeasonId="+strconv.FormatInt(seasons[0].ID, 10), nil)
	firstAfter, _ := store.GetMediaItemForUser(t.Context(), 1, episodes[0].ID)
	otherAfter, _ := store.GetMediaItemForUser(t.Context(), 1, episodes[1].ID)
	if firstAfter.UserData.Played || !otherAfter.UserData.Played {
		t.Fatalf("VidHub current-season delete states: first=%#v other=%#v", firstAfter.UserData, otherAfter.UserData)
	}

	userDataPath := "/emby/Users/" + auth.User.Id + "/Items/" + strconv.FormatInt(seasons[1].ID, 10) + "/UserData"
	request(http.MethodPost, userDataPath, bytes.NewReader([]byte(`{"Played":false}`)))
	secondAfter, _ := store.GetMediaItemForUser(t.Context(), 1, episodes[1].ID)
	seriesAfterUnplayed, _ := store.GetMediaItemForUser(t.Context(), 1, series.ID)
	if secondAfter.UserData.Played || seriesAfterUnplayed.UserData.Played {
		t.Fatalf("UserData season unplayed did not cascade: episode=%#v series=%#v", secondAfter.UserData, seriesAfterUnplayed.UserData)
	}
}
