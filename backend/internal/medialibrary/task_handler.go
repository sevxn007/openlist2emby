package medialibrary

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/metadata"
	"github.com/sevxn007/openlist2emby/backend/internal/task"
)

type TaskHandler struct {
	store            SettingsStore
	clients          ClientFactory
	metadata         MetadataProvider
	sources          *metadata.Registry
	music            MusicMetadataProvider
	assets           *AssetFiles
	frames           VideoFrameExtractor
	assetOnce        sync.Once
	assetSlots       chan struct{}
	profileMu        sync.Mutex
	profileDownloads map[int64]*profileDownloadCall
}

const mediaAssetWorkers = 8

type profileDownloadCall struct {
	done     chan struct{}
	download AssetDownload
	err      error
}

type episodeLocalizationJob struct {
	item   Item
	input  ItemInput
	people []domain.MediaPerson
}

type musicScanRecord struct {
	track     ParsedTrack
	item      Item
	input     ItemInput
	scanRoot  string
	thumbnail string
	result    musicEnrichment
}

type musicEnrichment struct {
	track   *domain.MusicTrack
	detail  domain.MusicDetail
	matched bool
	failed  bool
}

func (h *TaskHandler) SetAssetFiles(assets *AssetFiles) { h.assets = assets }

func (h *TaskHandler) SetVideoFrameExtractor(frames VideoFrameExtractor) { h.frames = frames }

type storedScanPayload struct {
	LibraryID       int64    `json:"library_id"`
	LibraryName     string   `json:"library_name"`
	OpenListBaseURL string   `json:"openlist_base_url"`
	Paths           []string `json:"paths,omitempty"`
}

func NewTaskHandler(store SettingsStore, clients ClientFactory, metadata MetadataProvider, music ...MusicMetadataProvider) *TaskHandler {
	handler := &TaskHandler{store: store, clients: clients, metadata: metadata, sources: NewMetadataRegistry(metadata)}
	if len(music) > 0 {
		handler.music = music[0]
	}
	return handler
}

func (h *TaskHandler) Descriptor() task.Descriptor {
	return task.Descriptor{
		Type: ScanTaskType, PayloadVersion: 1, Manual: true, Schedulable: true, Cancelable: true,
	}
}

func (h *TaskHandler) ScheduleIdentity(raw json.RawMessage) (string, bool) {
	var payload storedScanPayload
	if json.Unmarshal(raw, &payload) != nil || payload.LibraryID <= 0 {
		return "", false
	}
	return strconv.FormatInt(payload.LibraryID, 10), true
}

func (h *TaskHandler) Prepare(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	if h == nil || h.store == nil || h.clients == nil {
		return nil, task.ErrTypeUnavailable
	}
	var input ScanPayload
	if err := decodeStrict(raw, &input); err != nil || input.LibraryID <= 0 {
		return nil, task.NewPublicError("invalid_payload", "media library is required", http.StatusBadRequest, false, err)
	}
	settings, err := h.store.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	if !settings.MediaLibraryEnabled {
		return nil, task.NewPublicError("media_library_disabled", "media library is disabled", http.StatusConflict, false, nil)
	}
	library, err := h.store.GetLibrary(ctx, input.LibraryID)
	if err != nil {
		return nil, err
	}
	paths, err := normalizeImportPaths(library, input.Paths)
	if err != nil {
		return nil, task.NewPublicError("invalid_payload", err.Error(), http.StatusBadRequest, false, nil)
	}
	return json.Marshal(storedScanPayload{
		LibraryID:       input.LibraryID,
		LibraryName:     library.Name,
		OpenListBaseURL: settings.OpenListBaseURL,
		Paths:           paths,
	})
}

func (h *TaskHandler) Run(ctx context.Context, raw json.RawMessage, execution task.Execution) (json.RawMessage, error) {
	if h == nil || h.store == nil || h.clients == nil {
		return nil, task.ErrTypeUnavailable
	}
	var payload storedScanPayload
	if err := decodeStrict(raw, &payload); err != nil || payload.LibraryID <= 0 {
		return nil, task.NewPublicError("invalid_payload", "stored media scan payload is invalid", http.StatusUnprocessableEntity, false, err)
	}
	settings, err := h.store.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	if !settings.MediaLibraryEnabled {
		return nil, task.NewPublicError("media_library_disabled", "media library is disabled", http.StatusConflict, false, nil)
	}
	if settings.OpenListBaseURL != payload.OpenListBaseURL {
		return nil, task.NewPublicError("openlist_config_changed", "OpenList configuration changed after the task was created", http.StatusConflict, false, nil)
	}
	if strings.TrimSpace(settings.OpenListAdminKey) == "" {
		return nil, task.NewPublicError("openlist_admin_key_missing", "OpenList administrator key is not configured", http.StatusPreconditionFailed, false, nil)
	}
	library, err := h.store.GetLibrary(ctx, payload.LibraryID)
	if err != nil {
		return nil, err
	}
	client := h.clients(settings.OpenListBaseURL, settings.OpenListAdminKey)
	if client == nil {
		return nil, task.ErrTypeUnavailable
	}
	rootPaths := library.RootPaths
	if len(rootPaths) == 0 && library.RootPath != "" {
		rootPaths = []string{library.RootPath}
	}
	reporter := func(progress ScanProgress) {
		execution.Report(task.Progress{Stage: "scanning", Percent: 0, ScannedDirectories: progress.Directories, ScannedFiles: progress.Files})
	}
	incremental := len(payload.Paths) > 0
	var files []DiscoveredFile
	var scanProgress ScanProgress
	if incremental {
		files, scanProgress, err = DiscoverPaths(ctx, client, library, payload.Paths, reporter)
	} else {
		files, scanProgress, err = DiscoverRoots(ctx, client, rootPaths, reporter)
	}
	if err != nil {
		return nil, err
	}
	generation := library.ScanGeneration
	if !incremental {
		generation, err = h.store.BeginLibraryScan(ctx, library.ID)
		if err != nil {
			return nil, err
		}
	}
	supported := make([]DiscoveredFile, 0, len(files))
	for _, file := range files {
		if IsSupportedFile(library.ContentType, file.Name) {
			supported = append(supported, file)
		}
	}
	result := ScanResult{Directories: scanProgress.Directories, Files: scanProgress.Files}
	tracker := newIndexTracker(&result)
	tracker.incremental = incremental
	switch library.ContentType {
	case ContentMovies:
		err = h.indexMovies(ctx, library, generation, supported, settings.TMDBAPIKey, settings.OpenListBaseURL, tracker, execution)
	case ContentTVShows:
		err = h.indexTVShowsWithSource(ctx, library, generation, supported, settings.TMDBAPIKey, settings.OpenListBaseURL, tracker, execution)
	case ContentMusic:
		err = h.indexMusic(ctx, library, generation, supported, settings.OpenListBaseURL, tracker, execution)
	default:
		err = fmt.Errorf("%w: unsupported content type", ErrInvalidLibrary)
	}
	if err != nil {
		return nil, err
	}
	if !incremental {
		result.Removed, err = h.store.FinishLibraryScan(ctx, library.ID, generation)
		if err != nil {
			return nil, err
		}
	}
	if err := h.captureMissingVideoScreenshots(ctx, library, client, &result, execution, tracker); err != nil {
		return nil, err
	}
	if store, ok := h.store.(LocalizedMetadataStore); ok && h.assets != nil && !incremental {
		paths, tags, referenceErr := store.ListMediaAssetReferences(ctx)
		if referenceErr == nil {
			_ = h.assets.GarbageCollect(paths, tags)
		}
	}
	execution.Report(task.Progress{Stage: "completed", Percent: 100, ScannedDirectories: scanProgress.Directories, ScannedFiles: scanProgress.Files})
	return json.Marshal(result)
}

