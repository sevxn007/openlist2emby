package sqlite

import (
	"fmt"
	"testing"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
)

func TestNextUpIsDerivedFromViewingHistory(t *testing.T) {
	store := newMediaLibraryStore(t)
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "TV", ContentType: medialibrary.ContentTVShows, RootPaths: []string{"/TV"},
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
	seasons := make(map[int]medialibrary.Item)
	for _, number := range []int{0, 1, 2} {
		season, _, createErr := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
			LibraryID: library.ID, ParentID: &series.ID, Kind: medialibrary.KindSeason,
			Path: fmt.Sprintf("/TV/Show/S%d", number), Name: fmt.Sprintf("Season %d", number), SortName: fmt.Sprintf("%04d", number),
			SeasonNumber: &number, ScanGeneration: 1,
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		seasons[number] = season
	}
	createEpisode := func(seasonNumber, episodeNumber int) medialibrary.Item {
		t.Helper()
		season := seasons[seasonNumber]
		item, _, createErr := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
			LibraryID: library.ID, ParentID: &season.ID, Kind: medialibrary.KindEpisode,
			Path:      fmt.Sprintf("/TV/Show/S%dE%d.mkv", seasonNumber, episodeNumber),
			MediaPath: fmt.Sprintf("/TV/Show/S%dE%d.mkv", seasonNumber, episodeNumber),
			Name:      fmt.Sprintf("S%dE%d", seasonNumber, episodeNumber), SortName: fmt.Sprintf("%04d", episodeNumber),
			SeasonNumber: &seasonNumber, EpisodeNumber: &episodeNumber, RuntimeTicks: 10_000_000, ScanGeneration: 1,
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		return item
	}
	special := createEpisode(0, 1)
	episode11 := createEpisode(1, 1)
	episode12 := createEpisode(1, 2)
	episode21 := createEpisode(2, 1)
	query := medialibrary.NextUpQuery{SeriesID: &series.ID, EnableResumable: true, Limit: 20}

	page, err := store.ListMediaNextUpForUser(t.Context(), 7, query)
	if err != nil || page.Total != 0 {
		t.Fatalf("unstarted series next up = %#v, %v", page, err)
	}
	playedAt := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	for index, item := range []medialibrary.Item{episode11, episode12} {
		value := playedAt.Add(time.Duration(index) * time.Hour)
		if _, err := store.UpdateMediaUserDataForUser(t.Context(), 7, item.ID, medialibrary.UserData{
			Played: true, PlayCount: 1, LastPlayedAt: &value,
		}); err != nil {
			t.Fatal(err)
		}
	}
	page, err = store.ListMediaNextUpForUser(t.Context(), 7, query)
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != episode21.ID {
		t.Fatalf("cross-season next up = %#v, %v", page, err)
	}
	if page.Items[0].ID == special.ID {
		t.Fatal("season zero special appeared in next up")
	}
	otherLibrary, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Other TV", ContentType: medialibrary.ContentTVShows, RootPaths: []string{"/OtherTV"},
	})
	if err != nil {
		t.Fatal(err)
	}
	wrongLibraryQuery := query
	wrongLibraryQuery.LibraryID = &otherLibrary.ID
	page, err = store.ListMediaNextUpForUser(t.Context(), 7, wrongLibraryQuery)
	if err != nil || page.Total != 0 || len(page.Items) != 0 {
		t.Fatalf("next up outside library scope = %#v, %v", page, err)
	}

	lastPlayedAt := playedAt.Add(2 * time.Hour)
	if _, err := store.UpdateMediaUserDataForUser(t.Context(), 7, episode21.ID, medialibrary.UserData{
		Played: true, PlayCount: 1, LastPlayedAt: &lastPlayedAt,
	}); err != nil {
		t.Fatal(err)
	}
	page, _ = store.ListMediaNextUpForUser(t.Context(), 7, query)
	if page.Total != 0 {
		t.Fatalf("completed series next up = %#v", page)
	}

	// A later scan can add an episode without writing any per-user state.
	episode22 := createEpisode(2, 2)
	page, err = store.ListMediaNextUpForUser(t.Context(), 7, query)
	if err != nil || page.Total != 1 || page.Items[0].ID != episode22.ID || page.Items[0].UserData.PlaybackPositionTicks != 0 {
		t.Fatalf("newly scanned episode next up = %#v, %v", page, err)
	}
	resumeAt := lastPlayedAt.Add(time.Hour)
	if _, err := store.UpdateMediaUserDataForUser(t.Context(), 7, episode22.ID, medialibrary.UserData{
		PlaybackPositionTicks: 2_000_000, LastPlayedAt: &resumeAt,
	}); err != nil {
		t.Fatal(err)
	}
	query.EnableResumable = false
	page, _ = store.ListMediaNextUpForUser(t.Context(), 7, query)
	if page.Total != 0 {
		t.Fatalf("resumable episode included while disabled = %#v", page)
	}
	query.EnableResumable = true
	cutoff := resumeAt.Add(time.Hour)
	query.DateCutoff = &cutoff
	page, _ = store.ListMediaNextUpForUser(t.Context(), 7, query)
	if page.Total != 0 {
		t.Fatalf("series before date cutoff = %#v", page)
	}

	// Rewatching follows the most recently played-by-date episode, even when all
	// episodes are already marked played.
	query.DateCutoff = nil
	if _, err := store.UpdateMediaUserDataForUser(t.Context(), 7, episode22.ID, medialibrary.UserData{
		Played: true, PlayCount: 1, LastPlayedAt: &resumeAt,
	}); err != nil {
		t.Fatal(err)
	}
	rewatchAt := resumeAt.Add(time.Hour)
	if _, err := store.UpdateMediaUserDataForUser(t.Context(), 7, episode11.ID, medialibrary.UserData{
		Played: true, PlayCount: 2, LastPlayedAt: &rewatchAt,
	}); err != nil {
		t.Fatal(err)
	}
	query.EnableRewatching = true
	page, err = store.ListMediaNextUpForUser(t.Context(), 7, query)
	if err != nil || page.Total != 1 || page.Items[0].ID != episode12.ID {
		t.Fatalf("rewatching next up = %#v, %v", page, err)
	}
}

