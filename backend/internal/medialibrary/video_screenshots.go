package medialibrary

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/sevxn007/openlist2emby/backend/internal/task"
)

const (
	videoScreenshotWorkers   = 2
	videoScreenshotUserAgent = "openlist2emby/VideoScreenshot"
)

type screenshotJob struct {
	source  Item
	targets []Item
}

type screenshotOutcome struct {
	generated int
	failed    int
}

func (s *Service) CaptureItemPrimaryImageForUser(ctx context.Context, access UserAccess, itemID int64) (Item, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return Item{}, err
	}
	if s.frames == nil || s.assets == nil {
		return Item{}, ErrVideoScreenshotUnavailable
	}
	if err := s.frames.Available(); err != nil {
		return Item{}, err
	}
	target, err := s.GetItemForUser(ctx, access, itemID)
	if err != nil {
		return Item{}, err
	}
	if err := s.authorizeLibraryEdit(ctx, access, target.LibraryID); err != nil {
		return Item{}, err
	}
	source, err := s.videoSourceForItem(ctx, access, target)
	if err != nil {
		return Item{}, err
	}
	library, err := s.store.GetLibrary(ctx, target.LibraryID)
	if err != nil {
		return Item{}, err
	}
	if library.ContentType == ContentMusic {
		return Item{}, fmt.Errorf("%w: video screenshots are not available for music libraries", ErrInvalidLibrary)
	}
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		return Item{}, err
	}
	if s.clients == nil {
		return Item{}, ErrVideoScreenshotUnavailable
	}
	client := s.clients(settings.OpenListBaseURL, settings.OpenListAdminKey)
	if client == nil {
		return Item{}, ErrVideoScreenshotUnavailable
	}
	store, ok := s.store.(LocalizedMetadataStore)
	if !ok {
		return Item{}, ErrVideoScreenshotUnavailable
	}
	if err := captureVideoScreenshot(ctx, s.frames, s.assets, store, client, source, target, screenshotPercent(library.VideoScreenshotPercent), ImageSourceManualScreenshot); err != nil {
		return Item{}, err
	}
	if paths, tags, referenceErr := store.ListMediaAssetReferences(ctx); referenceErr == nil {
		_ = s.assets.GarbageCollect(paths, tags)
	}
	return s.GetItemForUser(ctx, access, target.ID)
}

func (s *Service) videoSourceForItem(ctx context.Context, access UserAccess, target Item) (Item, error) {
	switch target.Kind {
	case KindMovie, KindEpisode:
		if strings.TrimSpace(target.MediaPath) == "" {
			return Item{}, fmt.Errorf("%w: media item has no playable video", ErrInvalidLibrary)
		}
		return target, nil
	case KindSeries, KindSeason:
		descendants, err := s.listDescendantsForUser(ctx, access, target.ID)
		if err != nil {
			return Item{}, err
		}
		episodes := make([]Item, 0, len(descendants))
		for _, item := range descendants {
			if item.Kind == KindEpisode && strings.TrimSpace(item.MediaPath) != "" {
				episodes = append(episodes, item)
			}
		}
		sortVideoEpisodes(episodes)
		if len(episodes) == 0 {
			return Item{}, fmt.Errorf("%w: media item has no playable video", ErrInvalidLibrary)
		}
		return episodes[0], nil
	default:
		return Item{}, fmt.Errorf("%w: item type does not support video screenshots", ErrInvalidLibrary)
	}
}