func (h *TaskHandler) PublicSummary(raw json.RawMessage) any {
	var payload storedScanPayload
	if json.Unmarshal(raw, &payload) != nil {
		return nil
	}
	return ScanPayload{LibraryID: payload.LibraryID, LibraryName: payload.LibraryName, Paths: payload.Paths}
}

func (h *TaskHandler) indexMovies(ctx context.Context, library Library, generation int64, files []DiscoveredFile, apiKey, openListBaseURL string, tracker *indexTracker, execution task.Execution) error {
	for index, file := range files {
		parsed := ParseTitle(file.Name)
		input := ItemInput{
			LibraryID: library.ID, Kind: KindMovie, Path: file.Path, MediaPath: file.Path,
			Name: parsed.Name, SortName: parsed.Name, Container: extension(file.Name), SizeBytes: file.Size,
			ModifiedAt: file.ModifiedAt, ProductionYear: parsed.Year, ImageURL: file.Thumbnail, ScanGeneration: generation,
		}
		item, added, err := h.store.UpsertMediaItem(ctx, input)
		if err != nil {
			return err
		}
		tracker.record(item.ID, added)
		if h.shouldResolveMetadata(ctx, apiKey, item) {
			metadata, ok := h.resolveMetadata(ctx, apiKey, item, "movie", parsed.Name, parsed.Year)
			if ok {
				applyDetail(&input, metadata)
				input.MetadataPriorityKey = h.priorityKey(ctx, apiKey)
				item, _, err = h.store.UpsertMediaItem(ctx, input)
				if err != nil {
					return err
				}
			} else {
				tracker.metadataFailed(item.ID)
			}
		}
		if added {
			item, err = h.reassociateScannedItem(ctx, item, input, tracker)
			if err != nil {
				return err
			}
		}
		input.ImageURL, input.BackdropURL = item.ImageURL, item.BackdropURL
		if !h.localizeScannedItem(ctx, item, input, apiKey, openListBaseURL, tracker) {
			tracker.metadataFailed(item.ID)
		}
		reportIndexing(execution, index+1, len(files))
	}
	return nil
}

func (h *TaskHandler) indexTVShows(ctx context.Context, library Library, generation int64, files []DiscoveredFile, apiKey string, tracker *indexTracker, execution task.Execution) error {
	return h.indexTVShowsWithSource(ctx, library, generation, files, apiKey, "", tracker, execution)
}

