package medialibrary

import (
	"context"
	"encoding/json"
	"path"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/task"
)

type tvIndexStore struct {
	SettingsStore
	nextID int64
	items  map[string]Item
}

func (s *tvIndexStore) GetSettings(context.Context) (domain.Settings, error) {
	return domain.Settings{MediaLibraryEnabled: true}, nil
}

type mediaScanTaskStore struct {
	SettingsStore
	settings domain.Settings
	library  Library
}

func (s *mediaScanTaskStore) GetSettings(context.Context) (domain.Settings, error) {
	return s.settings, nil
}

func (s *mediaScanTaskStore) GetLibrary(_ context.Context, id int64) (Library, error) {
	if id != s.library.ID {
		return Library{}, domain.ErrNotFound
	}
	return s.library, nil
}

func (s *tvIndexStore) UpsertMediaItem(_ context.Context, input ItemInput) (Item, bool, error) {
	if s.items == nil {
		s.items = map[string]Item{}
	}
	key := string(input.Kind) + "\x00" + input.Path
	existing, found := s.items[key]
	if !found {
		s.nextID++
		existing.ID = s.nextID
	}
	metadataRefreshNeeded := !found || existing.TMDBID == nil || modifiedAtChanged(existing.ModifiedAt, input.ModifiedAt)
	item := Item{
		ID: existing.ID, LibraryID: input.LibraryID, ParentID: input.ParentID, Kind: input.Kind,
		Path: input.Path, MediaPath: input.MediaPath, Name: input.Name, SortName: input.SortName,
		Container: input.Container, SizeBytes: input.SizeBytes, ModifiedAt: input.ModifiedAt,
		MetadataPriorityKey: input.MetadataPriorityKey, MetadataSource: input.MetadataSource, ResolvedMetadataSource: input.ResolvedMetadataSource, TVDBID: input.TVDBID, TMDBID: input.TMDBID, IMDbID: input.IMDbID, Genres: input.Genres,
		SeasonNumber: input.SeasonNumber, EpisodeNumber: input.EpisodeNumber,
		ProductionYear: input.ProductionYear, PremiereDate: input.PremiereDate,
		Overview: input.Overview, CommunityRating: input.CommunityRating,
		RuntimeTicks: input.RuntimeTicks, ImageURL: input.ImageURL, BackdropURL: input.BackdropURL,
		Album: input.Album, AlbumArtist: input.AlbumArtist, Artists: input.Artists,
		TrackNumber: input.TrackNumber, DiscNumber: input.DiscNumber,
		Lyrics: input.Lyrics, TranslatedLyrics: input.TranslatedLyrics,
		MusicSource: input.MusicSource, MusicTrackID: input.MusicTrackID, MusicPictureID: input.MusicPictureID,
		MusicLyricID: input.MusicLyricID, MusicMetadataKey: input.MusicMetadataKey,
		MetadataRefreshNeeded: metadataRefreshNeeded,
	}
	if found && existing.TMDBID != nil && input.TMDBID == nil {
		item.TMDBID = existing.TMDBID
	}
	if found && input.MetadataPriorityKey == "" {
		item.MetadataPriorityKey = existing.MetadataPriorityKey
	}
	s.items[key] = item
	return item, !found, nil
}

type fakeMusicMetadataProvider struct {
	searches atomic.Int32
	details  atomic.Int32
}

func (*fakeMusicMetadataProvider) Sources() []string { return []string{"netease", "qmusic", "kuwo"} }

func (p *fakeMusicMetadataProvider) Search(_ context.Context, query, source string, limit int) ([]domain.MusicTrack, error) {
	p.searches.Add(1)
	if limit != 20 || query == "" {
		return nil, context.Canceled
	}
	if source == "qmusic" {
		return []domain.MusicTrack{{ID: "qq-wrong", Name: "A.I.N.Y", Artists: []string{"二宝"}, Source: "qmusic"}}, nil
	}
	if source != "netease" {
		return nil, context.Canceled
	}
	return []domain.MusicTrack{
		{ID: "wrong", Name: "A.I.N.Y", Artists: []string{"二宝"}, Album: "Wrong", Source: "netease"},
		{ID: "right", Name: "A.I.N.Y", Artists: []string{"G.E.M.邓紫棋"}, Album: "18...", PictureID: "pic", LyricID: "lyric", Source: "netease", DurationMS: 269_000},
	}, nil
}