func (h *TaskHandler) captureMissingVideoScreenshots(ctx context.Context, library Library, client OpenList, result *ScanResult, execution task.Execution) error {
	if !library.VideoScreenshotEnabled || library.ContentType == ContentMusic {
		return nil
	}
	store, ok := h.store.(LocalizedMetadataStore)
	if !ok || h.assets == nil || h.frames == nil || client == nil {
		return nil
	}
	items, err := h.listLibraryItems(ctx, library.ID)
	if err != nil {
		return err
	}
	jobs, targetCount, err := h.videoScreenshotJobs(ctx, library.ContentType, items, store)
	if err != nil {
		return err
	}
	if targetCount == 0 {
		execution.Report(task.Progress{Stage: "screenshots", Percent: 100})
		return nil
	}

	jobChannel := make(chan screenshotJob)
	outcomes := make(chan screenshotOutcome, targetCount)
	var workers sync.WaitGroup
	workerCount := min(videoScreenshotWorkers, len(jobs))
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range jobChannel {
				frame, captureErr := captureVideoFrame(ctx, h.frames, client, job.source, screenshotPercent(library.VideoScreenshotPercent))
				if captureErr != nil {
					outcomes <- screenshotOutcome{failed: len(job.targets)}
					continue
				}
				for _, target := range job.targets {
					storeErr := storeVideoScreenshot(ctx, h.assets, store, frame, job.source, target, ImageSourceAutoScreenshot)
					if storeErr != nil {
						outcomes <- screenshotOutcome{failed: 1}
					} else {
						outcomes <- screenshotOutcome{generated: 1}
					}
				}
			}
		}()
	}
	go func() {
		defer close(jobChannel)
		for _, job := range jobs {
			select {
			case jobChannel <- job:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(outcomes)
	}()

	completed := 0
	for outcome := range outcomes {
		result.ScreenshotsGenerated += outcome.generated
		result.ScreenshotsFailed += outcome.failed
		completed += outcome.generated + outcome.failed
		execution.Report(task.Progress{Stage: "screenshots", Percent: completed * 100 / targetCount})
	}
	return ctx.Err()
}

func (h *TaskHandler) listLibraryItems(ctx context.Context, libraryID int64) ([]Item, error) {
	const pageSize = 500
	items := make([]Item, 0)
	for start := 0; ; start += pageSize {
		page, err := h.store.ListMediaItems(ctx, ItemQuery{LibraryID: &libraryID, Start: start, Limit: pageSize})
		if err != nil {
			return nil, err
		}
		items = append(items, page.Items...)
		if len(page.Items) == 0 || len(items) >= page.Total {
			return items, nil
		}
	}
}

func (h *TaskHandler) videoScreenshotJobs(ctx context.Context, contentType ContentType, items []Item, store LocalizedMetadataStore) ([]screenshotJob, int, error) {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	images, err := store.ListMediaItemImages(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	targetSources := make(map[int64]Item)
	if contentType == ContentMovies {
		for _, item := range items {
			if item.Kind == KindMovie && strings.TrimSpace(item.MediaPath) != "" {
				targetSources[item.ID] = item
			}
		}
	} else {
		targetSources = tvScreenshotSources(items)
	}

	itemsByID := make(map[int64]Item, len(items))
	for _, item := range items {
		itemsByID[item.ID] = item
	}
	jobsBySource := make(map[int64]*screenshotJob)
	targetIDs := make([]int64, 0, len(targetSources))
	for targetID := range targetSources {
		targetIDs = append(targetIDs, targetID)
	}
	sort.Slice(targetIDs, func(left, right int) bool { return targetIDs[left] < targetIDs[right] })
	for _, targetID := range targetIDs {
		target := itemsByID[targetID]
		source := targetSources[targetID]
		if !h.needsAutomaticScreenshot(images[targetID][ImagePrimary], source) {
			continue
		}
		job := jobsBySource[source.ID]
		if job == nil {
			job = &screenshotJob{source: source}
			jobsBySource[source.ID] = job
		}
		job.targets = append(job.targets, target)
	}
	jobs := make([]screenshotJob, 0, len(jobsBySource))
	for _, job := range jobsBySource {
		jobs = append(jobs, *job)
	}
	sort.Slice(jobs, func(left, right int) bool { return jobs[left].source.ID < jobs[right].source.ID })
	targetCount := 0
	for _, job := range jobs {
		targetCount += len(job.targets)
	}
	return jobs, targetCount, nil
}

func (h *TaskHandler) needsAutomaticScreenshot(existing LocalImage, source Item) bool {
	sourceType := existing.SourceType
	if sourceType == ImageSourceUpload || sourceType == ImageSourceManualScreenshot {
		return false
	}
	if sourceType == ImageSourceAutoScreenshot {
		return existing.SourceVideoFingerprint != videoSourceFingerprint(source) || !h.assets.Exists(existing.RelativePath)
	}
	return existing.SHA256 == "" || !h.assets.Exists(existing.RelativePath)
}

func tvScreenshotSources(items []Item) map[int64]Item {
	result := make(map[int64]Item)
	seasons := make(map[int64]Item)
	episodes := make([]Item, 0)
	for _, item := range items {
		switch item.Kind {
		case KindSeason:
			seasons[item.ID] = item
		case KindEpisode:
			if strings.TrimSpace(item.MediaPath) != "" {
				episodes = append(episodes, item)
			}
		}
	}
	sortVideoEpisodes(episodes)
	seenSeasons := make(map[int64]struct{})
	seenSeries := make(map[int64]struct{})
	for _, episode := range episodes {
		result[episode.ID] = episode
		if episode.ParentID == nil {
			continue
		}
		seasonID := *episode.ParentID
		if _, found := seenSeasons[seasonID]; !found {
			seenSeasons[seasonID] = struct{}{}
			result[seasonID] = episode
		}
		season, found := seasons[seasonID]
		if !found || season.ParentID == nil {
			continue
		}
		seriesID := *season.ParentID
		if _, found := seenSeries[seriesID]; !found {
			seenSeries[seriesID] = struct{}{}
			result[seriesID] = episode
		}
	}
	return result
}

func sortVideoEpisodes(items []Item) {
	sort.SliceStable(items, func(left, right int) bool {
		if comparison := compareOptionalNumber(items[left].SeasonNumber, items[right].SeasonNumber); comparison != 0 {
			return comparison < 0
		}
		if comparison := compareOptionalNumber(items[left].EpisodeNumber, items[right].EpisodeNumber); comparison != 0 {
			return comparison < 0
		}
		leftName := strings.ToLower(items[left].SortName)
		rightName := strings.ToLower(items[right].SortName)
		if leftName != rightName {
			return leftName < rightName
		}
		return items[left].ID < items[right].ID
	})
}

func compareOptionalNumber(left, right *int) int {
	if left == nil && right == nil {
		return 0
	}
	if left == nil {
		return 1
	}
	if right == nil {
		return -1
	}
	return *left - *right
}

func captureVideoScreenshot(ctx context.Context, frames VideoFrameExtractor, assets *AssetFiles, store LocalizedMetadataStore, client OpenList, source, target Item, percent int, sourceType ImageSourceType) error {
	frame, err := captureVideoFrame(ctx, frames, client, source, percent)
	if err != nil {
		return err
	}
	return storeVideoScreenshot(ctx, assets, store, frame, source, target, sourceType)
}

func captureVideoFrame(ctx context.Context, frames VideoFrameExtractor, client OpenList, source Item, percent int) ([]byte, error) {
	if strings.TrimSpace(source.MediaPath) == "" {
		return nil, fmt.Errorf("%w: media item has no playable video", ErrInvalidLibrary)
	}
	if err := frames.Available(); err != nil {
		return nil, err
	}
	sourceURL, err := client.ResolveDownloadURL(ctx, source.MediaPath, videoScreenshotUserAgent)
	if err != nil {
		return nil, ErrVideoScreenshotFailed
	}
	frame, err := frames.Capture(ctx, sourceURL, percent)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrVideoScreenshotUnavailable) {
			return nil, err
		}
		return nil, ErrVideoScreenshotFailed
	}
	return frame, nil
}

func storeVideoScreenshot(ctx context.Context, assets *AssetFiles, store LocalizedMetadataStore, frame []byte, source, target Item, sourceType ImageSourceType) error {
	asset, err := assets.StoreItemImage(ctx, target.ID, ImagePrimary, bytes.NewReader(frame))
	if err != nil {
		return ErrVideoScreenshotFailed
	}
	return store.UpsertMediaItemImage(ctx, LocalImage{
		ItemID: target.ID, ImageType: ImagePrimary, SourceType: sourceType,
		SourceVideoFingerprint: videoSourceFingerprint(source), RelativePath: asset.RelativePath,
		MIMEType: asset.MIMEType, SizeBytes: asset.SizeBytes, Width: asset.Width,
		Height: asset.Height, SHA256: asset.SHA256, UpdatedAt: asset.UpdatedAt,
	})
}

func videoSourceFingerprint(item Item) string {
	modified := ""
	if item.ModifiedAt != nil {
		modified = item.ModifiedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
	}
	value := strings.Join([]string{item.MediaPath, strconv.FormatInt(item.SizeBytes, 10), modified}, "\x00")
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func screenshotPercent(value int) int {
	if value < 1 || value > 99 {
		return 10
	}
	return value
}