func TestContinueWatchingMergesAndDeduplicatesNextUp(t *testing.T) {
	store := newMediaLibraryStore(t)
	tv, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{Name: "TV", ContentType: medialibrary.ContentTVShows, RootPaths: []string{"/TV"}})
	if err != nil {
		t.Fatal(err)
	}
	movies, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{Name: "Movies", ContentType: medialibrary.ContentMovies, RootPaths: []string{"/Movies"}})
	if err != nil {
		t.Fatal(err)
	}
	series, _, _ := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{LibraryID: tv.ID, Kind: medialibrary.KindSeries, Path: "/TV/Show", Name: "Show", SortName: "Show", ScanGeneration: 1})
	seasonNumber := 1
	season, _, _ := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{LibraryID: tv.ID, ParentID: &series.ID, Kind: medialibrary.KindSeason, Path: "/TV/Show/S1", Name: "Season 1", SortName: "1", SeasonNumber: &seasonNumber, ScanGeneration: 1})
	createEpisode := func(number int) medialibrary.Item {
		item, _, createErr := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{LibraryID: tv.ID, ParentID: &season.ID, Kind: medialibrary.KindEpisode, Path: fmt.Sprintf("/TV/Show/E%d", number), MediaPath: fmt.Sprintf("/TV/Show/E%d", number), Name: fmt.Sprintf("E%d", number), SortName: fmt.Sprint(number), SeasonNumber: &seasonNumber, EpisodeNumber: &number, RuntimeTicks: 10_000_000, ScanGeneration: 1})
		if createErr != nil {
			t.Fatal(createErr)
		}
		return item
	}
	first, second := createEpisode(1), createEpisode(2)
	movie, _, _ := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{LibraryID: movies.ID, Kind: medialibrary.KindMovie, Path: "/Movies/Movie.mp4", MediaPath: "/Movies/Movie.mp4", Name: "Movie", SortName: "Movie", RuntimeTicks: 10_000_000, ScanGeneration: 1})
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	_, _ = store.UpdateMediaUserDataForUser(t.Context(), 9, first.ID, medialibrary.UserData{Played: true, PlayCount: 1, LastPlayedAt: &now})
	later := now.Add(time.Hour)
	_, _ = store.UpdateMediaUserDataForUser(t.Context(), 9, second.ID, medialibrary.UserData{PlaybackPositionTicks: 2_000_000, LastPlayedAt: &later})
	_, _ = store.UpdateMediaUserDataForUser(t.Context(), 9, movie.ID, medialibrary.UserData{PlaybackPositionTicks: 3_000_000, LastPlayedAt: &now})

	page, err := store.ListMediaContinueWatchingForUser(t.Context(), 9, medialibrary.NextUpQuery{EnableResumable: true, Limit: 20})
	if err != nil || page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("merged continue watching = %#v, %v", page, err)
	}
	if page.Items[0].Item.ID != second.ID || page.Items[0].Source != medialibrary.ContinueWatchingResume {
		t.Fatalf("episode resume entry = %#v", page.Items[0])
	}
	for _, entry := range page.Items {
		if entry.Item.ID == second.ID && entry.Source == medialibrary.ContinueWatchingNextUp {
			t.Fatal("series next up was not suppressed by a real episode resume")
		}
	}

	_, _ = store.UpdateMediaUserDataForUser(t.Context(), 9, second.ID, medialibrary.UserData{LastPlayedAt: &later})
	page, err = store.ListMediaContinueWatchingForUser(t.Context(), 9, medialibrary.NextUpQuery{EnableResumable: true, Limit: 1})
	if err != nil || page.Total != 2 || len(page.Items) != 1 || page.Items[0].Item.ID != second.ID || page.Items[0].Source != medialibrary.ContinueWatchingNextUp {
		t.Fatalf("resume converted to paged next up = %#v, %v", page, err)
	}
	denied, err := store.ListMediaContinueWatchingForUser(t.Context(), 9, medialibrary.NextUpQuery{EnableResumable: true, Limit: 20, AllowedLibraryIDs: []int64{}})
	if err != nil || denied.Total != 0 || len(denied.Items) != 0 {
		t.Fatalf("empty library access = %#v, %v", denied, err)
	}
	movieOnly, err := store.ListMediaContinueWatchingForUser(t.Context(), 9, medialibrary.NextUpQuery{
		EnableResumable: true, Limit: 20, Kinds: []medialibrary.ItemKind{medialibrary.KindMovie},
	})
	if err != nil || movieOnly.Total != 1 || len(movieOnly.Items) != 1 || movieOnly.Items[0].Item.ID != movie.ID {
		t.Fatalf("movie-only continue watching = %#v, %v", movieOnly, err)
	}
	episodeOnly, err := store.ListMediaContinueWatchingForUser(t.Context(), 9, medialibrary.NextUpQuery{
		EnableResumable: true, Limit: 20, Kinds: []medialibrary.ItemKind{medialibrary.KindEpisode},
	})
	if err != nil || episodeOnly.Total != 1 || len(episodeOnly.Items) != 1 || episodeOnly.Items[0].Item.ID != second.ID || episodeOnly.Items[0].Source != medialibrary.ContinueWatchingNextUp {
		t.Fatalf("episode-only continue watching = %#v, %v", episodeOnly, err)
	}
}