func (p *fakeMusicMetadataProvider) Detail(_ context.Context, source, pictureID, lyricID string) (domain.MusicDetail, error) {
	p.details.Add(1)
	if source != "netease" || pictureID != "pic" || lyricID != "lyric" {
		return domain.MusicDetail{}, context.Canceled
	}
	return domain.MusicDetail{CoverURL: "https://images.example/cover.jpg", Lyrics: "original", TranslatedLyrics: "translated"}, nil
}

func TestIndexMusicUsesConservativeSearchAndPersistsLocalMetadata(t *testing.T) {
	store := &tvIndexStore{}
	provider := &fakeMusicMetadataProvider{}
	handler := &TaskHandler{store: store, music: provider}
	file := DiscoveredFile{Path: "/Music/G.E.M.邓紫棋 - A.I.N.Y.flac", Relative: "G.E.M.邓紫棋 - A.I.N.Y.flac", Name: "G.E.M.邓紫棋 - A.I.N.Y.flac"}
	result := ScanResult{}
	if err := handler.indexMusic(t.Context(), Library{ID: 3, RootPath: "/Music"}, 1, []DiscoveredFile{file}, "", newIndexTracker(&result), task.Execution{}); err != nil {
		t.Fatal(err)
	}
	item := store.item(KindAudio, file.Path)
	if provider.searches.Load() != 3 || provider.details.Load() != 1 || result.MetadataFailed != 0 {
		t.Fatalf("calls = (%d, %d), result = %#v", provider.searches.Load(), provider.details.Load(), result)
	}
	if item.MusicTrackID != "right" || item.Name != "A.I.N.Y" || item.Album != "18..." || item.AlbumArtist != "G.E.M.邓紫棋" || item.Lyrics != "original" || item.TranslatedLyrics != "translated" || item.ImageURL != "https://images.example/cover.jpg" || item.RuntimeTicks != 269_000*10_000 {
		t.Fatalf("music item = %#v", item)
	}
	album := store.item(KindMusicAlbum, "/Music/@artist/G.E.M.邓紫棋/@album/18...")
	artist := store.item(KindMusicArtist, "/Music/@artist/G.E.M.邓紫棋")
	if item.ParentID == nil || *item.ParentID != album.ID || album.ParentID == nil || *album.ParentID != artist.ID {
		t.Fatalf("music hierarchy: artist=%#v album=%#v item=%#v", artist, album, item)
	}
	if album.ImageURL != "https://images.example/cover.jpg" || artist.ImageURL != "https://images.example/cover.jpg" {
		t.Fatalf("music group covers: album=%#v artist=%#v", album, artist)
	}
}

type qqOnlyMusicProvider struct {
	searches atomic.Int32
}

func (*qqOnlyMusicProvider) Sources() []string { return []string{"netease", "qmusic", "kuwo"} }

func (p *qqOnlyMusicProvider) Search(_ context.Context, query, source string, limit int) ([]domain.MusicTrack, error) {
	p.searches.Add(1)
	if query == "" || source != "qmusic" || limit != 20 {
		return nil, context.Canceled
	}
	return []domain.MusicTrack{{ID: "qq", Name: "A.I.N.Y", Artists: []string{"G.E.M.邓紫棋"}, Source: "qmusic"}}, nil
}

func (*qqOnlyMusicProvider) Detail(_ context.Context, source, _, _ string) (domain.MusicDetail, error) {
	if source != "qmusic" {
		return domain.MusicDetail{}, context.Canceled
	}
	return domain.MusicDetail{}, nil
}

func TestIndexMusicSearchesAllConfiguredSourcesWithoutEndpoint(t *testing.T) {
	store := &tvIndexStore{}
	provider := &qqOnlyMusicProvider{}
	handler := &TaskHandler{store: store, music: provider}
	file := DiscoveredFile{Path: "/Music/G.E.M.邓紫棋 - A.I.N.Y.flac", Relative: "G.E.M.邓紫棋 - A.I.N.Y.flac", Name: "G.E.M.邓紫棋 - A.I.N.Y.flac"}
	result := ScanResult{}
	if err := handler.indexMusic(t.Context(), Library{ID: 3, RootPath: "/Music"}, 1, []DiscoveredFile{file}, "", newIndexTracker(&result), task.Execution{}); err != nil {
		t.Fatal(err)
	}
	item := store.item(KindAudio, file.Path)
	if provider.searches.Load() != 3 || item.MusicSource != "qmusic" || item.MusicTrackID != "qq" || result.MetadataFailed != 0 {
		t.Fatalf("searches=%d item=%#v result=%#v", provider.searches.Load(), item, result)
	}
}

type kuwoOnlyMusicProvider struct {
	searches atomic.Int32
}