func (h *TaskHandler) indexTVShowsWithSource(ctx context.Context, library Library, generation int64, files []DiscoveredFile, apiKey, openListBaseURL string, tracker *indexTracker, execution task.Execution) error {
	seriesItems := map[string]Item{}
	seriesDetails := map[string]domain.MediaDetail{}
	seasonItems := map[string]Item{}
	seasonEpisodes := map[string]map[int]domain.MediaEpisode{}
	seasonEpisodePeople := map[string]map[int][]domain.MediaPerson{}
	episodeJobs := make([]episodeLocalizationJob, 0, len(files))
	metadataEnabled := h.metadataAvailable(ctx, apiKey)
	for index, file := range files {
		episode, ok := ParseEpisode(file.Name)
		standalone := !ok
		segments := strings.Split(file.Relative, "/")
		seriesKey := episode.Series
		if standalone {
			episode = ParsedEpisode{Season: 1, Episode: 1}
			seriesKey = file.Name
		} else if len(segments) > 1 {
			seriesKey = ParseSeriesFolder(segments[0], episode.Series)
		}
		seriesTitle := ParseTitle(seriesKey)
		seriesKey = seriesTitle.Name
		seriesPathKey := seriesKey
		seriesPath := path.Join(library.RootPath, "@series", seriesPathKey)
		if standalone {
			// Keep each unnumbered file independent, even when titles match.
			seriesPath = standaloneSeriesPrefix(library.RootPath) + strings.TrimPrefix(file.Path, "/")
		}
		scanRoot := file.ScanRoot
		if scanRoot == "" {
			scanRoot = library.RootPath
		}
		seriesMediaPath := scanRoot
		if standalone {
			seriesMediaPath = path.Dir(file.Path)
		} else if len(segments) > 1 {
			seriesMediaPath = path.Join(scanRoot, segments[0])
		}
		series, exists := seriesItems[seriesPath]
		if !exists {
			input := ItemInput{LibraryID: library.ID, Kind: KindSeries, Path: seriesPath, MediaPath: seriesMediaPath, Name: seriesTitle.Name, SortName: seriesTitle.Name, ProductionYear: seriesTitle.Year, ScanGeneration: generation}
			var added bool
			var err error
			series, added, err = h.store.UpsertMediaItem(ctx, input)
			if err != nil {
				return err
			}
			tracker.record(series.ID, added)
			if metadataEnabled && h.shouldResolveMetadata(ctx, apiKey, series) {
				metadata, found := h.resolveMetadata(ctx, apiKey, series, "tv", seriesTitle.Name, seriesTitle.Year)
				if found {
					applyDetail(&input, metadata)
					input.MetadataPriorityKey = h.priorityKey(ctx, apiKey)
					series, _, err = h.store.UpsertMediaItem(ctx, input)
					if err != nil {
						return err
					}
					seriesDetails[seriesPath] = metadata
				} else {
					tracker.metadataFailed(series.ID)
				}
			}
			if added && !standalone {
				series, err = h.reassociateScannedItem(ctx, series, input, tracker)
				if err != nil {
					return err
				}
			}
			seriesItems[seriesPath] = series
			input.ImageURL, input.BackdropURL = series.ImageURL, series.BackdropURL
			if !h.localizeScannedItem(ctx, series, input, apiKey, openListBaseURL, tracker) {
				tracker.metadataFailed(series.ID)
			}
		}
		seasonPath := path.Join(seriesPath, "@season", strconv.Itoa(episode.Season))
		season, exists := seasonItems[seasonPath]
		if !exists {
			seasonName := fmt.Sprintf("Season %d", episode.Season)
			input := ItemInput{LibraryID: library.ID, ParentID: &series.ID, Kind: KindSeason, Path: seasonPath, MediaPath: path.Dir(file.Path), Name: seasonName, SortName: fmt.Sprintf("%04d", episode.Season), SeasonNumber: &episode.Season, ScanGeneration: generation}
			if metadata, ok := seriesDetails[seriesPath]; ok && !standalone {
				if seasonMetadata, found := findSeasonMetadata(metadata.Seasons, episode.Season); found {
					applySeasonMetadata(&input, seasonMetadata)
				}
			}
			var added bool
			var err error
			season, added, err = h.store.UpsertMediaItem(ctx, input)
			if err != nil {
				return err
			}
			tracker.record(season.ID, added)
			if !standalone && metadataEnabled && resolvedID(series) > 0 && (season.MetadataRefreshNeeded || seriesDetails[seriesPath].ID > 0) {
				if resolvedID(series) == 0 {
					tracker.metadataFailed(season.ID)
				} else if metadata, found := h.resolveSeason(ctx, apiKey, series, &episode.Season); found {
					applySeasonDetailMetadata(&input, metadata)
					season, _, err = h.store.UpsertMediaItem(ctx, input)
					if err != nil {
						return err
					}
					seasonEpisodes[seasonPath] = episodesByNumber(metadata.Episodes, episode.Season)
				} else {
					tracker.metadataFailed(season.ID)
				}
			}
			if added && !standalone {
				season, err = h.reassociateScannedItem(ctx, season, input, tracker)
				if err != nil {
					return err
				}
			}
			seasonItems[seasonPath] = season
			input.ImageURL, input.BackdropURL = season.ImageURL, season.BackdropURL
			if !h.localizeScannedItem(ctx, season, input, apiKey, openListBaseURL, tracker) {
				tracker.metadataFailed(season.ID)
			}
		}
		episodeTitle := ParseEpisodeTitle(file.Name)
		if standalone {
			episodeTitle = seriesTitle.Name
		}
		if episodeTitle == "" {
			episodeTitle = fmt.Sprintf("Episode %02d", episode.Episode)
		}
		input := ItemInput{
			LibraryID: library.ID, ParentID: &season.ID, Kind: KindEpisode, Path: file.Path, MediaPath: file.Path,
			Name: episodeTitle, SortName: fmt.Sprintf("%04d", episode.Episode), Container: extension(file.Name),
			SizeBytes: file.Size, ModifiedAt: file.ModifiedAt, SeasonNumber: &episode.Season,
			EpisodeNumber: &episode.Episode, ImageURL: file.Thumbnail, ScanGeneration: generation,
		}
		metadataFound := false
		episodeStillFound := false
		if episodes, ok := seasonEpisodes[seasonPath]; ok {
			if metadata, found := episodes[episode.Episode]; found {
				applyEpisodeMetadata(&input, metadata)
				metadataFound = true
				episodeStillFound = strings.TrimSpace(metadata.StillURL) != ""
			}
		}
		item, added, err := h.store.UpsertMediaItem(ctx, input)
		if err != nil {
			return err
		}
		tracker.record(item.ID, added)

		if !standalone && metadataEnabled && resolvedID(series) > 0 && (!item.MetadataLocalized || item.MetadataRefreshNeeded) && seasonEpisodePeople[seasonPath] == nil && !h.hasLocalPeople(ctx, item.ID) {
			settings, settingsErr := h.sourceSettings(ctx, apiKey)
			if settingsErr == nil {
				people, peopleErr := h.sourceRegistry().SeasonEpisodeCredits(ctx, settings, domain.MediaCreditsQuery{Source: resolvedSource(series), ExternalID: resolvedID(series), MediaType: "tv", SeasonNumber: &episode.Season})
				if peopleErr == nil {
					seasonEpisodePeople[seasonPath] = people
				}
			}
		}

		// Episodes that currently use the series backdrop as their primary image
		// should be checked again on later scans. The fallback is deliberately
		// refreshed until TMDB supplies an episode still, so a newly available
		// cover can replace it without requiring a file change.
		episodeMetadataRefreshNeeded := item.MetadataRefreshNeeded || episodeUsesSeriesBackdrop(item, series)
		if !standalone && metadataEnabled && !metadataFound && episodeMetadataRefreshNeeded {
			if resolvedID(series) == 0 {
				tracker.metadataFailed(item.ID)
			} else if metadata, found := h.resolveSeason(ctx, apiKey, series, &episode.Season); found {
				seasonEpisodes[seasonPath] = episodesByNumber(metadata.Episodes, episode.Season)
				if detail, found := seasonEpisodes[seasonPath][episode.Episode]; found {
					applyEpisodeMetadata(&input, detail)
					episodeStillFound = strings.TrimSpace(detail.StillURL) != ""
					item, _, err = h.store.UpsertMediaItem(ctx, input)
					if err != nil {
						return err
					}
				} else {
					tracker.metadataFailed(item.ID)
				}
			} else {
				tracker.metadataFailed(item.ID)
			}
		}
		if applyEpisodeBackdropFallback(&input, item, series, episodeStillFound) {
			item, _, err = h.store.UpsertMediaItem(ctx, input)
			if err != nil {
				return err
			}
		}
		if added && !standalone {
			item, err = h.reassociateScannedItem(ctx, item, input, tracker)
			if err != nil {
				return err
			}
		}
		input.ImageURL, input.BackdropURL = item.ImageURL, item.BackdropURL
		if store, ok := h.store.(LocalizedMetadataStore); ok && h.assets != nil && item.MetadataLocalized && h.localItemFilesPresent(ctx, store, item) {
			reportIndexing(execution, index+1, len(files))
			continue
		}
		episodeJobs = append(episodeJobs, episodeLocalizationJob{item: item, input: input, people: seasonEpisodePeople[seasonPath][episode.Episode]})
		reportIndexing(execution, index+1, len(files))
	}
	return h.localizeEpisodeJobs(ctx, episodeJobs, openListBaseURL, tracker, execution)
}

func standaloneSeriesPrefix(libraryRoot string) string {
	return path.Join(libraryRoot, "@standalone") + "/"
}

