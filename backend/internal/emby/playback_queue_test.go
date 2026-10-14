package emby_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
	"github.com/sevxn007/openlist2emby/backend/internal/repository/sqlite"
)

type playbackQueueEpisodes struct {
	seriesID       int64
	first          medialibrary.Item
	second         medialibrary.Item
	third          medialibrary.Item
	seasonTwoFirst medialibrary.Item
	last           medialibrary.Item
}

func createPlaybackQueueShow(t *testing.T, store *sqlite.Store) (medialibrary.Library, playbackQueueEpisodes) {
	t.Helper()
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
		LibraryID: library.ID, Kind: medialibrary.KindSeries, Path: "/TV/Show", Name: "Show", SortName: "Show",
		Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	seasonOneNumber := 1
	seasonOne, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, ParentID: &series.ID, Kind: medialibrary.KindSeason, Path: "/TV/Show/Season 1", Name: "Season 1", SortName: "0001",
		SeasonNumber: &seasonOneNumber, Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	seasonTwoNumber := 2
	seasonTwo, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, ParentID: &series.ID, Kind: medialibrary.KindSeason, Path: "/TV/Show/Season 2", Name: "Season 2", SortName: "0002",
		SeasonNumber: &seasonTwoNumber, Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	createEpisode := func(season medialibrary.Item, seasonNumber, episodeNumber int) medialibrary.Item {
		t.Helper()
		episode, _, createErr := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
			LibraryID: library.ID, ParentID: &season.ID, Kind: medialibrary.KindEpisode,
			Path:      "/TV/Show/S" + strconv.Itoa(seasonNumber) + "E" + strconv.Itoa(episodeNumber) + ".mkv",
			MediaPath: "/TV/Show/S" + strconv.Itoa(seasonNumber) + "E" + strconv.Itoa(episodeNumber) + ".mkv",
			Name:      "Episode " + strconv.Itoa(seasonNumber) + "." + strconv.Itoa(episodeNumber), SortName: "Episode", Container: "mkv",
			RuntimeTicks: 10_000_000, SeasonNumber: &seasonNumber, EpisodeNumber: &episodeNumber,
			Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		return episode
	}
	episodes := playbackQueueEpisodes{
		seriesID:       series.ID,
		first:          createEpisode(seasonOne, 1, 1),
		second:         createEpisode(seasonOne, 1, 2),
		third:          createEpisode(seasonOne, 1, 3),
		seasonTwoFirst: createEpisode(seasonTwo, 2, 1),
		last:           createEpisode(seasonTwo, 2, 2),
	}
	if _, err := store.FinishLibraryScan(t.Context(), library.ID, generation); err != nil {
		t.Fatal(err)
	}
	return library, episodes
}

