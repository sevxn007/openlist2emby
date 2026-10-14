package medialibrary

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/task"
)

type fakeVideoScreenshotStore struct {
	SettingsStore
	LocalizedMetadataStore
	items  []Item
	images map[int64]map[ImageType]LocalImage
}

func (s *fakeVideoScreenshotStore) ListMediaItems(_ context.Context, query ItemQuery) (ItemPage, error) {
	start := min(query.Start, len(s.items))
	end := min(start+query.Limit, len(s.items))
	return ItemPage{Items: s.items[start:end], Total: len(s.items)}, nil
}

func (s *fakeVideoScreenshotStore) GetMediaItemImage(_ context.Context, itemID int64, imageType ImageType) (LocalImage, error) {
	image := s.images[itemID][imageType]
	if image.SHA256 == "" {
		return LocalImage{}, domain.ErrNotFound
	}
	return image, nil
}

func (s *fakeVideoScreenshotStore) ListMediaItemImages(_ context.Context, itemIDs []int64) (map[int64]map[ImageType]LocalImage, error) {
	result := make(map[int64]map[ImageType]LocalImage, len(itemIDs))
	for _, itemID := range itemIDs {
		result[itemID] = make(map[ImageType]LocalImage)
		for imageType, value := range s.images[itemID] {
			result[itemID][imageType] = value
		}
	}
	return result, nil
}

func (s *fakeVideoScreenshotStore) UpsertMediaItemImage(_ context.Context, value LocalImage) error {
	if s.images == nil {
		s.images = make(map[int64]map[ImageType]LocalImage)
	}
	if s.images[value.ItemID] == nil {
		s.images[value.ItemID] = make(map[ImageType]LocalImage)
	}
	s.images[value.ItemID][value.ImageType] = value
	return nil
}

type fakeVideoScreenshotOpenList struct{}

func (fakeVideoScreenshotOpenList) ListDirectory(context.Context, string, bool) ([]RemoteEntry, error) {
	return nil, nil
}

func (fakeVideoScreenshotOpenList) ResolveDownloadURL(_ context.Context, objectPath, _ string) (string, error) {
	return "video:" + objectPath, nil
}

type fakeVideoFrameExtractor struct {
	mu       sync.Mutex
	frame    []byte
	failURLs map[string]error
	calls    []string
	percents []int
	missing  bool
}

func (f *fakeVideoFrameExtractor) Available() error {
	if f.missing {
		return ErrVideoScreenshotUnavailable
	}
	return nil
}

func (f *fakeVideoFrameExtractor) Capture(_ context.Context, sourceURL string, percent int) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, sourceURL)
	f.percents = append(f.percents, percent)
	if err := f.failURLs[sourceURL]; err != nil {
		return nil, err
	}
	return f.frame, nil
}

func TestAutomaticVideoScreenshotsRollUpFirstEpisodeFrame(t *testing.T) {
	seasonNumber, firstEpisode, secondEpisode := 1, 1, 2
	seriesID, seasonID := int64(1), int64(2)
	items := []Item{
		{ID: 3, LibraryID: 9, ParentID: &seasonID, Kind: KindEpisode, MediaPath: "/TV/Show/S01E02.mkv", SortName: "Episode 2", SeasonNumber: &seasonNumber, EpisodeNumber: &secondEpisode},
		{ID: seriesID, LibraryID: 9, Kind: KindSeries, SortName: "Show"},
		{ID: 4, LibraryID: 9, ParentID: &seasonID, Kind: KindEpisode, MediaPath: "/TV/Show/S01E01.mkv", SortName: "Episode 1", SeasonNumber: &seasonNumber, EpisodeNumber: &firstEpisode},
		{ID: seasonID, LibraryID: 9, ParentID: &seriesID, Kind: KindSeason, SortName: "Season 1", SeasonNumber: &seasonNumber},
	}
	store := &fakeVideoScreenshotStore{items: items, images: make(map[int64]map[ImageType]LocalImage)}
	assets, err := NewAssetFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	frames := &fakeVideoFrameExtractor{frame: testJPEG(t)}
	handler := &TaskHandler{store: store, assets: assets, frames: frames}
	result := ScanResult{}

	err = handler.captureMissingVideoScreenshots(t.Context(), Library{
		ID: 9, ContentType: ContentTVShows, VideoScreenshotEnabled: true, VideoScreenshotPercent: 25,
	}, fakeVideoScreenshotOpenList{}, &result, task.Execution{})
	if err != nil {
		t.Fatal(err)
	}
	if result.ScreenshotsGenerated != 4 || result.ScreenshotsFailed != 0 {
		t.Fatalf("screenshot result = %#v", result)
	}
	if len(frames.calls) != 2 || frames.percents[0] != 25 || frames.percents[1] != 25 {
		t.Fatalf("capture calls = %#v at %#v", frames.calls, frames.percents)
	}
	firstFingerprint := videoSourceFingerprint(items[2])
	for _, targetID := range []int64{seriesID, seasonID, 4} {
		image := store.images[targetID][ImagePrimary]
		if image.SourceType != ImageSourceAutoScreenshot || image.SourceVideoFingerprint != firstFingerprint || !assets.Exists(image.RelativePath) {
			t.Fatalf("target %d image = %#v", targetID, image)
		}
	}
}