func (*kuwoOnlyMusicProvider) Sources() []string { return []string{"netease", "qmusic", "kuwo"} }

func (p *kuwoOnlyMusicProvider) Search(_ context.Context, query, source string, limit int) ([]domain.MusicTrack, error) {
	p.searches.Add(1)
	if query == "" || limit != 20 {
		return nil, context.Canceled
	}
	if source != "kuwo" {
		return nil, nil
	}
	return []domain.MusicTrack{{
		ID: "kuwo-track", Name: "A.I.N.Y", Artists: []string{"G.E.M.邓紫棋"}, Album: "18...",
		PictureID: "kuwo-picture", LyricID: "kuwo-lyric", Source: "kuwo", DurationMS: 269_000,
	}}, nil
}

func (*kuwoOnlyMusicProvider) Detail(_ context.Context, source, pictureID, lyricID string) (domain.MusicDetail, error) {
	if source != "kuwo" || pictureID != "kuwo-picture" || lyricID != "kuwo-lyric" {
		return domain.MusicDetail{}, context.Canceled
	}
	return domain.MusicDetail{CoverURL: "https://img.example/kuwo.jpg", Lyrics: "kuwo lyrics"}, nil
}

func TestIndexMusicPersistsKuwoMatch(t *testing.T) {
	store := &tvIndexStore{}
	provider := &kuwoOnlyMusicProvider{}
	handler := &TaskHandler{store: store, music: provider}
	file := DiscoveredFile{Path: "/Music/G.E.M.邓紫棋 - A.I.N.Y.flac", Relative: "G.E.M.邓紫棋 - A.I.N.Y.flac", Name: "G.E.M.邓紫棋 - A.I.N.Y.flac"}
	result := ScanResult{}
	if err := handler.indexMusic(t.Context(), Library{ID: 3, RootPath: "/Music"}, 1, []DiscoveredFile{file}, "", newIndexTracker(&result), task.Execution{}); err != nil {
		t.Fatal(err)
	}
	item := store.item(KindAudio, file.Path)
	if provider.searches.Load() != 3 || result.MetadataFailed != 0 {
		t.Fatalf("searches=%d result=%#v", provider.searches.Load(), result)
	}
	if item.MusicSource != "kuwo" || item.MusicTrackID != "kuwo-track" || item.MusicPictureID != "kuwo-picture" || item.MusicLyricID != "kuwo-lyric" || item.Lyrics != "kuwo lyrics" || item.ImageURL != "https://img.example/kuwo.jpg" {
		t.Fatalf("music item = %#v", item)
	}
}

func TestChooseMusicMatchRejectsWrongArtist(t *testing.T) {
	fallback := ParseTrack("G.E.M.邓紫棋 - A.I.N.Y.flac")
	if _, ok := chooseMusicMatch(fallback, []domain.MusicTrack{{ID: "wrong", Name: "A.I.N.Y", Artists: []string{"二宝"}}}); ok {
		t.Fatal("wrong artist was accepted")
	}
	matched, ok := chooseMusicMatch(fallback, []domain.MusicTrack{
		{ID: "wrong", Name: "A.I.N.Y", Artists: []string{"二宝"}},
		{ID: "right", Name: "A.I.N.Y", Artists: []string{"G.E.M.邓紫棋"}},
	})
	if !ok || matched.ID != "right" {
		t.Fatalf("match = %#v, ok = %v", matched, ok)
	}
}

func TestMusicScanProgressReservesCompletionForFinishedTask(t *testing.T) {
	tests := []struct {
		stage            string
		completed, total int
		start, end, want int
	}{
		{stage: "indexing", completed: 1, total: 4, start: 0, end: 20, want: 5},
		{stage: "indexing", completed: 4, total: 4, start: 0, end: 20, want: 20},
		{stage: "metadata", completed: 4, total: 4, start: 20, end: 70, want: 70},
		{stage: "localizing", completed: 4, total: 4, start: 70, end: 95, want: 95},
		{stage: "localizing", completed: 2, total: 2, start: 95, end: 99, want: 99},
	}
	previous := -1
	for _, test := range tests {
		progress := musicScanProgress(test.stage, test.completed, test.total, test.start, test.end)
		if progress.Stage != test.stage || progress.Percent != test.want {
			t.Fatalf("progress = %#v, want stage %q percent %d", progress, test.stage, test.want)
		}
		if progress.Percent < previous || progress.Percent >= 100 {
			t.Fatalf("music progress must be monotonic and below completion: previous=%d current=%d", previous, progress.Percent)
		}
		previous = progress.Percent
	}
}

