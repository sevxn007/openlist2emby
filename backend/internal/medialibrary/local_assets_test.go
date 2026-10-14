package medialibrary

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAssetFilesStoresUploadedImage(t *testing.T) {
	var encoded bytes.Buffer
	picture := image.NewRGBA(image.Rect(0, 0, 24, 12))
	if err := jpeg.Encode(&encoded, picture, nil); err != nil {
		t.Fatal(err)
	}
	assets, err := NewAssetFiles(filepath.Join(t.TempDir(), "media"))
	if err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(filepath.Join(assets.Root(), ".openlist2emby-media"))
	if err != nil || string(marker) != "openlist2emby-media-v1\n" {
		t.Fatalf("media ownership marker = %q, error = %v", marker, err)
	}
	stored, err := assets.StoreItemImage(t.Context(), 9, ImageBackdrop, bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if stored.MIMEType != "image/jpeg" || stored.Width != 24 || stored.Height != 12 || len(stored.SHA256) != 64 {
		t.Fatalf("unexpected uploaded image: %#v", stored)
	}
	if !assets.Exists(stored.RelativePath) {
		t.Fatalf("uploaded image %q was not saved", stored.RelativePath)
	}
	if _, err := assets.StoreItemImage(t.Context(), 9, ImagePrimary, bytes.NewBufferString("not an image")); err == nil {
		t.Fatal("expected invalid uploaded image to be rejected")
	}
}

func TestAssetFilesDownloadsValidImageAndCachesVariant(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		picture := image.NewRGBA(image.Rect(0, 0, 40, 20))
		for y := 0; y < 20; y++ {
			for x := 0; x < 40; x++ {
				picture.Set(x, y, color.RGBA{R: 40, G: 180, B: 90, A: 255})
			}
		}
		_ = jpeg.Encode(w, picture, &jpeg.Options{Quality: 90})
	}))
	defer server.Close()

	root := filepath.Join(t.TempDir(), "media")
	assets, err := NewAssetFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	download, err := assets.DownloadItemImage(t.Context(), 7, ImagePrimary, server.URL+"/poster", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if download.MIMEType != "image/jpeg" || download.Width != 40 || download.Height != 20 || len(download.SHA256) != 64 {
		t.Fatalf("unexpected download: %#v", download)
	}
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(download.RelativePath)))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("asset permissions = %o", info.Mode().Perm())
	}

	original, err := assets.Original(download.RelativePath, download.MIMEType, download.SHA256, download.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	first, err := assets.Variant(original, ImageVariant{MaxWidth: 10, Format: "png"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := assets.Variant(original, ImageVariant{MaxWidth: 10, Format: "png"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Path != second.Path || first.ETag != second.ETag || first.MIMEType != "image/png" {
		t.Fatalf("variant cache miss: %#v %#v", first, second)
	}
}

func TestAssetFilesBuildsEmbyStyleLibraryThumb(t *testing.T) {
	root := filepath.Join(t.TempDir(), "media")
	assets, err := NewAssetFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	background := image.NewRGBA(image.Rect(0, 0, 160, 90))
	draw.Draw(background, background.Bounds(), &image.Uniform{C: color.RGBA{R: 220, G: 120, B: 40, A: 255}}, image.Point{}, draw.Src)
	if err := jpeg.Encode(&encoded, background, nil); err != nil {
		t.Fatal(err)
	}
	stored, err := assets.StoreItemImage(t.Context(), 1, ImageBackdrop, bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	source, err := assets.Original(stored.RelativePath, stored.MIMEType, stored.SHA256, stored.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	cover, err := assets.Collage([]AssetContent{source}, "library-tag", "TV Shows", ImageVariant{})
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(cover.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	decoded, err := jpeg.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != 960 || decoded.Bounds().Dy() != 540 {
		t.Fatalf("library thumb size = %v", decoded.Bounds())
	}
	corner := color.RGBAModel.Convert(decoded.At(20, 20)).(color.RGBA)
	center := color.RGBAModel.Convert(decoded.At(480, 270)).(color.RGBA)
	if corner.R < 150 || corner.G < 70 || center.R < 150 || center.G < 70 {
		t.Fatalf("library thumb corner=%v center=%v", corner, center)
	}
}

func TestAssetFilesBuildsFourColumnLibraryThumb(t *testing.T) {
	assets, err := NewAssetFiles(filepath.Join(t.TempDir(), "media"))
	if err != nil {
		t.Fatal(err)
	}
	sources := make([]AssetContent, 0, 4)
	colors := []color.RGBA{
		{R: 220, G: 30, B: 30, A: 255},
		{R: 30, G: 220, B: 30, A: 255},
		{R: 30, G: 30, B: 220, A: 255},
		{R: 220, G: 80, B: 30, A: 255},
	}
	for index, fill := range colors {
		picture := image.NewRGBA(image.Rect(0, 0, 45, 80))
		draw.Draw(picture, picture.Bounds(), &image.Uniform{C: fill}, image.Point{}, draw.Src)
		var encoded bytes.Buffer
		if err := jpeg.Encode(&encoded, picture, &jpeg.Options{Quality: 100}); err != nil {
			t.Fatal(err)
		}
		stored, err := assets.StoreItemImage(t.Context(), int64(index+1), ImageBackdrop, bytes.NewReader(encoded.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		source, err := assets.Original(stored.RelativePath, stored.MIMEType, stored.SHA256, stored.UpdatedAt)
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, source)
	}
	cover, err := assets.Collage(sources, "four-column-tag", "TV Shows", ImageVariant{})
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(cover.Path)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(file)
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
	for index, want := range colors {
		corner := color.RGBAModel.Convert(decoded.At(index*240+8, 8)).(color.RGBA)
		pixel := color.RGBAModel.Convert(decoded.At(index*240+120, 270)).(color.RGBA)
		channels := []uint8{pixel.R, pixel.G, pixel.B}
		wantDominant := 0
		if want.G > want.R && want.G > want.B {
			wantDominant = 1
		} else if want.B > want.R && want.B > want.G {
			wantDominant = 2
		}
		if channels[wantDominant] < 70 || channels[wantDominant] < channels[(wantDominant+1)%3]+30 || channels[wantDominant] < channels[(wantDominant+2)%3]+30 || int(corner.R)+int(corner.G)+int(corner.B) < 60 {
			t.Fatalf("column %d corner=%v pixel=%v, want source color near %v", index, corner, pixel, want)
		}
	}
}

func TestFitInsidePreservesPortraitPoster(t *testing.T) {
	tile := image.Rect(0, 0, 240, 540)
	got := fitInside(image.Rect(0, 0, 200, 300), tile)
	if got != image.Rect(0, 90, 240, 450) {
		t.Fatalf("fitInside() = %v, want full poster centered in %v", got, tile)
	}
}

func TestTaskHandlerDeduplicatesConcurrentPersonDownloads(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		time.Sleep(50 * time.Millisecond)
		picture := image.NewRGBA(image.Rect(0, 0, 4, 4))
		_ = jpeg.Encode(w, picture, nil)
	}))
	defer server.Close()
	assets, err := NewAssetFiles(filepath.Join(t.TempDir(), "media"))
	if err != nil {
		t.Fatal(err)
	}
	handler := &TaskHandler{assets: assets}
	start := make(chan struct{})
	var calls sync.WaitGroup
	for index := 0; index < 12; index++ {
		calls.Add(1)
		go func() {
			defer calls.Done()
			<-start
			if _, err := handler.downloadPersonProfile(t.Context(), 42, server.URL+"/profile", "127.0.0.1"); err != nil {
				t.Errorf("downloadPersonProfile() error = %v", err)
			}
		}()
	}
	close(start)
	calls.Wait()
	if requests.Load() != 1 {
		t.Fatalf("profile download requests = %d, want 1", requests.Load())
	}
}

func TestAssetFilesRejectsPrivateManualImageURL(t *testing.T) {
	if _, err := validateRemoteImageURL(t.Context(), "http://127.0.0.1/private.jpg", ""); err == nil {
		t.Fatal("expected private image URL to be rejected")
	}
}

func TestAssetFilesMigratesManagedRoot(t *testing.T) {
	base := t.TempDir()
	oldRoot, newRoot := filepath.Join(base, "old"), filepath.Join(base, "new")
	assets, err := NewAssetFiles(oldRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWriteFile(filepath.Join(oldRoot, "items", "1", "primary", "original.jpg"), []byte("asset")); err != nil {
		t.Fatal(err)
	}
	committed := ""
	if err := assets.MigrateRoot(t.Context(), newRoot, func(path string) error { committed = path; return nil }); err != nil {
		t.Fatal(err)
	}
	if committed != newRoot || assets.Root() != newRoot {
		t.Fatalf("migration did not switch root: committed=%q root=%q", committed, assets.Root())
	}
	if data, err := os.ReadFile(filepath.Join(newRoot, "items", "1", "primary", "original.jpg")); err != nil || string(data) != "asset" {
		t.Fatalf("migrated asset = %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(oldRoot, "items")); !os.IsNotExist(err) {
		t.Fatalf("old managed content still exists: %v", err)
	}
}

func TestLibraryCoverCacheSurvivesCleanupAndRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "media")
	assets, err := NewAssetFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 32, 48)), nil); err != nil {
		t.Fatal(err)
	}
	stored, err := assets.StoreItemImage(t.Context(), 1, ImagePrimary, bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	source, err := assets.Original(stored.RelativePath, stored.MIMEType, stored.SHA256, stored.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	spec := ImageVariant{MaxWidth: 300}
	second := source
	second.ETag = "second-source-tag"
	cover, err := assets.Collage([]AssetContent{source, second}, "cover-cache-test", "Movies", spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := assets.GarbageCollect([]string{stored.RelativePath}, []string{stored.SHA256, second.ETag}); err != nil {
		t.Fatal(err)
	}
	assets, err = NewAssetFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	failLoad := func() ([]AssetContent, error) {
		t.Error("cached cover must not load source artwork")
		return nil, os.ErrNotExist
	}
	cached, err := assets.LibraryCover("cover-cache-test", spec, failLoad)
	if err != nil || cached.Path != cover.Path || cached.ETag != cover.ETag {
		t.Fatalf("cover lost after cleanup/restart: %#v, %v", cached, err)
	}
	// A different requested size can also be generated from the cached collage.
	if _, err := assets.LibraryCover("cover-cache-test", ImageVariant{MaxWidth: 200}, failLoad); err != nil {
		t.Fatal(err)
	}
	// Losing even one source invalidates the collage, while referenced originals remain.
	if err := assets.GarbageCollect([]string{stored.RelativePath}, []string{stored.SHA256}); err != nil {
		t.Fatal(err)
	}
	if !assets.Exists(stored.RelativePath) {
		t.Fatal("referenced source artwork was removed")
	}
	if _, err := os.Stat(cover.Path); !os.IsNotExist(err) {
		t.Fatalf("unreferenced collage variant remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(cover.Path), "library-cover.jpg")); !os.IsNotExist(err) {
		t.Fatalf("unreferenced collage remains: %v", err)
	}
}

func TestLibraryCoverConcurrentRequestsLoadSourcesOnce(t *testing.T) {
	assets, err := NewAssetFiles(filepath.Join(t.TempDir(), "media"))
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 32, 48)), nil); err != nil {
		t.Fatal(err)
	}
	stored, err := assets.StoreItemImage(t.Context(), 1, ImagePrimary, bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	source, err := assets.Original(stored.RelativePath, stored.MIMEType, stored.SHA256, stored.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	var loads atomic.Int32
	load := func() ([]AssetContent, error) {
		loads.Add(1)
		return []AssetContent{source}, nil
	}
	start := make(chan struct{})
	results := make(chan AssetContent, 12)
	var workers sync.WaitGroup
	for range 12 {
		workers.Go(func() {
			<-start
			content, err := assets.LibraryCover("concurrent-cover", ImageVariant{MaxWidth: 300}, load)
			if err != nil {
				t.Error(err)
			}
			results <- content
		})
	}
	close(start)
	workers.Wait()
	close(results)
	if loads.Load() != 1 {
		t.Fatalf("source loads = %d, want 1", loads.Load())
	}
	var first AssetContent
	for content := range results {
		if first.Path == "" {
			first = content
		}
		if content.Path == "" || content != first {
			t.Fatalf("inconsistent concurrent cover: %#v / %#v", content, first)
		}
	}
	if _, err := assets.LibraryCover("retry-cover", ImageVariant{}, func() ([]AssetContent, error) {
		return nil, os.ErrNotExist
	}); !os.IsNotExist(err) {
		t.Fatalf("source failure = %v", err)
	}
	if _, err := assets.LibraryCover("retry-cover", ImageVariant{}, load); err != nil {
		t.Fatalf("failed generation cannot be retried: %v", err)
	}
}