func TestAutomaticVideoScreenshotsContinueAfterCaptureFailure(t *testing.T) {
	items := []Item{
		{ID: 1, LibraryID: 7, Kind: KindMovie, MediaPath: "/Movies/Bad.mkv", SortName: "Bad"},
		{ID: 2, LibraryID: 7, Kind: KindMovie, MediaPath: "/Movies/Good.mkv", SortName: "Good"},
	}
	store := &fakeVideoScreenshotStore{items: items, images: make(map[int64]map[ImageType]LocalImage)}
	assets, err := NewAssetFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	frames := &fakeVideoFrameExtractor{
		frame: testJPEG(t), failURLs: map[string]error{"video:/Movies/Bad.mkv": errors.New("synthetic failure")},
	}
	handler := &TaskHandler{store: store, assets: assets, frames: frames}
	result := ScanResult{}

	err = handler.captureMissingVideoScreenshots(t.Context(), Library{
		ID: 7, ContentType: ContentMovies, VideoScreenshotEnabled: true, VideoScreenshotPercent: 10,
	}, fakeVideoScreenshotOpenList{}, &result, task.Execution{})
	if err != nil {
		t.Fatal(err)
	}
	if result.ScreenshotsGenerated != 1 || result.ScreenshotsFailed != 1 {
		t.Fatalf("screenshot result = %#v", result)
	}
	if store.images[1][ImagePrimary].SHA256 != "" || store.images[2][ImagePrimary].SHA256 == "" {
		t.Fatalf("stored images = %#v", store.images)
	}
}

func TestAutomaticVideoScreenshotPreservesUserImagesAndRefreshesChangedVideo(t *testing.T) {
	modified := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	item := Item{ID: 1, Kind: KindMovie, MediaPath: "/Movies/Film.mkv", SizeBytes: 20, ModifiedAt: &modified}
	store := &fakeVideoScreenshotStore{images: make(map[int64]map[ImageType]LocalImage)}
	assets, err := NewAssetFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	handler := &TaskHandler{store: store, assets: assets}
	stored := storedTestImage(t, assets, item.ID)
	stored.SourceType = ImageSourceUpload
	if handler.needsAutomaticScreenshot(stored, item) {
		t.Fatal("uploaded image was selected for replacement")
	}
	stored.SourceType = ImageSourceManualScreenshot
	if handler.needsAutomaticScreenshot(stored, item) {
		t.Fatal("manual screenshot was selected for replacement")
	}
	stored.SourceType = ImageSourceAutoScreenshot
	stored.SourceVideoFingerprint = videoSourceFingerprint(item)
	if handler.needsAutomaticScreenshot(stored, item) {
		t.Fatal("unchanged automatic screenshot was selected for replacement")
	}
	item.SizeBytes++
	if !handler.needsAutomaticScreenshot(stored, item) {
		t.Fatal("changed source video did not refresh its automatic screenshot")
	}
}

func TestManualVideoScreenshotStoresManualSource(t *testing.T) {
	store := &fakeVideoScreenshotStore{images: make(map[int64]map[ImageType]LocalImage)}
	assets, err := NewAssetFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	frames := &fakeVideoFrameExtractor{frame: testJPEG(t)}
	source := Item{ID: 2, Kind: KindEpisode, MediaPath: "/TV/Show/S01E01.mkv", SizeBytes: 100}
	target := Item{ID: 1, Kind: KindSeries}

	err = captureVideoScreenshot(t.Context(), frames, assets, store, fakeVideoScreenshotOpenList{}, source, target, 40, ImageSourceManualScreenshot)
	if err != nil {
		t.Fatal(err)
	}
	image := store.images[target.ID][ImagePrimary]
	if image.SourceType != ImageSourceManualScreenshot || image.SourceVideoFingerprint != videoSourceFingerprint(source) || len(frames.percents) != 1 || frames.percents[0] != 40 {
		t.Fatalf("manual screenshot = %#v, percents = %#v", image, frames.percents)
	}
}

func storedTestImage(t *testing.T, assets *AssetFiles, itemID int64) LocalImage {
	t.Helper()
	asset, err := assets.StoreItemImage(t.Context(), itemID, ImagePrimary, strings.NewReader(string(testJPEG(t))))
	if err != nil {
		t.Fatal(err)
	}
	return LocalImage{
		ItemID: itemID, ImageType: ImagePrimary, SourceType: ImageSourceRemote, RelativePath: asset.RelativePath,
		MIMEType: asset.MIMEType, SizeBytes: asset.SizeBytes, Width: asset.Width,
		Height: asset.Height, SHA256: asset.SHA256, UpdatedAt: asset.UpdatedAt,
	}
}

func testJPEG(t *testing.T) []byte {
	t.Helper()
	frame := image.NewRGBA(image.Rect(0, 0, 8, 4))
	for y := range 4 {
		for x := range 8 {
			frame.Set(x, y, color.RGBA{R: uint8(20 + x), G: uint8(30 + y), B: 80, A: 255})
		}
	}
	var output strings.Builder
	if err := jpeg.Encode(&output, frame, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	return []byte(output.String())
}