// applyEpisodeBackdropFallback supplies the parent series backdrop only when
// an episode has no image of its own. Existing episode artwork is never
// replaced by the fallback; a later metadata refresh can provide a still and
// overwrite the fallback through the normal Upsert path.
func applyEpisodeBackdropFallback(input *ItemInput, item, series Item, episodeStillFound bool) bool {
	backdrop := strings.TrimSpace(series.BackdropURL)
	if backdrop == "" || episodeStillFound {
		return false
	}
	// A thumbnail supplied during discovery is not episode metadata. Replace it
	// with the series backdrop, while preserving an existing independent image
	// (for example, a manually uploaded cover).
	current := strings.TrimSpace(item.ImageURL)
	incoming := strings.TrimSpace(input.ImageURL)
	if current != "" && current != incoming && current != backdrop {
		return false
	}
	input.ImageURL = backdrop
	return incoming != backdrop
}

func episodeUsesSeriesBackdrop(item, series Item) bool {
	backdrop := strings.TrimSpace(series.BackdropURL)
	return backdrop != "" && strings.TrimSpace(item.ImageURL) == backdrop
}

func (h *TaskHandler) localizeEpisodeJobs(ctx context.Context, jobs []episodeLocalizationJob, openListBaseURL string, tracker *indexTracker, execution task.Execution) error {
	if len(jobs) == 0 {
		return nil
	}
	store, ok := h.store.(LocalizedMetadataStore)
	if !ok || h.assets == nil {
		return nil
	}
	jobQueue := make(chan episodeLocalizationJob)
	results := make(chan int64, len(jobs))
	var workers sync.WaitGroup
	for worker := 0; worker < min(mediaAssetWorkers, len(jobs)); worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range jobQueue {
				localized := true
				if len(job.people) > 0 {
					if err := h.persistPeople(ctx, store, job.item.ID, job.people, openListBaseURL); err != nil {
						localized = false
					}
				} else if repaired, exists := h.repairLocalPeople(ctx, store, job.item.ID, openListBaseURL); exists && !repaired {
					localized = false
				}
				if !h.localizeScannedItemImages(ctx, job.item, job.input, openListBaseURL, true) {
					localized = false
				}
				if err := store.SetMediaItemLocalized(ctx, job.item.ID, localized); err != nil {
					localized = false
				}
				if localized {
					results <- 0
				} else {
					results <- job.item.ID
				}
			}
		}()
	}
	go func() {
		defer close(jobQueue)
		for _, job := range jobs {
			select {
			case jobQueue <- job:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() { workers.Wait(); close(results) }()
	completed := 0
	for failedID := range results {
		completed++
		if failedID > 0 {
			tracker.metadataFailed(failedID)
		}
		execution.Report(task.Progress{Stage: "localizing", Percent: completed * 100 / len(jobs)})
	}
	return ctx.Err()
}

func (h *TaskHandler) indexMusic(ctx context.Context, library Library, generation int64, files []DiscoveredFile, openListBaseURL string, tracker *indexTracker, execution task.Execution) error {
	musicSources := h.musicSources()
	musicKey := "smart|" + strings.Join(musicSources, ",")
	defaultMusicSource := ""
	if len(musicSources) > 0 {
		defaultMusicSource = musicSources[0]
	}
	records := make([]*musicScanRecord, 0, len(files))
	for index, file := range files {
		track := ParseTrack(file.Relative)
		scanRoot := file.ScanRoot
		if scanRoot == "" {
			scanRoot = library.RootPath
		}
		input := ItemInput{
			LibraryID: library.ID, Kind: KindAudio, Path: file.Path, MediaPath: file.Path,
			Name: track.Name, SortName: track.Name, Container: extension(file.Name), SizeBytes: file.Size,
			ModifiedAt: file.ModifiedAt, ImageURL: file.Thumbnail, Album: track.Album, AlbumArtist: track.Artist,
			Artists: track.Artists, TrackNumber: track.TrackNumber, DiscNumber: track.DiscNumber,
			MusicSource: defaultMusicSource, MusicMetadataKey: musicKey, ScanGeneration: generation,
		}
		item, added, err := h.store.UpsertMediaItem(ctx, input)
		if err != nil {
			return err
		}
		tracker.record(item.ID, added)
		records = append(records, &musicScanRecord{track: track, item: item, input: input, scanRoot: scanRoot, thumbnail: file.Thumbnail})
		execution.Report(musicScanProgress("indexing", index+1, len(files), 0, 20))
	}
	if h.music != nil && len(musicSources) > 0 {
		h.enrichMusicRecords(ctx, records, execution)
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	artists := map[string]Item{}
	albums := map[string]Item{}
	artistInputs := map[int64]ItemInput{}
	albumInputs := map[int64]ItemInput{}
	for index, record := range records {
		if record.result.failed {
			tracker.metadataFailed(record.item.ID)
		}
		if record.result.matched && record.result.track != nil {
			matched := record.result.track
			record.input.Name = matched.Name
			record.input.SortName = matched.Name
			record.input.Album = firstNonBlank(matched.Album, record.track.Album)
			record.input.AlbumArtist = firstMusicArtist(matched.Artists, record.track.Artist)
			record.input.Artists = musicArtistsOrFallback(matched.Artists, record.track.Artists)
			record.input.MusicSource = firstNonBlank(matched.Source, "netease")
			record.input.MusicTrackID = matched.ID
			record.input.MusicPictureID = matched.PictureID
			record.input.MusicLyricID = matched.LyricID
			if matched.TrackNumber > 0 {
				trackNumber := matched.TrackNumber
				record.input.TrackNumber = &trackNumber
			}
			if matched.DiscNumber > 0 {
				discNumber := matched.DiscNumber
				record.input.DiscNumber = &discNumber
			}
			if matched.DurationMS > 0 {
				record.input.RuntimeTicks = matched.DurationMS * 10_000
			}
			record.input.MusicMetadataKey = musicKey
			record.input.Lyrics = record.result.detail.Lyrics
			record.input.TranslatedLyrics = record.result.detail.TranslatedLyrics
			if record.result.detail.CoverURL != "" {
				record.input.ImageURL = record.result.detail.CoverURL
			}
			updated, _, err := h.store.UpsertMediaItem(ctx, record.input)
			if err != nil {
				return err
			}
			record.item = updated
		}
		artistName := firstNonBlank(record.item.AlbumArtist, record.track.Artist)
		albumName := firstNonBlank(record.item.Album, record.track.Album)
		artistKey := record.scanRoot + "\x00" + normalizeMusicText(artistName)
		artist, exists := artists[artistKey]
		if !exists {
			artistPath := path.Join(record.scanRoot, "@artist", artistName)
			artistInput := ItemInput{
				LibraryID: library.ID, Kind: KindMusicArtist, Path: artistPath, Name: artistName, SortName: artistName,
				ImageURL: firstNonBlank(record.item.ImageURL, record.thumbnail), Artists: []string{artistName}, ScanGeneration: generation,
			}
			var added bool
			var err error
			artist, added, err = h.store.UpsertMediaItem(ctx, artistInput)
			if err != nil {
				return err
			}
			tracker.record(artist.ID, added)
			artists[artistKey] = artist
			artistInputs[artist.ID] = artistInput
		}
		albumKey := artistKey + "\x00" + normalizeMusicText(albumName)
		album, exists := albums[albumKey]
		if !exists {
			albumPath := path.Join(artist.Path, "@album", albumName)
			albumInput := ItemInput{
				LibraryID: library.ID, ParentID: &artist.ID, Kind: KindMusicAlbum, Path: albumPath,
				Name: albumName, SortName: albumName, ImageURL: firstNonBlank(record.item.ImageURL, record.thumbnail),
				Album: albumName, AlbumArtist: artistName, Artists: record.item.Artists, ScanGeneration: generation,
			}
			var added bool
			var err error
			album, added, err = h.store.UpsertMediaItem(ctx, albumInput)
			if err != nil {
				return err
			}
			tracker.record(album.ID, added)
			albums[albumKey] = album
			albumInputs[album.ID] = albumInput
		}
		record.input.Album = album.Name
		record.input.AlbumArtist = artist.Name
		record.input.ParentID = &album.ID
		updated, _, err := h.store.UpsertMediaItem(ctx, record.input)
		if err != nil {
			return err
		}
		record.item = updated
		record.input.ImageURL, record.input.BackdropURL = record.item.ImageURL, record.item.BackdropURL
		if record.result.detail.CoverURL != "" {
			record.input.ImageURL = record.result.detail.CoverURL
			inheritMusicCover(albumInputs, artistInputs, record.input.ParentID, record.result.detail.CoverURL)
		}
		if !h.localizeMusicItem(ctx, record.item, record.input, openListBaseURL) {
			tracker.metadataFailed(record.item.ID)
		}
		if record.result.failed {
			if store, ok := h.store.(LocalizedMetadataStore); ok {
				_ = store.SetMediaItemLocalized(ctx, record.item.ID, false)
			}
		}
		execution.Report(musicScanProgress("localizing", index+1, len(records), 70, 95))
	}
	if err := h.localizeMusicGroups(ctx, albums, albumInputs, artists, artistInputs, openListBaseURL, tracker, execution); err != nil {
		return err
	}
	return nil
}

func inheritMusicCover(albums, artists map[int64]ItemInput, albumID *int64, coverURL string) {
	if albumID == nil || strings.TrimSpace(coverURL) == "" {
		return
	}
	album, ok := albums[*albumID]
	if !ok {
		return
	}
	album.ImageURL = coverURL
	albums[*albumID] = album
	if album.ParentID == nil {
		return
	}
	artist, ok := artists[*album.ParentID]
	if ok {
		artist.ImageURL = coverURL
		artists[*album.ParentID] = artist
	}
}

func (h *TaskHandler) localizeMusicGroups(ctx context.Context, albums map[string]Item, albumInputs map[int64]ItemInput, artists map[string]Item, artistInputs map[int64]ItemInput, openListBaseURL string, tracker *indexTracker, execution task.Execution) error {
	total := len(albums) + len(artists)
	completed := 0
	for _, groups := range []struct {
		items  map[string]Item
		inputs map[int64]ItemInput
	}{{albums, albumInputs}, {artists, artistInputs}} {
		for key, item := range groups.items {
			input := groups.inputs[item.ID]
			if input.ImageURL != "" && input.ImageURL != item.ImageURL {
				updated, _, err := h.store.UpsertMediaItem(ctx, input)
				if err != nil {
					return err
				}
				item = updated
				groups.items[key] = updated
			}
			input.ImageURL = item.ImageURL
			input.BackdropURL = item.BackdropURL
			if !h.localizeMusicItem(ctx, item, input, openListBaseURL) {
				tracker.metadataFailed(item.ID)
			}
			completed++
			execution.Report(musicScanProgress("localizing", completed, total, 95, 99))
		}
	}
	return nil
}

func (h *TaskHandler) enrichMusicRecords(ctx context.Context, records []*musicScanRecord, execution task.Execution) {
	if len(records) == 0 {
		return
	}
	jobs := make(chan *musicScanRecord)
	results := make(chan *musicScanRecord, len(records))
	workerCount := min(mediaAssetWorkers, len(records))
	var workers sync.WaitGroup
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for record := range jobs {
				if record.item.MetadataRefreshNeeded {
					record.result = h.lookupMusic(ctx, record.track)
				}
				results <- record
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, record := range records {
			select {
			case jobs <- record:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()
	completed := 0
	for range results {
		completed++
		execution.Report(musicScanProgress("metadata", completed, len(records), 20, 70))
	}
}

func musicScanProgress(stage string, completed, total, start, end int) task.Progress {
	percent := end
	if total > 0 && completed < total {
		percent = start + completed*(end-start)/total
	}
	return task.Progress{Stage: stage, Percent: percent}
}

func (h *TaskHandler) lookupMusic(ctx context.Context, fallback ParsedTrack) musicEnrichment {
	query := strings.TrimSpace(fallback.Name)
	if fallback.Artist != "" && fallback.Artist != "Unknown Artist" {
		query = strings.TrimSpace(query + " " + fallback.Artist)
	}
	results := h.searchMusicSources(ctx, query)
	if len(results) == 0 && query != fallback.Name {
		results = h.searchMusicSources(ctx, fallback.Name)
	}
	if len(results) == 0 {
		return musicEnrichment{failed: true}
	}
	best, ok := chooseMusicMatch(fallback, results)
	if !ok {
		return musicEnrichment{failed: true}
	}
	detail, detailErr := h.music.Detail(ctx, firstNonBlank(best.Source, "netease"), best.PictureID, best.LyricID)
	if detailErr != nil {
		return musicEnrichment{track: &best, matched: true, failed: true}
	}
	return musicEnrichment{track: &best, detail: detail, matched: true}
}

func (h *TaskHandler) searchMusicSources(ctx context.Context, query string) []domain.MusicTrack {
	sources := h.musicSources()
	type searchResult struct {
		index  int
		tracks []domain.MusicTrack
	}
	results := make(chan searchResult, len(sources))
	for index, source := range sources {
		index := index
		source := source
		go func() {
			tracks, err := h.music.Search(ctx, query, source, 20)
			if err != nil {
				log.Printf("music metadata search failed source=%s: %s", source, err.Error())
				tracks = nil
			}
			results <- searchResult{index: index, tracks: tracks}
		}()
	}
	bySource := make([][]domain.MusicTrack, len(sources))
	for range sources {
		select {
		case result := <-results:
			bySource[result.index] = result.tracks
		case <-ctx.Done():
			return nil
		}
	}
	combined := make([]domain.MusicTrack, 0, len(sources)*20)
	for _, tracks := range bySource {
		combined = append(combined, tracks...)
	}
	return combined
}

func (h *TaskHandler) musicSources() []string {
	if provider, ok := h.music.(MusicSourceProvider); ok {
		return provider.Sources()
	}
	return []string{"netease", "qmusic", "kuwo"}
}

func chooseMusicMatch(fallback ParsedTrack, results []domain.MusicTrack) (domain.MusicTrack, bool) {
	bestScore := 0
	var best domain.MusicTrack
	for _, result := range results {
		titleScore := musicTextScore(fallback.Name, result.Name)
		if titleScore == 0 {
			continue
		}
		score := titleScore
		artistMatch := false
		for _, artist := range result.Artists {
			if musicTextScore(fallback.Artist, artist) == 100 {
				artistMatch = true
				break
			}
		}
		if fallback.Artist != "" && fallback.Artist != "Unknown Artist" {
			if artistMatch {
				score += 50
			} else {
				score -= 30
			}
		}
		if musicTextScore(fallback.Album, result.Album) == 100 {
			score += 15
		}
		if score > bestScore {
			bestScore, best = score, result
		}
	}
	required := 100
	if fallback.Artist != "" && fallback.Artist != "Unknown Artist" {
		required = 150
	}
	if bestScore < required {
		return domain.MusicTrack{}, false
	}
	return best, true
}

func musicTextScore(left, right string) int {
	left, right = normalizeMusicText(left), normalizeMusicText(right)
	if left == "" || right == "" {
		return 0
	}
	if left == right {
		return 100
	}
	if strings.Contains(left, right) || strings.Contains(right, left) {
		return 60
	}
	return 0
}

func normalizeMusicText(value string) string {
	return strings.Map(func(character rune) rune {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			return unicode.ToLower(character)
		}
		return -1
	}, strings.TrimSpace(value))
}

func firstNonBlank(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}

func firstMusicArtist(values []string, fallback string) string {
	if len(values) > 0 {
		return firstNonBlank(values[0], fallback)
	}
	return fallback
}

func musicArtistsOrFallback(values, fallback []string) []string {
	if len(values) > 0 {
		return values
	}
	return fallback
}

func (h *TaskHandler) localizeMusicItem(ctx context.Context, item Item, input ItemInput, openListBaseURL string) bool {
	store, ok := h.store.(LocalizedMetadataStore)
	if !ok || h.assets == nil {
		return true
	}
	if item.MetadataLocalized && !item.MetadataRefreshNeeded && h.localItemFilesPresent(ctx, store, item) {
		return true
	}
	localized := h.localizeScannedItemImages(ctx, item, input, openListBaseURL, true)
	if err := store.SetMediaItemLocalized(ctx, item.ID, localized); err != nil {
		return false
	}
	return localized
}

func (h *TaskHandler) localizeScannedItem(ctx context.Context, item Item, input ItemInput, apiKey, openListBaseURL string, tracker *indexTracker) bool {
	return h.localizeScannedItemWithPolicy(ctx, item, input, apiKey, openListBaseURL, tracker, true, false)
}

func (h *TaskHandler) localizeScannedItemWithPolicy(ctx context.Context, item Item, input ItemInput, apiKey, openListBaseURL string, tracker *indexTracker, preserveUserImages, force bool) bool {
	store, ok := h.store.(LocalizedMetadataStore)
	if !ok || h.assets == nil {
		return true
	}
	if !force && item.MetadataLocalized && !item.MetadataRefreshNeeded && h.localItemFilesPresent(ctx, store, item) {
		return true
	}
	succeeded := h.localizeScannedItemImages(ctx, item, input, openListBaseURL, preserveUserImages)
	if h.metadataAvailable(ctx, apiKey) && (item.Kind == KindMovie || item.Kind == KindSeries || item.Kind == KindSeason) {
		peopleReady, localPeopleExist := h.repairLocalPeople(ctx, store, item.ID, openListBaseURL)
		if localPeopleExist {
			if !peopleReady {
				succeeded = false
			}
		} else if query, found := h.scanCreditsQuery(ctx, item); found {
			if settings, settingsErr := h.sourceSettings(ctx, apiKey); settingsErr == nil {
				people, err := h.sourceRegistry().Credits(ctx, settings, query)
				if err != nil || h.persistPeople(ctx, store, item.ID, people, openListBaseURL) != nil {
					succeeded = false
				}
			}
		}
	}
	if err := store.SetMediaItemLocalized(ctx, item.ID, succeeded); err != nil {
		return false
	}
	return succeeded
}

func (h *TaskHandler) hasLocalPeople(ctx context.Context, itemID int64) bool {
	store, ok := h.store.(LocalizedMetadataStore)
	if !ok {
		return false
	}
	people, err := store.ListMediaItemPeople(ctx, itemID)
	return err == nil && len(people) > 0
}

func (h *TaskHandler) repairLocalPeople(ctx context.Context, store LocalizedMetadataStore, itemID int64, openListBaseURL string) (bool, bool) {
	people, err := store.ListMediaItemPeople(ctx, itemID)
	if err != nil || len(people) == 0 {
		return err == nil, false
	}
	values := make([]LocalPerson, 0, len(people))
	for _, person := range people {
		stored, personErr := store.GetMediaPerson(ctx, person.ID)
		if personErr != nil {
			return false, true
		}
		if stored.ProfileSourceURL == "" || h.assets.Exists(stored.ProfileRelativePath) {
			continue
		}
		values = append(values, stored)
	}
	return h.downloadPeopleProfiles(ctx, store, values, openListBaseURL) == nil, true
}

func (h *TaskHandler) localizeScannedItemImages(ctx context.Context, item Item, input ItemInput, openListBaseURL string, preserveUserImages bool) bool {
	store, ok := h.store.(LocalizedMetadataStore)
	if !ok || h.assets == nil {
		return true
	}
	allowedHost := hostName(openListBaseURL)
	errorsFound := make(chan error, 2)
	var downloads sync.WaitGroup
	for _, image := range []struct {
		kind   ImageType
		source string
	}{{ImagePrimary, input.ImageURL}, {ImageBackdrop, input.BackdropURL}} {
		if strings.TrimSpace(image.source) == "" {
			continue
		}
		image := image
		downloads.Add(1)
		go func() {
			defer downloads.Done()
			existing, err := store.GetMediaItemImage(ctx, item.ID, image.kind)
			if err == nil && preserveUserImages {
				sourceType := existing.SourceType
				if sourceType == ImageSourceUpload || sourceType == ImageSourceManualScreenshot {
					return
				}
			}
			if err == nil && existing.SourceURL == image.source && h.assets.Exists(existing.RelativePath) {
				return
			}
			download, err := h.downloadItemImage(ctx, item.ID, image.kind, image.source, allowedHost)
			if err == nil {
				err = store.UpsertMediaItemImage(ctx, LocalImage{ItemID: item.ID, ImageType: image.kind, SourceType: ImageSourceRemote, SourceURL: image.source, RelativePath: download.RelativePath, MIMEType: download.MIMEType, SizeBytes: download.SizeBytes, Width: download.Width, Height: download.Height, SHA256: download.SHA256, UpdatedAt: download.UpdatedAt})
			}
			if err != nil {
				errorsFound <- err
			}
		}()
	}
	downloads.Wait()
	close(errorsFound)
	return len(errorsFound) == 0
}

func (h *TaskHandler) localItemFilesPresent(ctx context.Context, store LocalizedMetadataStore, item Item) bool {
	images, err := store.ListMediaItemImages(ctx, []int64{item.ID})
	if err != nil {
		return false
	}
	primary := images[item.ID][ImagePrimary]
	if item.ImageURL != "" {
		if primary.RelativePath == "" {
			return false
		}
		sourceType := primary.SourceType
		if sourceType != ImageSourceUpload && sourceType != ImageSourceManualScreenshot && primary.SourceURL != item.ImageURL {
			return false
		}
	}
	backdrop := images[item.ID][ImageBackdrop]
	if item.BackdropURL != "" {
		if backdrop.RelativePath == "" {
			return false
		}
		sourceType := backdrop.SourceType
		if sourceType != ImageSourceUpload && sourceType != ImageSourceManualScreenshot && backdrop.SourceURL != item.BackdropURL {
			return false
		}
	}
	for _, image := range images[item.ID] {
		if !h.assets.Exists(image.RelativePath) {
			return false
		}
	}
	people, err := store.ListMediaItemPeople(ctx, item.ID)
	if err != nil {
		return false
	}
	for _, person := range people {
		stored, err := store.GetMediaPerson(ctx, person.ID)
		if err != nil {
			return false
		}
		if stored.ProfileSourceURL != "" && !h.assets.Exists(stored.ProfileRelativePath) {
			return false
		}
	}
	return true
}

func (h *TaskHandler) scanCreditsQuery(ctx context.Context, item Item) (domain.MediaCreditsQuery, bool) {
	service := &Service{store: h.store}
	query, found, err := service.creditsQueryForItem(ctx, UserAccess{UserID: 1, IsAdmin: true}, item)
	return query, found && err == nil
}

func (h *TaskHandler) persistPeople(ctx context.Context, store LocalizedMetadataStore, itemID int64, people []domain.MediaPerson, openListBaseURL string) error {
	values := make([]LocalPerson, 0, len(people))
	for _, person := range people {
		values = append(values, LocalPerson{Person: person})
	}
	if err := store.ReplaceMediaItemPeople(ctx, itemID, values); err != nil {
		return err
	}
	pending := make([]LocalPerson, 0, len(people))
	for _, value := range values {
		person := value.Person
		if person.ProfileURL == "" {
			continue
		}
		existing, err := store.GetMediaPerson(ctx, person.ID)
		if err == nil && existing.ProfileSourceURL == person.ProfileURL && h.assets.Exists(existing.ProfileRelativePath) {
			continue
		}
		existing.Person = person
		existing.ProfileSourceURL = person.ProfileURL
		pending = append(pending, existing)
	}
	return h.downloadPeopleProfiles(ctx, store, pending, openListBaseURL)
}

func (h *TaskHandler) downloadPeopleProfiles(ctx context.Context, store LocalizedMetadataStore, people []LocalPerson, openListBaseURL string) error {
	if len(people) == 0 {
		return nil
	}
	allowedHost := hostName(openListBaseURL)
	errorsFound := make(chan error, len(people))
	var downloads sync.WaitGroup
	for _, value := range people {
		value := value
		downloads.Add(1)
		go func() {
			defer downloads.Done()
			download, err := h.downloadPersonProfile(ctx, value.Person.ID, value.ProfileSourceURL, allowedHost)
			if err == nil {
				value.ProfileRelativePath, value.ProfileMIMEType = download.RelativePath, download.MIMEType
				value.ProfileSizeBytes, value.ProfileWidth, value.ProfileHeight = download.SizeBytes, download.Width, download.Height
				value.ProfileSHA256, value.UpdatedAt = download.SHA256, download.UpdatedAt
				err = store.SetMediaPersonProfile(ctx, value.Person.ID, value)
			}
			if err != nil {
				errorsFound <- err
			}
		}()
	}
	downloads.Wait()
	close(errorsFound)
	for err := range errorsFound {
		return err
	}
	return nil
}

func (h *TaskHandler) downloadItemImage(ctx context.Context, itemID int64, imageType ImageType, sourceURL, allowedHost string) (AssetDownload, error) {
	if err := h.acquireAssetSlot(ctx); err != nil {
		return AssetDownload{}, err
	}
	defer h.releaseAssetSlot()
	return h.assets.DownloadItemImage(ctx, itemID, imageType, sourceURL, allowedHost)
}

func (h *TaskHandler) downloadPersonProfile(ctx context.Context, personID int64, sourceURL, allowedHost string) (AssetDownload, error) {
	h.profileMu.Lock()
	if h.profileDownloads == nil {
		h.profileDownloads = make(map[int64]*profileDownloadCall)
	}
	if active := h.profileDownloads[personID]; active != nil {
		h.profileMu.Unlock()
		select {
		case <-active.done:
			return active.download, active.err
		case <-ctx.Done():
			return AssetDownload{}, ctx.Err()
		}
	}
	call := &profileDownloadCall{done: make(chan struct{})}
	h.profileDownloads[personID] = call
	h.profileMu.Unlock()

	if err := h.acquireAssetSlot(ctx); err != nil {
		call.err = err
	} else {
		call.download, call.err = h.assets.DownloadPersonImage(ctx, personID, sourceURL, allowedHost)
		h.releaseAssetSlot()
	}
	h.profileMu.Lock()
	delete(h.profileDownloads, personID)
	close(call.done)
	h.profileMu.Unlock()
	return call.download, call.err
}

func (h *TaskHandler) acquireAssetSlot(ctx context.Context) error {
	h.assetOnce.Do(func() { h.assetSlots = make(chan struct{}, mediaAssetWorkers) })
	select {
	case h.assetSlots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *TaskHandler) releaseAssetSlot() { <-h.assetSlots }

func hostName(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

func findSeasonMetadata(seasons []domain.MediaSeason, number int) (domain.MediaSeason, bool) {
	for _, season := range seasons {
		if season.SeasonNumber == number {
			return season, true
		}
	}
	return domain.MediaSeason{}, false
}

func episodesByNumber(episodes []domain.MediaEpisode, seasonNumber int) map[int]domain.MediaEpisode {
	result := make(map[int]domain.MediaEpisode, len(episodes))
	for _, episode := range episodes {
		if episode.SeasonNumber == seasonNumber {
			result[episode.EpisodeNumber] = episode
		}
	}
	return result
}

func applySeasonMetadata(input *ItemInput, detail domain.MediaSeason) {
	setInputIdentity(input, detail.Source, detail.ID)
	if name := strings.TrimSpace(detail.Name); name != "" {
		input.Name = name
	}
	input.Overview = detail.Overview
	input.CommunityRating = roundedRating(detail.VoteAverage)
	input.ImageURL = detail.PosterURL
	applyPremiereDate(input, detail.AirDate)
}

func applySeasonDetailMetadata(input *ItemInput, detail domain.MediaDetail) {
	if detail.ID > 0 {
		setInputIdentity(input, detail.Source, detail.ID)
	}
	if name := strings.TrimSpace(detail.Name); name != "" {
		input.Name = name
	}
	if strings.TrimSpace(detail.Overview) != "" {
		input.Overview = detail.Overview
	}
	if detail.VoteAverage != 0 {
		input.CommunityRating = roundedRating(detail.VoteAverage)
	}
	if detail.PosterURL != "" {
		input.ImageURL = detail.PosterURL
	}
	if detail.BackdropURL != "" {
		input.BackdropURL = detail.BackdropURL
	}
	premiereDate := detail.ReleaseDate
	if premiereDate == "" {
		premiereDate = detail.FirstAirDate
	}
	if premiereDate != "" {
		applyPremiereDate(input, premiereDate)
	}
}

func applyEpisodeMetadata(input *ItemInput, detail domain.MediaEpisode) {
	setInputIdentity(input, detail.Source, detail.ID)
	if name := strings.TrimSpace(detail.Name); name != "" {
		input.Name = name
	}
	input.Overview = detail.Overview
	input.CommunityRating = roundedRating(detail.VoteAverage)
	if detail.Runtime > 0 {
		input.RuntimeTicks = int64(detail.Runtime) * 60 * 10_000_000
	}
	if detail.StillURL != "" {
		input.ImageURL = detail.StillURL
	}
	applyPremiereDate(input, detail.AirDate)
}

func applyDetail(input *ItemInput, detail domain.MediaDetail) {
	setInputIdentity(input, detail.Source, detail.ID)
	input.IMDbID = detail.IMDbID
	input.Genres = detail.Genres
	if title := strings.TrimSpace(detail.Title); title != "" {
		input.Name, input.SortName = title, title
	} else if name := strings.TrimSpace(detail.Name); name != "" {
		input.Name, input.SortName = name, name
	}
	input.Overview = detail.Overview
	input.CommunityRating = roundedRating(detail.VoteAverage)
	input.ImageURL = detail.PosterURL
	input.BackdropURL = detail.BackdropURL
	if detail.Runtime > 0 {
		input.RuntimeTicks = int64(detail.Runtime) * 60 * 10_000_000
	}
	premiereDate := detail.ReleaseDate
	if premiereDate == "" {
		premiereDate = detail.FirstAirDate
	}
	applyPremiereDate(input, premiereDate)
}

func applyPremiereDate(input *ItemInput, value string) {
	input.PremiereDate = value
	if len(input.PremiereDate) >= 4 {
		if year, err := strconv.Atoi(input.PremiereDate[:4]); err == nil {
			input.ProductionYear = &year
		}
	}
}

type indexTracker struct {
	incremental    bool
	result         *ScanResult
	added          map[int64]struct{}
	updated        map[int64]struct{}
	metadataErrors map[int64]struct{}
}

func newIndexTracker(result *ScanResult) *indexTracker {
	return &indexTracker{result: result, added: map[int64]struct{}{}, updated: map[int64]struct{}{}, metadataErrors: map[int64]struct{}{}}
}

func (t *indexTracker) record(id int64, added bool) {
	if added {
		if _, exists := t.added[id]; !exists {
			t.added[id] = struct{}{}
			t.result.Added++
		}
		return
	}
	if _, wasAdded := t.added[id]; wasAdded {
		return
	}
	if _, exists := t.updated[id]; !exists {
		t.updated[id] = struct{}{}
		t.result.Updated++
	}
}

func (t *indexTracker) metadataFailed(id int64) {
	if _, exists := t.metadataErrors[id]; !exists {
		t.metadataErrors[id] = struct{}{}
		t.result.MetadataFailed++
	}
}

func (h *TaskHandler) reassociateScannedItem(ctx context.Context, item Item, input ItemInput, tracker *indexTracker) (Item, error) {
	// A partial discovery cannot establish that an older path has disappeared.
	if tracker.incremental {
		return item, nil
	}
	store, ok := h.store.(MediaItemReassociationStore)
	if !ok {
		return item, nil
	}
	updated, reassociated, err := store.ReassociateMediaItem(ctx, item.ID, input)
	if err != nil || !reassociated {
		return item, err
	}
	tracker.reassociated(item.ID, updated.ID)
	return updated, nil
}

func (t *indexTracker) reassociated(temporaryID, existingID int64) {
	if _, exists := t.added[temporaryID]; exists {
		delete(t.added, temporaryID)
		if t.result.Added > 0 {
			t.result.Added--
		}
	}
	delete(t.updated, temporaryID)
	if _, exists := t.updated[existingID]; !exists {
		t.updated[existingID] = struct{}{}
		t.result.Updated++
	}
}

func reportIndexing(execution task.Execution, completed, total int) {
	percent := 100
	if total > 0 {
		percent = completed * 100 / total
	}
	execution.Report(task.Progress{Stage: "indexing", Percent: percent})
}

func extension(name string) string {
	return strings.TrimPrefix(strings.ToLower(path.Ext(name)), ".")
}

func roundedRating(value float32) float64 {
	return math.Round(float64(value)*10) / 10
}

func decodeStrict(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

var _ task.Handler = (*TaskHandler)(nil)
