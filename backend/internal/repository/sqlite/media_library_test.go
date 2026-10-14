package sqlite

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	glebarezsqlite "github.com/glebarez/sqlite"
	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newMediaLibraryStore(t *testing.T) *Store {
	t.Helper()
	db, err := gorm.Open(glebarezsqlite.Open(filepath.Join(t.TempDir(), "media.db")), &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Silent),
		TranslateError: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	store := NewStoreWithClock(db, func() time.Time {
		return time.Date(2026, 7, 27, 8, 0, 0, 0, time.UTC)
	})
	if err := store.InitializeSchema(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestLocalPeopleFilterReturnsGuestEpisode(t *testing.T) {
	store := newMediaLibraryStore(t)
	if err := store.SaveInitialSettings(t.Context(), "http://openlist.local", "admin-key", "tmdb-key"); err != nil {
		t.Fatal(err)
	}
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{Name: "TV", ContentType: medialibrary.ContentTVShows, RootPaths: []string{"/TV"}})
	if err != nil {
		t.Fatal(err)
	}
	series, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{LibraryID: library.ID, Kind: medialibrary.KindSeries, Path: "/TV/Show", Name: "Show", SortName: "Show", ScanGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	seasonNumber, episodeNumber := 1, 2
	season, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{LibraryID: library.ID, ParentID: &series.ID, Kind: medialibrary.KindSeason, Path: "/TV/Show/S1", Name: "Season 1", SortName: "0001", SeasonNumber: &seasonNumber, ScanGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	episode, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{LibraryID: library.ID, ParentID: &season.ID, Kind: medialibrary.KindEpisode, Path: "/TV/Show/S1/E2.mkv", Name: "Episode 2", SortName: "0002", SeasonNumber: &seasonNumber, EpisodeNumber: &episodeNumber, ScanGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceMediaItemPeople(t.Context(), episode.ID, []medialibrary.LocalPerson{{Person: domain.MediaPerson{ID: 99, Name: "演员", Type: "Actor", Role: "客串", SortOrder: 1}}}); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListMediaItemsForUser(t.Context(), 1, medialibrary.ItemQuery{Kinds: []medialibrary.ItemKind{medialibrary.KindEpisode}, PersonIDs: []int64{99}, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != episode.ID {
		t.Fatalf("person filter page = %#v", page)
	}
	if page.Items[0].SeriesID == nil || *page.Items[0].SeriesID != series.ID || page.Items[0].SeriesName != "Show" {
		t.Fatalf("episode series = %#v", page.Items[0])
	}
	people, err := store.ListMediaItemPeople(t.Context(), episode.ID)
	if err != nil || len(people) != 1 || people[0].Name != "演员" {
		t.Fatalf("local people = %#v, %v", people, err)
	}
}

func TestPlayedStateCascadesSeriesSeasonAndEpisode(t *testing.T) {
	store := newMediaLibraryStore(t)
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{Name: "TV", ContentType: medialibrary.ContentTVShows, RootPaths: []string{"/TV"}})
	if err != nil {
		t.Fatal(err)
	}
	series, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{LibraryID: library.ID, Kind: medialibrary.KindSeries, Path: "/TV/Show", Name: "Show", SortName: "Show", ScanGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	items := []medialibrary.Item{series}
	for seasonNumber := 1; seasonNumber <= 2; seasonNumber++ {
		season, _, createErr := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{LibraryID: library.ID, ParentID: &series.ID, Kind: medialibrary.KindSeason, Path: fmt.Sprintf("/TV/Show/S%d", seasonNumber), Name: fmt.Sprintf("Season %d", seasonNumber), SortName: fmt.Sprintf("%04d", seasonNumber), SeasonNumber: &seasonNumber, ScanGeneration: 1})
		if createErr != nil {
			t.Fatal(createErr)
		}
		items = append(items, season)
		for episodeNumber := 1; episodeNumber <= 2; episodeNumber++ {
			episode, _, episodeErr := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{LibraryID: library.ID, ParentID: &season.ID, Kind: medialibrary.KindEpisode, Path: fmt.Sprintf("/TV/Show/S%d/E%d.mkv", seasonNumber, episodeNumber), Name: fmt.Sprintf("Episode %d", episodeNumber), SortName: fmt.Sprintf("%04d", episodeNumber), SeasonNumber: &seasonNumber, EpisodeNumber: &episodeNumber, ScanGeneration: 1})
			if episodeErr != nil {
				t.Fatal(episodeErr)
			}
			items = append(items, episode)
		}
	}
	favoriteEpisode := items[2]
	if _, err := store.UpdateMediaUserDataForUser(t.Context(), 5, favoriteEpisode.ID, medialibrary.UserData{Favorite: true, PlayCount: 2}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 7, 15, 0, 0, 0, time.UTC)
	seriesData, err := store.SetMediaPlayedStateForUser(t.Context(), 5, series.ID, true, now)
	if err != nil {
		t.Fatal(err)
	}
	if !seriesData.Played || seriesData.UnplayedItemCount == nil || *seriesData.UnplayedItemCount != 0 {
		t.Fatalf("series data = %#v", seriesData)
	}
	for _, item := range items {
		stored, getErr := store.GetMediaItemForUser(t.Context(), 5, item.ID)
		if getErr != nil || !stored.UserData.Played {
			t.Fatalf("item %d played = %v, error = %v", item.ID, stored.UserData.Played, getErr)
		}
	}
	storedFavorite, _ := store.GetMediaItemForUser(t.Context(), 5, favoriteEpisode.ID)
	if !storedFavorite.UserData.Favorite || storedFavorite.UserData.PlayCount != 3 {
		t.Fatalf("favorite episode state = %#v", storedFavorite.UserData)
	}
	if _, err := store.SetMediaPlayedStateForUser(t.Context(), 5, series.ID, true, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	repeated, _ := store.GetMediaItemForUser(t.Context(), 5, favoriteEpisode.ID)
	if repeated.UserData.PlayCount != 3 {
		t.Fatalf("repeat play count = %d", repeated.UserData.PlayCount)
	}

	seasonOne := items[1]
	seasonData, err := store.SetMediaPlayedStateForUser(t.Context(), 5, seasonOne.ID, false, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if seasonData.Played || seasonData.UnplayedItemCount == nil || *seasonData.UnplayedItemCount != 2 {
		t.Fatalf("season data = %#v", seasonData)
	}
	seriesAfter, _ := store.GetMediaItemForUser(t.Context(), 5, series.ID)
	if seriesAfter.UserData.Played || seriesAfter.UserData.UnplayedItemCount == nil || *seriesAfter.UserData.UnplayedItemCount != 2 {
		t.Fatalf("series after season unplayed = %#v", seriesAfter.UserData)
	}
	seasonTwo, _ := store.GetMediaItemForUser(t.Context(), 5, items[4].ID)
	if !seasonTwo.UserData.Played {
		t.Fatalf("other season was changed: %#v", seasonTwo.UserData)
	}

	for _, episode := range items[2:4] {
		if _, err := store.SetMediaPlayedStateForUser(t.Context(), 5, episode.ID, true, now.Add(3*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	seasonOneAfter, _ := store.GetMediaItemForUser(t.Context(), 5, seasonOne.ID)
	seriesFinal, _ := store.GetMediaItemForUser(t.Context(), 5, series.ID)
	if !seasonOneAfter.UserData.Played || !seriesFinal.UserData.Played {
		t.Fatalf("parent states were not reconciled: season=%#v series=%#v", seasonOneAfter.UserData, seriesFinal.UserData)
	}
}

func TestMediaLibraryPersistenceAndGenerationCleanup(t *testing.T) {
	store := newMediaLibraryStore(t)
	if err := store.SaveInitialSettings(t.Context(), "http://openlist.local", "admin-key", "tmdb-key"); err != nil {
		t.Fatal(err)
	}
	settings, err := store.GetSettings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if settings.MediaLibraryEnabled {
		t.Fatal("media library should be disabled by default")
	}
	if err := store.SetMediaLibraryConfiguration(t.Context(), true, domain.DefaultEmbyPort); err != nil {
		t.Fatal(err)
	}

	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Movies", ContentType: medialibrary.ContentMovies, RootPaths: []string{"/Movies", "/Archive"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if library.RootPath != "/Movies" || len(library.RootPaths) != 2 || library.RootPaths[1] != "/Archive" {
		t.Fatalf("library roots = %#v", library)
	}
	generation, err := store.BeginLibraryScan(t.Context(), library.ID)
	if err != nil {
		t.Fatal(err)
	}
	oldItem, added, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, Kind: medialibrary.KindMovie, Path: "/Movies/Old.mkv",
		MediaPath: "/Movies/Old.mkv", Name: "Old", SortName: "Old", Container: "mkv",
		Genres: []string{"Drama"}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil || !added {
		t.Fatalf("UpsertMediaItem() added = %v error = %v", added, err)
	}
	if removed, err := store.FinishLibraryScan(t.Context(), library.ID, generation); err != nil || removed != 0 {
		t.Fatalf("FinishLibraryScan() removed = %d error = %v", removed, err)
	}

	generation, err = store.BeginLibraryScan(t.Context(), library.ID)
	if err != nil {
		t.Fatal(err)
	}
	newItem, added, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, Kind: medialibrary.KindMovie, Path: "/Movies/New.mkv",
		MediaPath: "/Movies/New.mkv", Name: "New", SortName: "New", Container: "mkv",
		IMDbID: "tt0000001", Genres: []string{"Action"}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil || !added {
		t.Fatalf("second UpsertMediaItem() added = %v error = %v", added, err)
	}
	updatedItem, added, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, Kind: medialibrary.KindMovie, Path: "/Movies/New.mkv",
		MediaPath: "/Movies/New.mkv", Name: "New title", SortName: "New title", Container: "mkv",
		IMDbID: "tt0000001", Genres: []string{"Action", "Drama"}, Artists: []string{},
		Overview: "Updated metadata", ScanGeneration: generation,
	})
	if err != nil || added {
		t.Fatalf("updated UpsertMediaItem() added = %v error = %v", added, err)
	}
	if updatedItem.ID != newItem.ID || updatedItem.Name != "New title" || updatedItem.Overview != "Updated metadata" {
		t.Fatalf("updated media item = %#v", updatedItem)
	}
	newItem = updatedItem
	removed, err := store.FinishLibraryScan(t.Context(), library.ID, generation)
	if err != nil || removed != 1 {
		t.Fatalf("FinishLibraryScan() removed = %d error = %v", removed, err)
	}
	if _, err := store.GetMediaItem(t.Context(), oldItem.ID); err == nil {
		t.Fatal("stale media item was not removed")
	}
	page, err := store.ListMediaItems(t.Context(), medialibrary.ItemQuery{
		LibraryID: &library.ID, ParentSet: true, Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != newItem.ID || page.Items[0].IMDbID != "tt0000001" {
		t.Fatalf("media page = %#v", page)
	}

	now := time.Date(2026, 7, 27, 9, 0, 0, 0, time.UTC)
	data, err := store.UpdateMediaUserData(t.Context(), newItem.ID, medialibrary.UserData{
		PlaybackPositionTicks: 123, PlayCount: 2, Favorite: true, LastPlayedAt: &now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if data.PlaybackPositionTicks != 123 || data.PlayCount != 2 || !data.Favorite {
		t.Fatalf("user data = %#v", data)
	}
}

func TestReassociateMediaItemPreservesIdentityAndUserState(t *testing.T) {
	store := newMediaLibraryStore(t)
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{Name: "Movies", ContentType: medialibrary.ContentMovies, RootPaths: []string{"/Movies"}})
	if err != nil {
		t.Fatal(err)
	}
	tmdbID, year := int64(12345), 2020
	firstGeneration, err := store.BeginLibraryScan(t.Context(), library.ID)
	if err != nil {
		t.Fatal(err)
	}
	old, added, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, Kind: medialibrary.KindMovie, Path: "/Movies/Old.mkv", MediaPath: "/Movies/Old.mkv",
		Name: "Example Movie", SortName: "Example Movie", ProductionYear: &year, TMDBID: &tmdbID, ScanGeneration: firstGeneration,
	})
	if err != nil || !added {
		t.Fatalf("create old item: added=%v err=%v", added, err)
	}
	if err := store.UpdateMediaItemMetadata(t.Context(), old.ID, medialibrary.MetadataInput{Name: "Pinned Custom Title", SortName: "Pinned Custom Title", TMDBID: &tmdbID, ProductionYear: &year}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateMediaUserDataForUser(t.Context(), 9, old.ID, medialibrary.UserData{PlaybackPositionTicks: 42, Played: true, PlayCount: 3, Favorite: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertMediaItemImage(t.Context(), medialibrary.LocalImage{ItemID: old.ID, ImageType: medialibrary.ImagePrimary, SourceType: medialibrary.ImageSourceRemote, RelativePath: "movie.jpg", MIMEType: "image/jpeg"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishLibraryScan(t.Context(), library.ID, firstGeneration); err != nil {
		t.Fatal(err)
	}

	secondGeneration, err := store.BeginLibraryScan(t.Context(), library.ID)
	if err != nil {
		t.Fatal(err)
	}
	moved, added, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, Kind: medialibrary.KindMovie, Path: "/Movies/New.mkv", MediaPath: "/Movies/New.mkv",
		Name: "Example Movie", SortName: "Example Movie", ProductionYear: &year, TMDBID: &tmdbID, ScanGeneration: secondGeneration,
	})
	if err != nil || !added {
		t.Fatalf("create temporary item: added=%v err=%v", added, err)
	}
	reassociated, didReassociate, err := store.ReassociateMediaItem(t.Context(), moved.ID, medialibrary.ItemInput{
		LibraryID: library.ID, Kind: medialibrary.KindMovie, Path: "/Movies/New.mkv", MediaPath: "/Movies/New.mkv",
		Name: "Example Movie", SortName: "Example Movie", ProductionYear: &year, TMDBID: &tmdbID, ScanGeneration: secondGeneration,
	})
	if err != nil || !didReassociate {
		t.Fatalf("reassociate: did=%v err=%v", didReassociate, err)
	}
	if reassociated.ID != old.ID || reassociated.Path != "/Movies/New.mkv" || reassociated.Name != "Pinned Custom Title" {
		t.Fatalf("reassociated item = %#v, old=%#v", reassociated, old)
	}
	stored, err := store.GetMediaItemForUser(t.Context(), 9, old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.UserData.PlaybackPositionTicks != 42 || !stored.UserData.Played || stored.UserData.PlayCount != 3 || !stored.UserData.Favorite {
		t.Fatalf("user state was not preserved: %#v", stored.UserData)
	}
	image, err := store.GetMediaItemImage(t.Context(), old.ID, medialibrary.ImagePrimary)
	if err != nil || image.RelativePath != "movie.jpg" {
		t.Fatalf("local image was not preserved: %#v, %v", image, err)
	}
	if _, err := store.GetMediaItem(t.Context(), moved.ID); err == nil {
		t.Fatal("temporary item still exists")
	}
	if removed, err := store.FinishLibraryScan(t.Context(), library.ID, secondGeneration); err != nil || removed != 0 {
		t.Fatalf("finish scan removed=%d err=%v", removed, err)
	}
}

func TestReassociateMediaItemRejectsAmbiguousTMDBCandidates(t *testing.T) {
	store := newMediaLibraryStore(t)
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{Name: "Movies", ContentType: medialibrary.ContentMovies, RootPaths: []string{"/Movies"}})
	if err != nil {
		t.Fatal(err)
	}
	tmdbID, year := int64(9876), 2022
	firstGeneration, _ := store.BeginLibraryScan(t.Context(), library.ID)
	for _, objectPath := range []string{"/Movies/one.mkv", "/Movies/two.mkv"} {
		if _, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{LibraryID: library.ID, Kind: medialibrary.KindMovie, Path: objectPath, MediaPath: objectPath, Name: "Duplicate", SortName: "Duplicate", ProductionYear: &year, TMDBID: &tmdbID, ScanGeneration: firstGeneration}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.FinishLibraryScan(t.Context(), library.ID, firstGeneration); err != nil {
		t.Fatal(err)
	}
	secondGeneration, _ := store.BeginLibraryScan(t.Context(), library.ID)
	temporary, added, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{LibraryID: library.ID, Kind: medialibrary.KindMovie, Path: "/Movies/moved.mkv", MediaPath: "/Movies/moved.mkv", Name: "Duplicate", SortName: "Duplicate", ProductionYear: &year, TMDBID: &tmdbID, ScanGeneration: secondGeneration})
	if err != nil || !added {
		t.Fatalf("create temporary item: added=%v err=%v", added, err)
	}
	_, didReassociate, err := store.ReassociateMediaItem(t.Context(), temporary.ID, medialibrary.ItemInput{LibraryID: library.ID, Kind: medialibrary.KindMovie, Path: "/Movies/moved.mkv", MediaPath: "/Movies/moved.mkv", Name: "Duplicate", SortName: "Duplicate", ProductionYear: &year, TMDBID: &tmdbID, ScanGeneration: secondGeneration})
	if err != nil {
		t.Fatal(err)
	}
	if didReassociate {
		t.Fatal("ambiguous TMDB candidates were reassociated")
	}
}

func TestReassociateTVHierarchyUsesStableParentAndEpisodeNumbers(t *testing.T) {
	store := newMediaLibraryStore(t)
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{Name: "TV", ContentType: medialibrary.ContentTVShows, RootPaths: []string{"/TV"}})
	if err != nil {
		t.Fatal(err)
	}
	seriesTMDB, seasonTMDB, episodeTMDB := int64(11), int64(12), int64(13)
	seasonNumber, episodeNumber := 1, 2
	firstGeneration, _ := store.BeginLibraryScan(t.Context(), library.ID)
	series, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{LibraryID: library.ID, Kind: medialibrary.KindSeries, Path: "/TV/Old", MediaPath: "/TV/Old", Name: "Example Show", SortName: "Example Show", TMDBID: &seriesTMDB, ScanGeneration: firstGeneration})
	if err != nil {
		t.Fatal(err)
	}
	season, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{LibraryID: library.ID, ParentID: &series.ID, Kind: medialibrary.KindSeason, Path: "/TV/Old/S1", MediaPath: "/TV/Old/S1", Name: "Season 1", SortName: "0001", SeasonNumber: &seasonNumber, TMDBID: &seasonTMDB, ScanGeneration: firstGeneration})
	if err != nil {
		t.Fatal(err)
	}
	episode, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{LibraryID: library.ID, ParentID: &season.ID, Kind: medialibrary.KindEpisode, Path: "/TV/Old/S1/E2.mkv", MediaPath: "/TV/Old/S1/E2.mkv", Name: "Episode 2", SortName: "0002", SeasonNumber: &seasonNumber, EpisodeNumber: &episodeNumber, TMDBID: &episodeTMDB, ScanGeneration: firstGeneration})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateMediaUserDataForUser(t.Context(), 3, episode.ID, medialibrary.UserData{Played: true, PlaybackPositionTicks: 99}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishLibraryScan(t.Context(), library.ID, firstGeneration); err != nil {
		t.Fatal(err)
	}

	secondGeneration, _ := store.BeginLibraryScan(t.Context(), library.ID)
	newSeries, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{LibraryID: library.ID, Kind: medialibrary.KindSeries, Path: "/TV/New", MediaPath: "/TV/New", Name: "Example Show", SortName: "Example Show", TMDBID: &seriesTMDB, ScanGeneration: secondGeneration})
	if err != nil {
		t.Fatal(err)
	}
	newSeries, did, err := store.ReassociateMediaItem(t.Context(), newSeries.ID, medialibrary.ItemInput{LibraryID: library.ID, Kind: medialibrary.KindSeries, Path: "/TV/New", MediaPath: "/TV/New", Name: "Example Show", SortName: "Example Show", TMDBID: &seriesTMDB, ScanGeneration: secondGeneration})
	if err != nil || !did || newSeries.ID != series.ID {
		t.Fatalf("series reassociation: item=%#v did=%v err=%v", newSeries, did, err)
	}
	newSeason, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{LibraryID: library.ID, ParentID: &newSeries.ID, Kind: medialibrary.KindSeason, Path: "/TV/New/S1", MediaPath: "/TV/New/S1", Name: "Season 1", SortName: "0001", SeasonNumber: &seasonNumber, TMDBID: &seasonTMDB, ScanGeneration: secondGeneration})
	if err != nil {
		t.Fatal(err)
	}
	newSeason, did, err = store.ReassociateMediaItem(t.Context(), newSeason.ID, medialibrary.ItemInput{LibraryID: library.ID, ParentID: &newSeries.ID, Kind: medialibrary.KindSeason, Path: "/TV/New/S1", MediaPath: "/TV/New/S1", Name: "Season 1", SortName: "0001", SeasonNumber: &seasonNumber, TMDBID: &seasonTMDB, ScanGeneration: secondGeneration})
	if err != nil || !did || newSeason.ID != season.ID {
		t.Fatalf("season reassociation: item=%#v did=%v err=%v", newSeason, did, err)
	}
	newEpisode, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{LibraryID: library.ID, ParentID: &newSeason.ID, Kind: medialibrary.KindEpisode, Path: "/TV/New/S1/E2.mkv", MediaPath: "/TV/New/S1/E2.mkv", Name: "Episode 2", SortName: "0002", SeasonNumber: &seasonNumber, EpisodeNumber: &episodeNumber, TMDBID: &episodeTMDB, ScanGeneration: secondGeneration})
	if err != nil {
		t.Fatal(err)
	}
	reassociatedEpisode, did, err := store.ReassociateMediaItem(t.Context(), newEpisode.ID, medialibrary.ItemInput{LibraryID: library.ID, ParentID: &newSeason.ID, Kind: medialibrary.KindEpisode, Path: "/TV/New/S1/E2.mkv", MediaPath: "/TV/New/S1/E2.mkv", Name: "Episode 2", SortName: "0002", SeasonNumber: &seasonNumber, EpisodeNumber: &episodeNumber, TMDBID: &episodeTMDB, ScanGeneration: secondGeneration})
	if err != nil || !did || reassociatedEpisode.ID != episode.ID {
		t.Fatalf("episode reassociation: item=%#v did=%v err=%v", reassociatedEpisode, did, err)
	}
	stored, err := store.GetMediaItemForUser(t.Context(), 3, episode.ID)
	if err != nil || !stored.UserData.Played || stored.UserData.PlaybackPositionTicks != 99 {
		t.Fatalf("episode state = %#v err=%v", stored.UserData, err)
	}
}

func TestMediaLibraryPersistsPlaybackAndVideoScreenshotConfiguration(t *testing.T) {
	store := newMediaLibraryStore(t)
	enabled, percent, completionPercent := true, 35, 95
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Movies", ContentType: medialibrary.ContentMovies, RootPaths: []string{"/Movies"},
		VideoScreenshotEnabled: &enabled, VideoScreenshotPercent: &percent,
		PlaybackCompletionPercent: &completionPercent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !library.VideoScreenshotEnabled || library.VideoScreenshotPercent != 35 || library.PlaybackCompletionPercent != 95 {
		t.Fatalf("created library configuration = %#v", library)
	}
	loaded, err := store.GetLibrary(t.Context(), library.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.VideoScreenshotEnabled || loaded.VideoScreenshotPercent != 35 || loaded.PlaybackCompletionPercent != 95 {
		t.Fatalf("loaded library configuration = %#v", loaded)
	}
}

func TestMediaLibraryPersistsPeopleImageVisibilitySetting(t *testing.T) {
	store := newMediaLibraryStore(t)
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Movies", ContentType: medialibrary.ContentMovies, RootPaths: []string{"/Movies"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if library.HidePeopleWithoutImages {
		t.Fatal("new media library should show people without images by default")
	}
	hide := true
	updated, err := store.UpdateLibrary(t.Context(), library.ID, medialibrary.LibraryInput{
		Name: library.Name, ContentType: library.ContentType, RootPaths: library.RootPaths,
		HidePeopleWithoutImages: &hide,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.HidePeopleWithoutImages {
		t.Fatal("updated people image visibility setting was not returned")
	}
	withoutField, err := store.UpdateLibrary(t.Context(), library.ID, medialibrary.LibraryInput{
		Name: library.Name, ContentType: library.ContentType, RootPaths: library.RootPaths,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !withoutField.HidePeopleWithoutImages {
		t.Fatal("omitting people image visibility setting should preserve it")
	}
}

func TestMediaLibraryRootsCanBeReplacedAtomically(t *testing.T) {
	store := newMediaLibraryStore(t)
	first, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "First", ContentType: medialibrary.ContentMovies, RootPaths: []string{"/Movies", "/Archive"},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Second", ContentType: medialibrary.ContentMovies, RootPath: "/Other",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateLibrary(t.Context(), first.ID, medialibrary.LibraryInput{
		Name: "First", ContentType: medialibrary.ContentMovies, RootPaths: []string{"/Other", "/New"},
	}); err == nil {
		t.Fatal("updating a library with another library's root succeeded")
	}
	unchanged, err := store.GetLibrary(t.Context(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(unchanged.RootPaths) != 2 || unchanged.RootPaths[0] != "/Movies" || unchanged.RootPaths[1] != "/Archive" {
		t.Fatalf("failed update changed roots = %#v", unchanged.RootPaths)
	}
	if _, err := store.UpdateLibrary(t.Context(), first.ID, medialibrary.LibraryInput{
		Name: "First", ContentType: medialibrary.ContentMovies, RootPaths: []string{"/New", "/New-Archive"},
	}); err != nil {
		t.Fatal(err)
	}
	updated, err := store.GetLibrary(t.Context(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.RootPaths) != 2 || updated.RootPaths[0] != "/New" || updated.RootPaths[1] != "/New-Archive" {
		t.Fatalf("updated roots = %#v", updated.RootPaths)
	}
	if _, err := store.GetLibrary(t.Context(), second.ID); err != nil {
		t.Fatal(err)
	}
}

func TestMediaLibraryOrderCanBeReplacedAtomically(t *testing.T) {
	store := newMediaLibraryStore(t)
	first, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "First", ContentType: medialibrary.ContentMovies, RootPath: "/First",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Second", ContentType: medialibrary.ContentTVShows, RootPath: "/Second",
	})
	if err != nil {
		t.Fatal(err)
	}
	third, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Third", ContentType: medialibrary.ContentMusic, RootPath: "/Third",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReorderLibraries(t.Context(), []int64{third.ID, first.ID, second.ID}); err != nil {
		t.Fatal(err)
	}
	libraries, err := store.ListLibraries(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(libraries) != 3 || libraries[0].ID != third.ID || libraries[1].ID != first.ID || libraries[2].ID != second.ID {
		t.Fatalf("library order = %#v", libraries)
	}
	if libraries[0].SortOrder != 0 || libraries[1].SortOrder != 1 || libraries[2].SortOrder != 2 {
		t.Fatalf("library sort orders = %#v", libraries)
	}
	if err := store.ReorderLibraries(t.Context(), []int64{third.ID, third.ID, first.ID}); err == nil {
		t.Fatal("reordering with duplicate library IDs succeeded")
	}
	libraries, err = store.ListLibraries(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if libraries[0].ID != third.ID || libraries[1].ID != first.ID || libraries[2].ID != second.ID {
		t.Fatalf("invalid reorder changed libraries = %#v", libraries)
	}
}

func TestListMediaItemsSupportsHierarchyAndResumeFilters(t *testing.T) {
	store := newMediaLibraryStore(t)
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Shows", ContentType: medialibrary.ContentTVShows, RootPath: "/Shows",
	})
	if err != nil {
		t.Fatal(err)
	}
	generation, err := store.BeginLibraryScan(t.Context(), library.ID)
	if err != nil {
		t.Fatal(err)
	}
	series, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, Kind: medialibrary.KindSeries, Path: "/Shows/Series",
		Name: "Series", SortName: "Series", ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	season, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, ParentID: &series.ID, Kind: medialibrary.KindSeason,
		Path: "/Shows/Series/Season 01", Name: "Season 01", SortName: "Season 01",
		ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	episode, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, ParentID: &season.ID, Kind: medialibrary.KindEpisode,
		Path: "/Shows/Series/Season 01/Episode 01.mkv", MediaPath: "/Shows/Series/Season 01/Episode 01.mkv",
		Name: "Episode 01", SortName: "Episode 01", Container: "mkv", ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	audio, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, Kind: medialibrary.KindAudio,
		Path: "/Shows/Series/track.flac", MediaPath: "/Shows/Series/track.flac",
		Name: "Track", SortName: "Track", Container: "flac", ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}

	page, err := store.ListMediaItems(t.Context(), medialibrary.ItemQuery{
		LibraryID: &library.ID, AncestorID: &series.ID, Recursive: true,
		Kinds: []medialibrary.ItemKind{medialibrary.KindEpisode}, Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != episode.ID {
		t.Fatalf("recursive media page = %#v", page)
	}

	lastPlayed := time.Date(2026, 7, 27, 9, 30, 0, 0, time.UTC)
	if _, err := store.UpdateMediaUserData(t.Context(), episode.ID, medialibrary.UserData{
		PlaybackPositionTicks: 456, PlayCount: 1, LastPlayedAt: &lastPlayed,
	}); err != nil {
		t.Fatal(err)
	}
	page, err = store.ListMediaItems(t.Context(), medialibrary.ItemQuery{
		LibraryID: &library.ID, Resume: true, Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != episode.ID || page.Items[0].UserData.PlaybackPositionTicks != 456 {
		t.Fatalf("resume media page = %#v", page)
	}
	if _, err := store.UpdateMediaUserData(t.Context(), audio.ID, medialibrary.UserData{
		PlaybackPositionTicks: 789, LastPlayedAt: &lastPlayed,
	}); err != nil {
		t.Fatal(err)
	}
	page, err = store.ListMediaItems(t.Context(), medialibrary.ItemQuery{
		LibraryID: &library.ID, Resume: true, Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range page.Items {
		if item.ID == audio.ID {
			t.Fatalf("audio item appeared in resume page: %#v", item)
		}
	}
}

func TestManualMediaMetadataSurvivesLaterScans(t *testing.T) {
	store := newMediaLibraryStore(t)
	if err := store.SaveInitialSettings(t.Context(), "http://openlist.local", "admin-key"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetMediaLibraryConfiguration(t.Context(), true, domain.DefaultEmbyPort); err != nil {
		t.Fatal(err)
	}
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Movies", ContentType: medialibrary.ContentMovies, RootPath: "/Movies",
	})
	if err != nil {
		t.Fatal(err)
	}
	generation, err := store.BeginLibraryScan(t.Context(), library.ID)
	if err != nil {
		t.Fatal(err)
	}
	item, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, Kind: medialibrary.KindMovie, Path: "/Movies/Dune.mkv",
		MediaPath: "/Movies/Dune.mkv", Name: "Scanner title", SortName: "Scanner title",
		SizeBytes: 100, Genres: []string{"Science fiction"}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	year := 2021
	if err := store.UpdateMediaItemMetadata(t.Context(), item.ID, medialibrary.MetadataInput{
		Name: "Dune", SortName: "Dune", Genres: []string{"Sci-Fi"}, ProductionYear: &year,
		Overview: "A custom description.", Artists: []string{},
	}); err != nil {
		t.Fatal(err)
	}

	nextGeneration, err := store.BeginLibraryScan(t.Context(), library.ID)
	if err != nil {
		t.Fatal(err)
	}
	scannerYear := 2024
	if _, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, Kind: medialibrary.KindMovie, Path: "/Movies/Dune.mkv",
		MediaPath: "/Movies/Dune.remux.mkv", Name: "Changed scanner title", SortName: "Changed scanner title",
		SizeBytes: 200, Genres: []string{"Adventure"}, ProductionYear: &scannerYear,
		Overview: "Scanner description.", Artists: []string{}, ScanGeneration: nextGeneration,
	}); err != nil {
		t.Fatal(err)
	}

	updated, err := store.GetMediaItem(t.Context(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Dune" || updated.SortName != "Dune" || updated.Overview != "A custom description." || updated.ProductionYear == nil || *updated.ProductionYear != 2021 || len(updated.Genres) != 1 || updated.Genres[0] != "Sci-Fi" {
		t.Fatalf("manual metadata was overwritten: %#v", updated)
	}
	if updated.MediaPath != "/Movies/Dune.remux.mkv" || updated.SizeBytes != 200 {
		t.Fatalf("file scan fields were not refreshed: %#v", updated)
	}
}

func TestManualMusicMetadataSavesLyricsAndSurvivesLaterScans(t *testing.T) {
	store := newMediaLibraryStore(t)
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Music", ContentType: medialibrary.ContentMusic, RootPath: "/Music",
	})
	if err != nil {
		t.Fatal(err)
	}
	generation, err := store.BeginLibraryScan(t.Context(), library.ID)
	if err != nil {
		t.Fatal(err)
	}
	item, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, Kind: medialibrary.KindAudio, Path: "/Music/source.flac", MediaPath: "/Music/source.flac",
		Name: "Scanner title", SortName: "Scanner title", Album: "Scanner album", Artists: []string{"Scanner artist"}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	track, disc := 3, 1
	if err := store.UpdateMediaItemMetadata(t.Context(), item.ID, medialibrary.MetadataInput{
		Name: "Manual title", SortName: "Manual title", Album: "Manual album", AlbumArtist: "Manual artist",
		Artists: []string{"Manual artist"}, TrackNumber: &track, DiscNumber: &disc,
		Lyrics: "[00:01.00]Original", TranslatedLyrics: "[00:01.00]Translated",
	}); err != nil {
		t.Fatal(err)
	}

	nextGeneration, err := store.BeginLibraryScan(t.Context(), library.ID)
	if err != nil {
		t.Fatal(err)
	}
	rescanned, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, Kind: medialibrary.KindAudio, Path: item.Path, MediaPath: item.MediaPath,
		Name: "Changed scanner title", SortName: "Changed scanner title", Album: "Changed scanner album",
		Artists: []string{"Changed scanner artist"}, MusicMetadataKey: "netease|https://music.example/api.php", ScanGeneration: nextGeneration,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rescanned.MetadataRefreshNeeded {
		t.Fatal("manually edited music requested a metadata refresh")
	}
	if rescanned.Name != "Manual title" || rescanned.Album != "Manual album" || rescanned.AlbumArtist != "Manual artist" || rescanned.Lyrics != "[00:01.00]Original" || rescanned.TranslatedLyrics != "[00:01.00]Translated" {
		t.Fatalf("manual music metadata was overwritten: %#v", rescanned)
	}
}

func TestUpsertMediaItemRefreshesMetadataOnlyForChangedFiles(t *testing.T) {
	store := newMediaLibraryStore(t)
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Movies", ContentType: medialibrary.ContentMovies, RootPath: "/Movies",
	})
	if err != nil {
		t.Fatal(err)
	}
	modifiedAt := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	input := medialibrary.ItemInput{
		LibraryID: library.ID, Kind: medialibrary.KindMovie, Path: "/Movies/Dune.mkv",
		MediaPath: "/Movies/Dune.mkv", Name: "Dune", SortName: "Dune", Container: "mkv",
		ModifiedAt: &modifiedAt, Artists: []string{}, ScanGeneration: 1,
	}
	item, added, err := store.UpsertMediaItem(t.Context(), input)
	if err != nil || !added || !item.MetadataRefreshNeeded {
		t.Fatalf("new item refresh = %v, added = %v, error = %v", item.MetadataRefreshNeeded, added, err)
	}

	tmdbID := int64(438631)
	input.TMDBID = &tmdbID
	if _, _, err := store.UpsertMediaItem(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	input.TMDBID = nil
	item, added, err = store.UpsertMediaItem(t.Context(), input)
	if err != nil || added || item.MetadataRefreshNeeded {
		t.Fatalf("unchanged item refresh = %v, added = %v, error = %v", item.MetadataRefreshNeeded, added, err)
	}

	changedAt := modifiedAt.Add(time.Minute)
	input.ModifiedAt = &changedAt
	item, _, err = store.UpsertMediaItem(t.Context(), input)
	if err != nil || !item.MetadataRefreshNeeded {
		t.Fatalf("changed item refresh = %v, error = %v", item.MetadataRefreshNeeded, err)
	}

	if err := store.UpdateMediaItemMetadata(t.Context(), item.ID, medialibrary.MetadataInput{Name: "Dune", SortName: "Dune", TMDBID: &tmdbID, Artists: []string{}}); err != nil {
		t.Fatal(err)
	}
	lockedChangedAt := changedAt.Add(time.Minute)
	input.ModifiedAt = &lockedChangedAt
	item, _, err = store.UpsertMediaItem(t.Context(), input)
	if err != nil || item.MetadataRefreshNeeded {
		t.Fatalf("locked item refresh = %v, error = %v", item.MetadataRefreshNeeded, err)
	}
}

func TestListMediaItemsAppliesRequestedSort(t *testing.T) {
	store := newMediaLibraryStore(t)
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Movies", ContentType: medialibrary.ContentMovies, RootPath: "/Movies",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		name string
		year int
		date string
	}{
		{name: "Bravo", year: 2020, date: "2020-06-01"},
		{name: "Alpha", year: 2024, date: "2024-02-10"},
		{name: "Charlie", year: 2018, date: "2018-11-20"},
	} {
		year := item.year
		if _, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
			LibraryID: library.ID, Kind: medialibrary.KindMovie, Path: "/Movies/" + item.name + ".mkv",
			MediaPath: "/Movies/" + item.name + ".mkv", Name: item.name, SortName: item.name,
			ProductionYear: &year, PremiereDate: item.date, Artists: []string{}, ScanGeneration: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}

	page, err := store.ListMediaItems(t.Context(), medialibrary.ItemQuery{
		LibraryID: &library.ID, ParentSet: true, SortBy: "PremiereDate", SortDescending: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 3 || page.Items[0].Name != "Alpha" || page.Items[1].Name != "Bravo" || page.Items[2].Name != "Charlie" {
		t.Fatalf("release date order = %#v", page.Items)
	}

	page, err = store.ListMediaItems(t.Context(), medialibrary.ItemQuery{
		LibraryID: &library.ID, ParentSet: true, SortBy: "SortName",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 3 || page.Items[0].Name != "Alpha" || page.Items[1].Name != "Bravo" || page.Items[2].Name != "Charlie" {
		t.Fatalf("name order = %#v", page.Items)
	}
}

func TestListMediaItemsLimitZeroReturnsAll(t *testing.T) {
	store := newMediaLibraryStore(t)
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Movies", ContentType: medialibrary.ContentMovies, RootPath: "/Movies",
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 3; index++ {
		name := fmt.Sprintf("Movie %d", index)
		if _, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
			LibraryID: library.ID, Kind: medialibrary.KindMovie, Path: "/Movies/" + name + ".mkv",
			MediaPath: "/Movies/" + name + ".mkv", Name: name, SortName: name, ScanGeneration: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}

	all, err := store.ListMediaItems(t.Context(), medialibrary.ItemQuery{LibraryID: &library.ID, Limit: 0})
	if err != nil {
		t.Fatal(err)
	}
	if all.Total != 3 || len(all.Items) != 3 {
		t.Fatalf("unlimited page = total %d items %d", all.Total, len(all.Items))
	}

	limited, err := store.ListMediaItems(t.Context(), medialibrary.ItemQuery{LibraryID: &library.ID, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if limited.Total != 3 || len(limited.Items) != 1 {
		t.Fatalf("limited page = total %d items %d", limited.Total, len(limited.Items))
	}
}

func TestLatestTVSeriesSortsByLatestEpisodeImportDate(t *testing.T) {
	store := newMediaLibraryStore(t)
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Shows", ContentType: medialibrary.ContentTVShows, RootPath: "/Shows",
	})
	if err != nil {
		t.Fatal(err)
	}
	currentTime := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return currentTime }
	createSeries := func(name string) (medialibrary.Item, medialibrary.Item) {
		t.Helper()
		series, _, createErr := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
			LibraryID: library.ID, Kind: medialibrary.KindSeries, Path: "/Shows/" + name,
			Name: name, SortName: name, ScanGeneration: 1,
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		seasonNumber := 1
		season, _, createErr := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
			LibraryID: library.ID, ParentID: &series.ID, Kind: medialibrary.KindSeason,
			Path: "/Shows/" + name + "/Season 01", Name: "Season 1", SortName: "0001",
			SeasonNumber: &seasonNumber, ScanGeneration: 1,
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		return series, season
	}
	newer, newerSeason := createSeries("New Episode Added")
	currentTime = currentTime.Add(time.Hour)
	older, olderSeason := createSeries("Old Episode Added")
	createEpisode := func(series medialibrary.Item, season medialibrary.Item) {
		t.Helper()
		seasonNumber, episodeNumber := 1, 1
		if _, _, createErr := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
			LibraryID: library.ID, ParentID: &season.ID, Kind: medialibrary.KindEpisode,
			Path: "/Shows/" + series.Name + "/Season 01/S01E01.mkv", MediaPath: "/Shows/" + series.Name + "/Season 01/S01E01.mkv",
			Name: "Episode 1", SortName: "0001", SeasonNumber: &seasonNumber, EpisodeNumber: &episodeNumber, ScanGeneration: 1,
		}); createErr != nil {
			t.Fatal(createErr)
		}
	}
	currentTime = currentTime.Add(time.Hour)
	createEpisode(older, olderSeason)
	currentTime = currentTime.Add(time.Hour)
	createEpisode(newer, newerSeason)

	page, err := store.ListMediaItems(t.Context(), medialibrary.ItemQuery{
		LibraryID: &library.ID, Kinds: []medialibrary.ItemKind{medialibrary.KindSeries}, Latest: true, Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].ID != newer.ID || page.Items[1].ID != older.ID {
		t.Fatalf("latest TV series order = %#v", page.Items)
	}
	page, err = store.ListMediaItems(t.Context(), medialibrary.ItemQuery{
		LibraryID: &library.ID, Kinds: []medialibrary.ItemKind{medialibrary.KindSeries},
		SortBy: "DateLastContentAdded", SortDescending: true, Limit: 20,
	})
	if err != nil || len(page.Items) != 2 || page.Items[0].ID != newer.ID || page.Items[1].ID != older.ID {
		t.Fatalf("DateLastContentAdded TV series order = %#v, %v", page.Items, err)
	}
}

func TestMediaItemsSortChineseAndEnglishNamesByPinyin(t *testing.T) {
	store := newMediaLibraryStore(t)
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Movies", ContentType: medialibrary.ContentMovies, RootPath: "/Movies",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"重返未来", "Bravo", "阿凡达", "Avatar", "北京爱情故事"} {
		if _, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
			LibraryID: library.ID, Kind: medialibrary.KindMovie, Path: "/Movies/" + name + ".mkv",
			MediaPath: "/Movies/" + name + ".mkv", Name: name, SortName: name,
			Artists: []string{}, ScanGeneration: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := store.ListMediaItems(t.Context(), medialibrary.ItemQuery{
		LibraryID: &library.ID, ParentSet: true, SortBy: "SortName",
	})
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"阿凡达", "Avatar", "北京爱情故事", "Bravo", "重返未来"}
	if len(page.Items) != len(expected) {
		t.Fatalf("items = %#v", page.Items)
	}
	for index, name := range expected {
		if page.Items[index].Name != name {
			t.Fatalf("name order = %#v", page.Items)
		}
	}
}

func TestMusicMetadataRefreshUsesProviderKeyAndKeepsLocalizedValues(t *testing.T) {
	store := newMediaLibraryStore(t)
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Music", ContentType: medialibrary.ContentMusic, RootPath: "/Music",
	})
	if err != nil {
		t.Fatal(err)
	}
	modifiedAt := time.Date(2026, 8, 7, 8, 0, 0, 0, time.UTC)
	input := medialibrary.ItemInput{
		LibraryID: library.ID, Kind: medialibrary.KindAudio, Path: "/Music/G.E.M. - A.I.N.Y.flac",
		MediaPath: "/Music/G.E.M. - A.I.N.Y.flac", Name: "A.I.N.Y", SortName: "A.I.N.Y",
		ModifiedAt: &modifiedAt, MusicSource: "netease", MusicMetadataKey: "netease|https://music.example/api",
		Artists: []string{"G.E.M."}, ScanGeneration: 1,
	}
	created, _, err := store.UpsertMediaItem(t.Context(), input)
	if err != nil || !created.MetadataRefreshNeeded {
		t.Fatalf("created item = %#v, error = %v", created, err)
	}
	input.Name, input.SortName = "A.I.N.Y. (Album Version)", "A.I.N.Y. (Album Version)"
	input.Album, input.AlbumArtist = "G.E.M.", "G.E.M."
	input.MusicTrackID, input.MusicPictureID, input.MusicLyricID = "track-1", "picture-1", "lyric-1"
	input.Lyrics, input.TranslatedLyrics = "original", "translated"
	localized, _, err := store.UpsertMediaItem(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetMediaItemLocalized(t.Context(), localized.ID, true); err != nil {
		t.Fatal(err)
	}

	input.Name, input.SortName = "A.I.N.Y", "A.I.N.Y"
	input.Album, input.AlbumArtist = "Singles", "G.E.M."
	input.MusicTrackID, input.MusicPictureID, input.MusicLyricID = "", "", ""
	input.Lyrics, input.TranslatedLyrics = "", ""
	unchanged, _, err := store.UpsertMediaItem(t.Context(), input)
	if err != nil || unchanged.MetadataRefreshNeeded {
		t.Fatalf("unchanged item = %#v, error = %v", unchanged, err)
	}
	if unchanged.Name != "A.I.N.Y. (Album Version)" || unchanged.MusicTrackID != "track-1" || unchanged.Lyrics != "original" {
		t.Fatalf("localized metadata was overwritten: %#v", unchanged)
	}

	input.MusicMetadataKey = "netease|https://replacement.example/api"
	changed, _, err := store.UpsertMediaItem(t.Context(), input)
	if err != nil || !changed.MetadataRefreshNeeded {
		t.Fatalf("provider change refresh = %v, error = %v", changed.MetadataRefreshNeeded, err)
	}
}

func TestPersonWorksSelectsCreditLevelBeforePagination(t *testing.T) {
	store := newMediaLibraryStore(t)
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{Name: "TV", ContentType: medialibrary.ContentTVShows, RootPaths: []string{"/TV"}})
	if err != nil {
		t.Fatal(err)
	}
	add := func(kind medialibrary.ItemKind, name string, parent *int64) medialibrary.Item {
		t.Helper()
		item, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{LibraryID: library.ID, Kind: kind, ParentID: parent, Path: "/TV/" + name, Name: name, SortName: name, ScanGeneration: 1})
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	credit := func(item medialibrary.Item, people ...domain.MediaPerson) {
		t.Helper()
		values := make([]medialibrary.LocalPerson, len(people))
		for i, person := range people {
			values[i].Person = person
		}
		if err := store.ReplaceMediaItemPeople(t.Context(), item.ID, values); err != nil {
			t.Fatal(err)
		}
	}
	actor := domain.MediaPerson{ID: 99, Name: "Actor", Type: "Actor"}
	guest := domain.MediaPerson{ID: 100, Name: "Guest", Type: "Actor"}
	director := domain.MediaPerson{ID: 99, Name: "Actor", Type: "Director"}
	series := add(medialibrary.KindSeries, "A regular show", nil)
	season := add(medialibrary.KindSeason, "A season", &series.ID)
	otherSeason := add(medialibrary.KindSeason, "A other season", &series.ID)
	regularEpisode := add(medialibrary.KindEpisode, "B regular episode", &otherSeason.ID)
	directedEpisode := add(medialibrary.KindEpisode, "C directed episode", &season.ID)
	guestSeries := add(medialibrary.KindSeries, "D guest show", nil)
	guestSeason := add(medialibrary.KindSeason, "D guest season", &guestSeries.ID)
	guestEpisode := add(medialibrary.KindEpisode, "E guest episode", &guestSeason.ID)
	movie := add(medialibrary.KindMovie, "F movie", nil)
	credit(season, actor)
	credit(regularEpisode, actor, guest)
	credit(directedEpisode, director)
	credit(guestEpisode, actor)
	credit(movie, actor)
	for _, test := range []struct {
		name   string
		people []int64
		kinds  []medialibrary.ItemKind
		want   []int64
	}{
		{"season regular and guest elsewhere", []int64{99}, nil, []int64{series.ID, directedEpisode.ID, guestEpisode.ID, movie.ID}},
		{"other matching guest is not suppressed", []int64{99, 100}, nil, []int64{series.ID, regularEpisode.ID, directedEpisode.ID, guestEpisode.ID, movie.ID}},
		{"series filter", []int64{99}, []medialibrary.ItemKind{medialibrary.KindSeries}, []int64{series.ID}},
		{"episode filter", []int64{99}, []medialibrary.ItemKind{medialibrary.KindEpisode}, []int64{directedEpisode.ID, guestEpisode.ID}},
		{"guest only", []int64{100}, nil, []int64{regularEpisode.ID}},
		{"unknown", []int64{101}, nil, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			for start := 0; start <= len(test.want); start++ {
				page, err := store.ListMediaItemsForUser(t.Context(), 1, medialibrary.ItemQuery{PersonIDs: test.people, Kinds: test.kinds, Start: start, Limit: 1})
				if err != nil {
					t.Fatal(err)
				}
				if page.Total != len(test.want) {
					t.Fatalf("total = %d want %d", page.Total, len(test.want))
				}
				if start == len(test.want) {
					if len(page.Items) != 0 {
						t.Fatal("unexpected final page")
					}
					continue
				}
				if len(page.Items) != 1 || page.Items[0].ID != test.want[start] {
					t.Fatalf("page %d = %#v", start, page)
				}
			}
		})
	}
	page, err := store.ListMediaItemsForUser(t.Context(), 1, medialibrary.ItemQuery{PersonIDs: []int64{99}, AllowedLibraryIDs: []int64{library.ID + 1}})
	if err != nil || page.Total != 0 {
		t.Fatalf("inaccessible library = %#v, %v", page, err)
	}
	page, err = store.ListMediaItemsForUser(t.Context(), 1, medialibrary.ItemQuery{PersonIDs: []int64{99}, ParentSet: true, ParentID: &guestSeason.ID})
	if err != nil || page.Total != 1 || page.Items[0].ID != guestEpisode.ID {
		t.Fatalf("parent scope = %#v, %v", page, err)
	}
	seasonOnly := domain.MediaPerson{ID: 101, Name: "Season only", Type: "Actor"}
	credit(season, actor, seasonOnly)
	page, err = store.ListMediaItemsForUser(t.Context(), 1, medialibrary.ItemQuery{PersonIDs: []int64{101}, Kinds: []medialibrary.ItemKind{medialibrary.KindEpisode}})
	if err != nil || page.Total != 0 {
		t.Fatalf("inferred episodes = %#v, %v", page, err)
	}
	orphan := add(medialibrary.KindEpisode, "Orphan episode", nil)
	credit(orphan, seasonOnly)
	page, err = store.ListMediaItemsForUser(t.Context(), 1, medialibrary.ItemQuery{PersonIDs: []int64{101}, Kinds: []medialibrary.ItemKind{medialibrary.KindEpisode}})
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != orphan.ID || page.Items[0].SeriesID != nil {
		t.Fatalf("episode without parent metadata = %#v, %v", page, err)
	}
	credit(series, actor)
	page, err = store.ListMediaItemsForUser(t.Context(), 1, medialibrary.ItemQuery{PersonIDs: []int64{99}, Kinds: []medialibrary.ItemKind{medialibrary.KindSeries}})
	if err != nil || page.Total != 1 {
		t.Fatalf("duplicate parent credits = %#v, %v", page, err)
	}
}