func TestPlayingItemCompletionDerivesNextEpisode(t *testing.T) {
	var media *medialibrary.Service
	server, store, _, movie, _ := newEmbyTestServer(t, func(service *medialibrary.Service) {
		media = service
	})
	library, episodes := createPlaybackQueueShow(t, store)
	alice := medialibrary.UserAccess{UserID: 1, IsAdmin: true}
	allEpisodes := []medialibrary.Item{episodes.first, episodes.second, episodes.third, episodes.seasonTwoFirst, episodes.last}
	resetPlayback := func(userID int64) {
		t.Helper()
		for _, episode := range allEpisodes {
			if _, err := store.UpdateMediaUserDataForUser(t.Context(), userID, episode.ID, medialibrary.UserData{}); err != nil {
				t.Fatal(err)
			}
		}
	}
	readData := func(userID, itemID int64) medialibrary.UserData {
		t.Helper()
		item, err := store.GetMediaItemForUser(t.Context(), userID, itemID)
		if err != nil {
			t.Fatal(err)
		}
		return item.UserData
	}
	complete := func(access medialibrary.UserAccess, item medialibrary.Item) {
		t.Helper()
		if _, err := media.UpdatePlaybackForUser(t.Context(), access, item.ID, medialibrary.PlaybackUpdate{
			PositionTicks: 9_000_000, Stopped: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	nextUp := func(access medialibrary.UserAccess, seriesID int64) medialibrary.Item {
		t.Helper()
		page, err := media.ListNextUpForUser(t.Context(), access, medialibrary.NextUpQuery{
			SeriesID: &seriesID, EnableResumable: true, Limit: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) == 0 {
			return medialibrary.Item{}
		}
		return page.Items[0]
	}
	assertNotQueued := func(userID int64, item medialibrary.Item) {
		t.Helper()
		if data := readData(userID, item.ID); data.PlaybackPositionTicks != 0 || data.LastPlayedAt != nil {
			t.Fatalf("episode %d was queued: %#v", item.ID, data)
		}
	}

	t.Run("exposes the derived episode through continue watching without fake progress", func(t *testing.T) {
		resetPlayback(1)
		complete(alice, episodes.first)
		if data := readData(1, episodes.first.ID); !data.Played || data.PlaybackPositionTicks != 0 || data.PlayCount != 1 {
			t.Fatalf("completed episode = %#v", data)
		}
		assertNotQueued(1, episodes.second)
		if got := nextUp(alice, episodes.seriesID); got.ID != episodes.second.ID {
			t.Fatalf("next up = %#v", got)
		}
		auth := authenticateEmby(t, server)
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet,
			"/emby/Users/1/Items/Resume?IncludeItemTypes=Episode&api_key="+auth.AccessToken, nil))
		var response struct {
			Items []struct {
				ID       string `json:"Id"`
				UserData struct {
					PlaybackPositionTicks int64 `json:"PlaybackPositionTicks"`
				} `json:"UserData"`
			} `json:"Items"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if recorder.Code != http.StatusOK || len(response.Items) != 1 || response.Items[0].ID != strconv.FormatInt(episodes.second.ID, 10) || response.Items[0].UserData.PlaybackPositionTicks != 0 {
			t.Fatalf("resume status = %d items = %#v body = %s", recorder.Code, response.Items, recorder.Body.String())
		}
	})

	t.Run("continues with next season", func(t *testing.T) {
		resetPlayback(1)
		complete(alice, episodes.third)
		assertNotQueued(1, episodes.seasonTwoFirst)
		if got := nextUp(alice, episodes.seriesID); got.ID != episodes.seasonTwoFirst.ID {
			t.Fatalf("next season episode = %#v", got)
		}
	})

	t.Run("skips watched subsequent episodes", func(t *testing.T) {
		resetPlayback(1)
		if _, err := store.UpdateMediaUserDataForUser(t.Context(), 1, episodes.second.ID, medialibrary.UserData{Played: true}); err != nil {
			t.Fatal(err)
		}
		complete(alice, episodes.first)
		assertNotQueued(1, episodes.second)
		assertNotQueued(1, episodes.third)
		if got := nextUp(alice, episodes.seriesID); got.ID != episodes.third.ID {
			t.Fatalf("first later unplayed episode = %#v", got)
		}
	})

	t.Run("preserves existing next episode progress", func(t *testing.T) {
		resetPlayback(1)
		historical := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
		want := medialibrary.UserData{PlaybackPositionTicks: 2_000_000, PlayCount: 4, Favorite: true, LastPlayedAt: &historical}
		if _, err := store.UpdateMediaUserDataForUser(t.Context(), 1, episodes.second.ID, want); err != nil {
			t.Fatal(err)
		}
		complete(alice, episodes.first)
		got := readData(1, episodes.second.ID)
		if got.PlaybackPositionTicks != want.PlaybackPositionTicks || got.PlayCount != want.PlayCount || got.Favorite != want.Favorite || got.LastPlayedAt == nil || !got.LastPlayedAt.Equal(historical) {
			t.Fatalf("existing progress was replaced: %#v", got)
		}
		assertNotQueued(1, episodes.third)
	})

	t.Run("does not queue outside a completed episode", func(t *testing.T) {
		resetPlayback(1)
		if _, err := media.UpdatePlaybackForUser(t.Context(), alice, episodes.first.ID, medialibrary.PlaybackUpdate{PositionTicks: 9_000_000}); err != nil {
			t.Fatal(err)
		}
		assertNotQueued(1, episodes.second)

		resetPlayback(1)
		if _, err := media.UpdatePlaybackForUser(t.Context(), alice, episodes.first.ID, medialibrary.PlaybackUpdate{PositionTicks: 8_999_999, Stopped: true}); err != nil {
			t.Fatal(err)
		}
		assertNotQueued(1, episodes.second)

		resetPlayback(1)
		if _, err := media.SetPlayedForUser(t.Context(), alice, episodes.first.ID, true); err != nil {
			t.Fatal(err)
		}
		assertNotQueued(1, episodes.second)

		resetPlayback(1)
		complete(alice, episodes.last)
		assertNotQueued(1, episodes.first)

		resetPlayback(1)
		complete(alice, movie)
		assertNotQueued(1, episodes.first)
	})

	t.Run("keeps queue state isolated to the viewing user", func(t *testing.T) {
		resetPlayback(1)
		bob, err := store.CreateUser(t.Context(), domain.LocalUser{OpenListUserID: 101, InstanceID: "test-instance",
			Username: "bob", Role: domain.LocalUserRoleMedia, Enabled: true,
			LibraryGrants: []domain.LibraryGrant{{LibraryID: library.ID, Access: domain.LibraryAccessView}},
		})
		if err != nil {
			t.Fatal(err)
		}
		resetPlayback(bob.ID)
		auth := authenticateEmbyAs(t, server, bob.Username, "bob-password", "bob-playback-queue")
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete,
			"/emby/Users/"+strconv.FormatInt(bob.ID, 10)+"/PlayingItems/"+strconv.FormatInt(episodes.first.ID, 10)+"?PositionTicks=9000000&api_key="+auth.AccessToken, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("Bob completion status = %d body = %s", recorder.Code, recorder.Body.String())
		}
		assertNotQueued(1, episodes.second)
		assertNotQueued(bob.ID, episodes.second)
		if got := nextUp(medialibrary.UserAccess{UserID: bob.ID}, episodes.seriesID); got.ID != episodes.second.ID {
			t.Fatalf("Bob next up episode = %#v", got)
		}
	})

	t.Run("adds a newly indexed episode to the Emby continue watching response", func(t *testing.T) {
		resetPlayback(1)
		complete(alice, episodes.last)
		seasonNumber, episodeNumber := 2, 3
		newEpisode, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
			LibraryID: library.ID, ParentID: episodes.last.ParentID, Kind: medialibrary.KindEpisode,
			Path: "/TV/Show/S2E3.mkv", MediaPath: "/TV/Show/S2E3.mkv",
			Name: "Episode 2.3", SortName: "Episode", Container: "mkv", RuntimeTicks: 10_000_000,
			SeasonNumber: &seasonNumber, EpisodeNumber: &episodeNumber,
			Genres: []string{}, Artists: []string{}, ScanGeneration: 1,
		})
		if err != nil {
			t.Fatal(err)
		}

		auth := authenticateEmby(t, server)
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet,
			"/emby/Users/1/Items/Resume?IncludeItemTypes=Episode&api_key="+auth.AccessToken, nil))
		var response struct {
			Items []struct {
				ID       string `json:"Id"`
				UserData struct {
					PlaybackPositionTicks int64 `json:"PlaybackPositionTicks"`
				} `json:"UserData"`
			} `json:"Items"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if recorder.Code != http.StatusOK || len(response.Items) != 1 || response.Items[0].ID != strconv.FormatInt(newEpisode.ID, 10) || response.Items[0].UserData.PlaybackPositionTicks != 0 {
			t.Fatalf("new episode resume status = %d items = %#v body = %s", recorder.Code, response.Items, recorder.Body.String())
		}
		assertNotQueued(1, newEpisode)
	})
}