func TestIndexMusicGroupsFlatArtistTitleFiles(t *testing.T) {
	store := &tvIndexStore{}
	handler := &TaskHandler{store: store}
	files := []DiscoveredFile{
		{Path: "/Music/G.E.M.邓紫棋 - A.I.N.Y.flac", Relative: "G.E.M.邓紫棋 - A.I.N.Y.flac", Name: "G.E.M.邓紫棋 - A.I.N.Y.flac"},
		{Path: "/Music/G.E.M.邓紫棋,Faouzia – Still Breathing.flac", Relative: "G.E.M.邓紫棋,Faouzia – Still Breathing.flac", Name: "G.E.M.邓紫棋,Faouzia – Still Breathing.flac"},
	}
	result := ScanResult{}
	if err := handler.indexMusic(t.Context(), Library{ID: 3, RootPath: "/Music"}, 1, files, "", newIndexTracker(&result), task.Execution{}); err != nil {
		t.Fatal(err)
	}
	artist := store.item(KindMusicArtist, "/Music/@artist/G.E.M.邓紫棋")
	album := store.item(KindMusicAlbum, "/Music/@artist/G.E.M.邓紫棋/@album/Singles")
	first := store.item(KindAudio, files[0].Path)
	second := store.item(KindAudio, files[1].Path)
	if artist.Name != "G.E.M.邓紫棋" || album.Name != "Singles" || first.Name != "A.I.N.Y" || first.AlbumArtist != "G.E.M.邓紫棋" {
		t.Fatalf("flat music hierarchy: artist=%#v album=%#v first=%#v", artist, album, first)
	}
	if second.Name != "Still Breathing" || len(second.Artists) != 2 || second.Artists[1] != "Faouzia" {
		t.Fatalf("collaboration = %#v", second)
	}
}

func modifiedAtChanged(existing, incoming *time.Time) bool {
	if existing == nil || incoming == nil {
		return existing != nil || incoming != nil
	}
	return !existing.Equal(*incoming)
}

func (s *tvIndexStore) item(kind ItemKind, objectPath string) Item {
	return s.items[string(kind)+"\x00"+objectPath]
}

type tvMetadataProvider struct {
	searches      int
	detailSeasons []int
}

func (p *tvMetadataProvider) Search(_ context.Context, apiKey, query, language string) ([]domain.MediaSearchResult, error) {
	p.searches++
	return []domain.MediaSearchResult{{ID: 100, MediaType: "tv", Name: "Severance"}}, nil
}

func (p *tvMetadataProvider) Detail(_ context.Context, apiKey, mediaType string, id int64, season *int, language string) (domain.MediaDetail, error) {
	if season == nil {
		p.detailSeasons = append(p.detailSeasons, -1)
		return domain.MediaDetail{
			ID: 100, MediaType: "tv", Name: "Severance", Overview: "Series overview",
			PosterURL: "https://images.example/series.jpg",
			Seasons: []domain.MediaSeason{{
				ID: 200, AirDate: "2022-02-18", EpisodeCount: 2, Name: "Season 1",
				Overview: "Season summary", PosterURL: "https://images.example/season-summary.jpg",
				SeasonNumber: 1, VoteAverage: 8.2,
			}},
		}, nil
	}
	p.detailSeasons = append(p.detailSeasons, *season)
	return domain.MediaDetail{
		ID: 200, MediaType: "tv", Name: "Season 1", Overview: "Season detail",
		PosterURL: "https://images.example/season.jpg", FirstAirDate: "2022-02-18",
		VoteAverage: 8.3, SeasonNumber: season,
		Episodes: []domain.MediaEpisode{
			{ID: 301, AirDate: "2022-02-18", EpisodeNumber: 1, Name: "Good News About Hell", Overview: "Episode one", Runtime: 57, SeasonNumber: 1, StillURL: "https://images.example/episode-1.jpg", VoteAverage: 8.4},
			{ID: 302, AirDate: "2022-02-25", EpisodeNumber: 2, Name: "Half Loop", Overview: "Episode two", Runtime: 54, SeasonNumber: 1, StillURL: "https://images.example/episode-2.jpg", VoteAverage: 8.1},
		},
	}, nil
}

func TestDecodeStrictRejectsTrailingJSONValue(t *testing.T) {
	var payload ScanPayload
	if err := decodeStrict(json.RawMessage(`{"library_id":1} {"library_id":2}`), &payload); err == nil {
		t.Fatal("decodeStrict accepted multiple JSON values")
	}
}