func TestHiddenResumeEpisodeDoesNotReappearAsNextUp(t *testing.T) {
	store := newMediaLibraryStore(t)
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{Name: "TV", ContentType: medialibrary.ContentTVShows, RootPaths: []string{"/TV"}})
	if err != nil {
		t.Fatal(err)
	}
	create := func(input medialibrary.ItemInput) medialibrary.Item {
		t.Helper()
		input.LibraryID = library.ID
		input.ScanGeneration = 1
		item, _, err := store.UpsertMediaItem(t.Context(), input)
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	series := create(medialibrary.ItemInput{Kind: medialibrary.KindSeries, Path: "/TV/Show", Name: "Show"})
	n := 1
	season := create(medialibrary.ItemInput{Kind: medialibrary.KindSeason, ParentID: &series.ID, SeasonNumber: &n, Path: "/TV/Show/S1", Name: "Season 1"})
	episode := create(medialibrary.ItemInput{Kind: medialibrary.KindEpisode, ParentID: &season.ID, SeasonNumber: &n, EpisodeNumber: &n, Path: "/TV/Show/S1/E1.mkv", Name: "Episode 1"})
	n2 := 2
	create(medialibrary.ItemInput{Kind: medialibrary.KindEpisode, ParentID: &season.ID, SeasonNumber: &n, EpisodeNumber: &n2, Path: "/TV/Show/S1/E2.mkv", Name: "Episode 2"})
	now := time.Now().UTC()
	for _, position := range []int64{0, 4200000} {
		for _, hide := range []bool{false, true, false} {
			_, err := store.UpdateMediaUserDataForUser(t.Context(), 1, episode.ID, medialibrary.UserData{HideFromResume: hide, PlaybackPositionTicks: position, LastPlayedAt: &now})
			if err != nil {
				t.Fatal(err)
			}
			query := medialibrary.NextUpQuery{EnableResumable: true, Limit: 20}
			continued, err := store.ListMediaContinueWatchingForUser(t.Context(), 1, query)
			if err != nil {
				t.Fatal(err)
			}
			next, err := store.ListMediaNextUpForUser(t.Context(), 1, query)
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if hide {
				want = 0
			}
			if len(continued.Items) != want || continued.Total != want || len(next.Items) != want || next.Total != want {
				t.Fatalf("hide=%v position=%d: continue=%#v next=%#v", hide, position, continued, next)
			}
		}
	}
}