func TestMediaScanPreparationPreservesLibraryName(t *testing.T) {
	store := &mediaScanTaskStore{
		settings: domain.Settings{MediaLibraryEnabled: true, OpenListBaseURL: "http://openlist.example"},
		library:  Library{ID: 42, Name: "电影"},
	}
	handler := NewTaskHandler(store, func(string, string) OpenList { return nil }, nil)
	payload, err := handler.Prepare(t.Context(), json.RawMessage(`{"library_id":42}`))
	if err != nil {
		t.Fatal(err)
	}
	var stored storedScanPayload
	if err := json.Unmarshal(payload, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.LibraryID != 42 || stored.LibraryName != "电影" {
		t.Fatalf("stored payload = %#v", stored)
	}
	subject, ok := handler.PublicSummary(payload).(ScanPayload)
	if !ok || subject.LibraryID != 42 || subject.LibraryName != "电影" {
		t.Fatalf("public subject = %#v", handler.PublicSummary(payload))
	}
}

func TestIndexTVShowsAppliesSeasonAndEpisodeMetadata(t *testing.T) {
	store := &tvIndexStore{}
	metadata := &tvMetadataProvider{}
	handler := &TaskHandler{store: store, metadata: metadata}
	files := []DiscoveredFile{
		{Path: "/Shows/Severance/Season 01/Severance.S01E01.mkv", Relative: "Severance/Season 01/Severance.S01E01.mkv", Name: "Severance.S01E01.mkv"},
		{Path: "/Shows/Severance/Season 01/Severance.S01E02.mkv", Relative: "Severance/Season 01/Severance.S01E02.mkv", Name: "Severance.S01E02.mkv"},
	}
	result := ScanResult{}
	err := handler.indexTVShows(
		t.Context(),
		Library{ID: 1, RootPath: "/Shows", ContentType: ContentTVShows},
		1,
		files,
		"synthetic-tmdb-key",
		newIndexTracker(&result),
		task.Execution{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.searches != 1 || len(metadata.detailSeasons) != 2 || metadata.detailSeasons[0] != -1 || metadata.detailSeasons[1] != 1 {
		t.Fatalf("metadata calls: searches=%d seasons=%v", metadata.searches, metadata.detailSeasons)
	}
	if result.Added != 4 || result.MetadataFailed != 0 {
		t.Fatalf("scan result = %#v", result)
	}

	season := store.item(KindSeason, "/Shows/@series/Severance/@season/1")
	if season.TMDBID == nil || *season.TMDBID != 200 || season.Name != "Season 1" || season.SortName != "0001" || season.Overview != "Season detail" || season.ImageURL != "https://images.example/season.jpg" || season.PremiereDate != "2022-02-18" || season.MediaPath != "/Shows/Severance/Season 01" {
		t.Fatalf("season = %#v", season)
	}
	series := store.item(KindSeries, "/Shows/@series/Severance")
	if series.MediaPath != "/Shows/Severance" {
		t.Fatalf("series media path = %q", series.MediaPath)
	}
	episode := store.item(KindEpisode, files[0].Path)
	if episode.TMDBID == nil || *episode.TMDBID != 301 || episode.Name != "Good News About Hell" || episode.Overview != "Episode one" || episode.ImageURL != "https://images.example/episode-1.jpg" || episode.PremiereDate != "2022-02-18" || episode.RuntimeTicks != 57*60*10_000_000 {
		t.Fatalf("episode = %#v", episode)
	}
}

func TestIndexTVShowsIndexesUnnumberedFilesAsIndependentSeries(t *testing.T) {
	store := &tvIndexStore{}
	metadata := &tvMetadataProvider{}
	handler := &TaskHandler{store: store, metadata: metadata}
	library := Library{ID: 1, RootPath: "/Shows", RootPaths: []string{"/Shows", "/Archive"}, ContentType: ContentTVShows}
	files := []DiscoveredFile{
		{Path: "/Shows/One/Severance.mkv", ScanRoot: "/Shows", Relative: "One/Severance.mkv", Name: "Severance.mkv"},
		{Path: "/Archive/One/Severance.mkv", ScanRoot: "/Archive", Relative: "One/Severance.mkv", Name: "Severance.mkv"},
	}
	result := ScanResult{}
	if err := handler.indexTVShows(t.Context(), library, 1, files, "synthetic-tmdb-key", newIndexTracker(&result), task.Execution{}); err != nil {
		t.Fatal(err)
	}
	if result.Added != 6 || result.MetadataFailed != 0 || metadata.searches != 2 {
		t.Fatalf("scan result = %#v, searches = %d", result, metadata.searches)
	}
	for _, file := range files {
		seriesPath := standaloneSeriesPrefix(library.RootPath) + file.Path[1:]
		series := store.item(KindSeries, seriesPath)
		season := store.item(KindSeason, seriesPath+"/@season/1")
		episode := store.item(KindEpisode, file.Path)
		if series.ID == 0 || series.Name != "Severance" || series.TMDBID == nil || *series.TMDBID != 100 || series.MediaPath != path.Dir(file.Path) {
			t.Fatalf("series for %q = %#v", file.Path, series)
		}
		if season.ID == 0 || season.ParentID == nil || *season.ParentID != series.ID || season.SeasonNumber == nil || *season.SeasonNumber != 1 || season.TMDBID != nil {
			t.Fatalf("season for %q = %#v", file.Path, season)
		}
		if episode.ID == 0 || episode.ParentID == nil || *episode.ParentID != season.ID || episode.MediaPath != file.Path || episode.Container != "mkv" || episode.Name != "Severance" || episode.TMDBID != nil {
			t.Fatalf("episode for %q = %#v", file.Path, episode)
		}
	}
	if len(metadata.detailSeasons) != 2 || metadata.detailSeasons[0] != -1 || metadata.detailSeasons[1] != -1 {
		t.Fatalf("unexpected season metadata requests: %v", metadata.detailSeasons)
	}
	firstSeriesID := store.item(KindSeries, standaloneSeriesPrefix(library.RootPath)+"Shows/One/Severance.mkv").ID
	secondSeriesID := store.item(KindSeries, standaloneSeriesPrefix(library.RootPath)+"Archive/One/Severance.mkv").ID
	if firstSeriesID == secondSeriesID {
		t.Fatal("files under different roots share a series")
	}
	rescan := ScanResult{}
	if err := handler.indexTVShows(t.Context(), library, 2, files, "synthetic-tmdb-key", newIndexTracker(&rescan), task.Execution{}); err != nil {
		t.Fatal(err)
	}
	if rescan.Added != 0 || store.item(KindSeries, standaloneSeriesPrefix(library.RootPath)+"Shows/One/Severance.mkv").ID != firstSeriesID || store.item(KindSeries, standaloneSeriesPrefix(library.RootPath)+"Archive/One/Severance.mkv").ID != secondSeriesID {
		t.Fatalf("rescan changed identities: %#v", rescan)
	}
	imported := ScanResult{}
	if err := handler.indexTVShows(t.Context(), library, 2, files[:1], "synthetic-tmdb-key", newIndexTracker(&imported), task.Execution{}); err != nil {
		t.Fatal(err)
	}
	if imported.Added != 0 || store.item(KindEpisode, files[0].Path).ID == 0 {
		t.Fatalf("incremental import changed identity: %#v", imported)
	}
}

type tvBackdropFallbackProvider struct {
	withEpisodeStill bool
}

func (*tvBackdropFallbackProvider) Search(_ context.Context, _, _, _ string) ([]domain.MediaSearchResult, error) {
	return []domain.MediaSearchResult{{ID: 100, MediaType: "tv", Name: "Severance"}}, nil
}

func (p *tvBackdropFallbackProvider) Detail(_ context.Context, _, _ string, id int64, season *int, _ string) (domain.MediaDetail, error) {
	if season == nil {
		return domain.MediaDetail{
			ID: 100, MediaType: "tv", Name: "Severance", BackdropURL: "https://images.example/series-backdrop.jpg",
		}, nil
	}
	return domain.MediaDetail{
		ID: 200, MediaType: "tv", Name: "Season 1", SeasonNumber: season,
		Episodes: []domain.MediaEpisode{{
			ID: 301, EpisodeNumber: 1, SeasonNumber: 1, Name: "Episode one",
			StillURL: func() string {
				if p.withEpisodeStill {
					return "https://images.example/episode-still.jpg"
				}
				return ""
			}(),
		}},
	}, nil
}

func TestIndexTVShowsUsesSeriesBackdropWhenEpisodeHasNoStill(t *testing.T) {
	store := &tvIndexStore{}
	metadata := &tvBackdropFallbackProvider{}
	handler := &TaskHandler{store: store, metadata: metadata}
	file := DiscoveredFile{
		Path:     "/Shows/Severance/Season 01/Severance.S01E01.mkv",
		Relative: "Severance/Season 01/Severance.S01E01.mkv",
		Name:     "Severance.S01E01.mkv",
	}
	result := ScanResult{}
	if err := handler.indexTVShows(t.Context(), Library{ID: 1, RootPath: "/Shows", ContentType: ContentTVShows}, 1, []DiscoveredFile{file}, "synthetic-tmdb-key", newIndexTracker(&result), task.Execution{}); err != nil {
		t.Fatal(err)
	}
	episode := store.item(KindEpisode, file.Path)
	if episode.ImageURL != "https://images.example/series-backdrop.jpg" {
		t.Fatalf("episode image = %q, want series backdrop", episode.ImageURL)
	}
}

func TestIndexTVShowsReplacesDiscoveryThumbnailWithSeriesBackdrop(t *testing.T) {
	store := &tvIndexStore{}
	metadata := &tvBackdropFallbackProvider{}
	handler := &TaskHandler{store: store, metadata: metadata}
	file := DiscoveredFile{
		Path:      "/Shows/Severance/Season 01/Severance.S01E01.mkv",
		Relative:  "Severance/Season 01/Severance.S01E01.mkv",
		Name:      "Severance.S01E01.mkv",
		Thumbnail: "https://openlist.example/thumb.jpg",
	}
	result := ScanResult{}
	if err := handler.indexTVShows(t.Context(), Library{ID: 1, RootPath: "/Shows", ContentType: ContentTVShows}, 1, []DiscoveredFile{file}, "synthetic-tmdb-key", newIndexTracker(&result), task.Execution{}); err != nil {
		t.Fatal(err)
	}
	episode := store.item(KindEpisode, file.Path)
	if episode.ImageURL != "https://images.example/series-backdrop.jpg" {
		t.Fatalf("episode image = %q, want series backdrop", episode.ImageURL)
	}
}

func TestIndexTVShowsReplacesSeriesBackdropWithEpisodeStill(t *testing.T) {
	store := &tvIndexStore{}
	metadata := &tvBackdropFallbackProvider{}
	handler := &TaskHandler{store: store, metadata: metadata}
	file := DiscoveredFile{
		Path:     "/Shows/Severance/Season 01/Severance.S01E01.mkv",
		Relative: "Severance/Season 01/Severance.S01E01.mkv",
		Name:     "Severance.S01E01.mkv",
	}
	first := ScanResult{}
	if err := handler.indexTVShows(t.Context(), Library{ID: 1, RootPath: "/Shows", ContentType: ContentTVShows}, 1, []DiscoveredFile{file}, "synthetic-tmdb-key", newIndexTracker(&first), task.Execution{}); err != nil {
		t.Fatal(err)
	}
	metadata.withEpisodeStill = true
	modified := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	file.ModifiedAt = &modified
	second := ScanResult{}
	if err := handler.indexTVShows(t.Context(), Library{ID: 1, RootPath: "/Shows", ContentType: ContentTVShows}, 2, []DiscoveredFile{file}, "synthetic-tmdb-key", newIndexTracker(&second), task.Execution{}); err != nil {
		t.Fatal(err)
	}
	episode := store.item(KindEpisode, file.Path)
	if episode.ImageURL != "https://images.example/episode-still.jpg" {
		t.Fatalf("episode image = %q, want episode still", episode.ImageURL)
	}
}

func TestIndexTVShowsSkipsTMDBForUnchangedFiles(t *testing.T) {
	store := &tvIndexStore{}
	metadata := &tvMetadataProvider{}
	handler := &TaskHandler{store: store, metadata: metadata}
	modifiedAt := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	files := []DiscoveredFile{
		{Path: "/Shows/Severance/Season 01/Severance.S01E01.mkv", Relative: "Severance/Season 01/Severance.S01E01.mkv", Name: "Severance.S01E01.mkv", ModifiedAt: &modifiedAt},
		{Path: "/Shows/Severance/Season 01/Severance.S01E02.mkv", Relative: "Severance/Season 01/Severance.S01E02.mkv", Name: "Severance.S01E02.mkv", ModifiedAt: &modifiedAt},
	}
	library := Library{ID: 1, RootPath: "/Shows", ContentType: ContentTVShows}
	first := ScanResult{}
	if err := handler.indexTVShows(t.Context(), library, 1, files, "synthetic-tmdb-key", newIndexTracker(&first), task.Execution{}); err != nil {
		t.Fatal(err)
	}
	if first.MetadataFailed != 0 {
		t.Fatalf("first scan metadata failures = %d", first.MetadataFailed)
	}

	second := ScanResult{}
	if err := handler.indexTVShows(t.Context(), library, 2, files, "synthetic-tmdb-key", newIndexTracker(&second), task.Execution{}); err != nil {
		t.Fatal(err)
	}
	if metadata.searches != 1 || len(metadata.detailSeasons) != 2 {
		t.Fatalf("unchanged rescan repeated TMDB requests: searches=%d seasons=%v", metadata.searches, metadata.detailSeasons)
	}
	if second.MetadataFailed != 0 {
		t.Fatalf("unchanged rescan metadata failures = %d", second.MetadataFailed)
	}
}

func TestIndexTVShowsMergesSameSeriesAcrossLibraryRoots(t *testing.T) {
	store := &tvIndexStore{}
	handler := &TaskHandler{store: store}
	files := []DiscoveredFile{
		{
			Path: "/Current/忙忙碌碌寻宝藏/Season 2/忙忙碌碌寻宝藏.S02E01.mkv", Relative: "忙忙碌碌寻宝藏/Season 2/忙忙碌碌寻宝藏.S02E01.mkv",
			ScanRoot: "/Current", Name: "忙忙碌碌寻宝藏.S02E01.mkv",
		},
		{
			Path: "/Archive/忙忙碌碌寻宝藏 (2024)/Season 1/忙忙碌碌寻宝藏.S01E01.mkv", Relative: "忙忙碌碌寻宝藏 (2024)/Season 1/忙忙碌碌寻宝藏.S01E01.mkv",
			ScanRoot: "/Archive", Name: "忙忙碌碌寻宝藏.S01E01.mkv",
		},
	}
	result := ScanResult{}
	if err := handler.indexTVShows(
		t.Context(),
		Library{
			ID: 1, ContentType: ContentTVShows,
			RootPath: "/Current", RootPaths: []string{"/Current", "/Archive"},
		},
		1,
		files,
		"",
		newIndexTracker(&result),
		task.Execution{},
	); err != nil {
		t.Fatal(err)
	}

	series := store.item(KindSeries, "/Current/@series/忙忙碌碌寻宝藏")
	firstSeason := store.item(KindSeason, "/Current/@series/忙忙碌碌寻宝藏/@season/1")
	secondSeason := store.item(KindSeason, "/Current/@series/忙忙碌碌寻宝藏/@season/2")
	if series.ID == 0 || firstSeason.ParentID == nil || secondSeason.ParentID == nil || *firstSeason.ParentID != series.ID || *secondSeason.ParentID != series.ID {
		t.Fatalf("series hierarchy: series=%#v first=%#v second=%#v", series, firstSeason, secondSeason)
	}
	if series.MediaPath != "/Current/忙忙碌碌寻宝藏" || firstSeason.MediaPath != "/Archive/忙忙碌碌寻宝藏 (2024)/Season 1" || secondSeason.MediaPath != "/Current/忙忙碌碌寻宝藏/Season 2" {
		t.Fatalf("source paths: series=%q first=%q second=%q", series.MediaPath, firstSeason.MediaPath, secondSeason.MediaPath)
	}
	if result.Added != 5 {
		t.Fatalf("added items = %d, want 5", result.Added)
	}
}

func TestIndexTVShowsMergesSeasonSuffixedSiblingFolders(t *testing.T) {
	store := &tvIndexStore{}
	handler := &TaskHandler{store: store}
	files := []DiscoveredFile{
		{Path: "/Shows/庆余年 第一季/庆余年.S01E01.mkv", Relative: "庆余年 第一季/庆余年.S01E01.mkv", Name: "庆余年.S01E01.mkv"},
		{Path: "/Shows/庆余年 第二季/庆余年.S02E01.mkv", Relative: "庆余年 第二季/庆余年.S02E01.mkv", Name: "庆余年.S02E01.mkv"},
	}
	result := ScanResult{}
	if err := handler.indexTVShows(t.Context(), Library{ID: 1, RootPath: "/Shows", ContentType: ContentTVShows}, 1, files, "", newIndexTracker(&result), task.Execution{}); err != nil {
		t.Fatal(err)
	}
	series := store.item(KindSeries, "/Shows/@series/庆余年")
	firstSeason := store.item(KindSeason, "/Shows/@series/庆余年/@season/1")
	secondSeason := store.item(KindSeason, "/Shows/@series/庆余年/@season/2")
	if series.ID == 0 || firstSeason.ParentID == nil || secondSeason.ParentID == nil || *firstSeason.ParentID != series.ID || *secondSeason.ParentID != series.ID {
		t.Fatalf("series hierarchy: series=%#v first=%#v second=%#v", series, firstSeason, secondSeason)
	}
	if result.Added != 5 {
		t.Fatalf("added items = %d, want 5", result.Added)
	}
}
