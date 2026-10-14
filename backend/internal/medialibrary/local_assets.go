package medialibrary

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	_ "golang.org/x/image/webp"
)

const (
	assetMarkerName = ".openlist2emby-media"
	assetMarkerID   = "openlist2emby-media-v1"
	maxImageBytes   = 25 << 20
	maxImagePixels  = 100_000_000
	maxDerivedSide  = 4096
	// MaxUploadedImageBytes is shared with the HTTP upload boundary. Image
	// decoding still validates the actual content and pixel count below.
	MaxUploadedImageBytes = maxImageBytes
)

type AssetFiles struct {
	mu         sync.RWMutex
	root       string
	client     *http.Client
	coverLocks [32]sync.Mutex
}

type AssetDownload struct {
	RelativePath string
	MIMEType     string
	SizeBytes    int64
	Width        int
	Height       int
	SHA256       string
	UpdatedAt    time.Time
}

type ImageVariant struct {
	MaxWidth  int
	MaxHeight int
	Width     int
	Height    int
	Quality   int
	Format    string
}

type AssetContent struct {
	Path         string
	MIMEType     string
	SizeBytes    int64
	ETag         string
	LastModified time.Time
}

func (f *AssetFiles) MigrateRoot(ctx context.Context, target string, commit func(string) error) error {
	target, err := validateAssetRoot(target)
	if err != nil {
		return err
	}
	oldRoot := f.Root()
	if target == oldRoot {
		return nil
	}
	if strings.HasPrefix(target, oldRoot+string(filepath.Separator)) || strings.HasPrefix(oldRoot, target+string(filepath.Separator)) {
		return fmt.Errorf("media metadata directories must not contain one another")
	}
	if err := ensureManagedAssetRoot(target); err != nil {
		return err
	}
	if err := copyManagedAssets(ctx, oldRoot, target); err != nil {
		return err
	}
	f.mu.Lock()
	if err := copyManagedAssets(ctx, oldRoot, target); err != nil {
		f.mu.Unlock()
		return err
	}
	if commit != nil {
		if err := commit(target); err != nil {
			f.mu.Unlock()
			return err
		}
	}
	f.root = target
	f.mu.Unlock()
	_ = cleanManagedAssetRoot(oldRoot)
	return nil
}

func copyManagedAssets(ctx context.Context, source, target string) error {
	for _, directory := range []string{"items", "people", "derived"} {
		sourceDirectory := filepath.Join(source, directory)
		if _, err := os.Stat(sourceDirectory); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(sourceDirectory, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			relative, err := filepath.Rel(source, path)
			if err != nil {
				return err
			}
			destination, err := secureAssetPath(target, relative)
			if err != nil {
				return err
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("managed media directory contains a symbolic link")
			}
			if entry.IsDir() {
				return os.MkdirAll(destination, 0o700)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if existing, readErr := os.ReadFile(destination); readErr == nil && sha256.Sum256(existing) == sha256.Sum256(data) {
				return nil
			}
			if err := atomicWriteFile(destination, data); err != nil {
				return err
			}
			verified, err := os.ReadFile(destination)
			if err != nil {
				return err
			}
			if sha256.Sum256(verified) != sha256.Sum256(data) {
				return fmt.Errorf("media metadata copy verification failed")
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func cleanManagedAssetRoot(root string) error {
	marker, err := os.ReadFile(filepath.Join(root, assetMarkerName))
	if err != nil || strings.TrimSpace(string(marker)) != assetMarkerID {
		return fmt.Errorf("refusing to clean an unmanaged media metadata directory")
	}
	var result error
	for _, name := range []string{"items", "people", "derived", assetMarkerName} {
		result = errors.Join(result, os.RemoveAll(filepath.Join(root, name)))
	}
	return result
}

func NewAssetFiles(root string) (*AssetFiles, error) {
	files := &AssetFiles{}
	files.client = &http.Client{Timeout: 45 * time.Second}
	if err := files.SetRoot(root); err != nil {
		return nil, err
	}
	return files, nil
}

func (f *AssetFiles) Root() string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.root
}

func (f *AssetFiles) Exists(relativePath string) bool {
	target, err := secureAssetPath(f.Root(), relativePath)
	if err != nil {
		return false
	}
	info, err := os.Stat(target)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}

func (f *AssetFiles) GarbageCollect(referencedPaths, referencedTags []string) error {
	// Do not collect a collage while it is being published or resized.
	for i := range f.coverLocks {
		f.coverLocks[i].Lock()
	}
	defer func() {
		for i := range f.coverLocks {
			f.coverLocks[i].Unlock()
		}
	}()
	paths := make(map[string]struct{}, len(referencedPaths))
	for _, value := range referencedPaths {
		paths[filepath.Clean(filepath.FromSlash(value))] = struct{}{}
	}
	tags := make(map[string]struct{}, len(referencedTags))
	for _, value := range referencedTags {
		if value != "" {
			tags[value] = struct{}{}
		}
	}
	root := f.Root()
	// Collage hashes differ from individual artwork hashes. Keep their variants
	// while all source artwork is referenced, including across server restarts.
	manifests, err := filepath.Glob(filepath.Join(root, "derived", "*", "library-cover-sources.json"))
	if err != nil {
		return err
	}
	for _, manifest := range manifests {
		data, readErr := os.ReadFile(manifest)
		var sources []string
		if readErr != nil || json.Unmarshal(data, &sources) != nil || len(sources) == 0 {
			continue
		}
		keep := true
		for _, source := range sources {
			if _, referenced := tags[source]; !referenced {
				keep = false
				break
			}
		}
		if keep {
			tags[filepath.Base(filepath.Dir(manifest))] = struct{}{}
		}
	}
	for _, directory := range []string{"items", "people", "derived"} {
		base := filepath.Join(root, directory)
		err := filepath.WalkDir(base, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				if os.IsNotExist(walkErr) {
					return nil
				}
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			keep := false
			if directory == "derived" {
				parts := strings.Split(filepath.ToSlash(relative), "/")
				if len(parts) >= 3 {
					_, keep = tags[parts[1]]
				}
			} else {
				_, keep = paths[filepath.Clean(relative)]
			}
			if !keep {
				return os.Remove(path)
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (f *AssetFiles) SetRoot(root string) error {
	root, err := validateAssetRoot(root)
	if err != nil {
		return err
	}
	if err := ensureManagedAssetRoot(root); err != nil {
		return err
	}
	f.mu.Lock()
	f.root = root
	f.mu.Unlock()
	return nil
}

func validateAssetRoot(root string) (string, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "." || !filepath.IsAbs(root) {
		return "", fmt.Errorf("media metadata path must be absolute")
	}
	volume := filepath.VolumeName(root)
	if root == string(filepath.Separator) || root == volume+string(filepath.Separator) {
		return "", fmt.Errorf("media metadata path must not be a filesystem root")
	}
	return root, nil
}

func ensureManagedAssetRoot(root string) error {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("create media metadata directory: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return fmt.Errorf("secure media metadata directory: %w", err)
	}
	marker := filepath.Join(root, assetMarkerName)
	data, err := os.ReadFile(marker)
	if err == nil {
		if strings.TrimSpace(string(data)) != assetMarkerID {
			return fmt.Errorf("media metadata directory belongs to another installation")
		}
		return os.Chmod(marker, 0o600)
	}
	if !os.IsNotExist(err) {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("media metadata directory must be empty or managed by this project")
	}
	return os.WriteFile(marker, []byte(assetMarkerID+"\n"), 0o600)
}

func (f *AssetFiles) DownloadItemImage(ctx context.Context, itemID int64, imageType ImageType, sourceURL, allowedHost string) (AssetDownload, error) {
	relativeDir := filepath.Join("items", strconv.FormatInt(itemID, 10), string(imageType))
	return f.download(ctx, sourceURL, allowedHost, relativeDir)
}

func (f *AssetFiles) StoreItemImage(ctx context.Context, itemID int64, imageType ImageType, reader io.Reader) (AssetDownload, error) {
	if err := ctx.Err(); err != nil {
		return AssetDownload{}, err
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxImageBytes+1))
	if err != nil {
		return AssetDownload{}, err
	}
	if len(data) == 0 || len(data) > maxImageBytes {
		return AssetDownload{}, fmt.Errorf("image exceeds the %d byte limit", maxImageBytes)
	}
	relativeDir := filepath.Join("items", strconv.FormatInt(itemID, 10), string(imageType))
	return f.storeImageData(data, relativeDir)
}

func (f *AssetFiles) DownloadPersonImage(ctx context.Context, personID int64, sourceURL, allowedHost string) (AssetDownload, error) {
	relativeDir := filepath.Join("people", strconv.FormatInt(personID, 10), "primary")
	return f.download(ctx, sourceURL, allowedHost, relativeDir)
}

func (f *AssetFiles) download(ctx context.Context, sourceURL, allowedHost, relativeDir string) (AssetDownload, error) {
	parsed, err := validateRemoteImageURL(ctx, sourceURL, allowedHost)
	if err != nil {
		return AssetDownload{}, err
	}
	client := *f.client
	redirects := 0
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		redirects++
		if redirects > 3 {
			return errors.New("too many image redirects")
		}
		_, validateErr := validateRemoteImageURL(req.Context(), req.URL.String(), allowedHost)
		return validateErr
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return AssetDownload{}, err
	}
	req.Header.Set("Accept", "image/webp,image/png,image/jpeg,image/gif")
	response, err := client.Do(req)
	if err != nil {
		return AssetDownload{}, fmt.Errorf("download image: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return AssetDownload{}, fmt.Errorf("download image: upstream returned %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxImageBytes+1))
	if err != nil {
		return AssetDownload{}, err
	}
	if len(data) == 0 || len(data) > maxImageBytes {
		return AssetDownload{}, fmt.Errorf("image exceeds the %d byte limit", maxImageBytes)
	}
	return f.storeImageData(data, relativeDir)
}

func (f *AssetFiles) storeImageData(data []byte, relativeDir string) (AssetDownload, error) {
	mimeType, width, height, err := inspectImage(data)
	if err != nil {
		return AssetDownload{}, err
	}
	extension := imageExtension(mimeType)
	if extension == "" {
		return AssetDownload{}, fmt.Errorf("unsupported image type %q", mimeType)
	}
	sum := sha256.Sum256(data)
	tag := hex.EncodeToString(sum[:])
	relativePath := filepath.Join(relativeDir, "original-"+tag[:16]+extension)
	f.mu.RLock()
	defer f.mu.RUnlock()
	root := f.root
	target, err := secureAssetPath(root, relativePath)
	if err != nil {
		return AssetDownload{}, err
	}
	if err := atomicWriteFile(target, data); err != nil {
		return AssetDownload{}, err
	}
	now := time.Now().UTC()
	return AssetDownload{RelativePath: filepath.ToSlash(relativePath), MIMEType: mimeType, SizeBytes: int64(len(data)), Width: width, Height: height, SHA256: tag, UpdatedAt: now}, nil
}

func validateRemoteImageURL(ctx context.Context, rawURL, allowedHost string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("image source must be an HTTP or HTTPS URL")
	}
	host := strings.ToLower(parsed.Hostname())
	allowedHost = strings.ToLower(strings.TrimSpace(allowedHost))
	trusted := host == "image.tmdb.org" || strings.HasSuffix(host, ".tmdb.org") || (allowedHost != "" && host == allowedHost)
	addresses, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve image host: %w", err)
	}
	if !trusted {
		for _, address := range addresses {
			if address.IsLoopback() || address.IsPrivate() || address.IsUnspecified() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() {
				return nil, fmt.Errorf("image source points to a local or private network")
			}
		}
	}
	return parsed, nil
}

func inspectImage(data []byte) (string, int, int, error) {
	contentType := http.DetectContentType(data)
	switch contentType {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
	default:
		return "", 0, 0, fmt.Errorf("unsupported or invalid image content")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", 0, 0, fmt.Errorf("decode image header: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > maxImagePixels {
		return "", 0, 0, fmt.Errorf("image dimensions are invalid or too large")
	}
	return contentType, config.Width, config.Height, nil
}

func imageExtension(mimeType string) string {
	switch mimeType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ""
	}
}

func atomicWriteFile(target string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".asset-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, target)
}

func secureAssetPath(root, relative string) (string, error) {
	root = filepath.Clean(root)
	target := filepath.Clean(filepath.Join(root, filepath.FromSlash(relative)))
	if target == root || !strings.HasPrefix(target, root+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid media asset path")
	}
	return target, nil
}

func (f *AssetFiles) Original(relativePath, mimeType, tag string, updatedAt time.Time) (AssetContent, error) {
	target, err := secureAssetPath(f.Root(), relativePath)
	if err != nil {
		return AssetContent{}, err
	}
	info, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return AssetContent{}, os.ErrNotExist
		}
		return AssetContent{}, err
	}
	if updatedAt.IsZero() {
		updatedAt = info.ModTime()
	}
	return AssetContent{Path: target, MIMEType: mimeType, SizeBytes: info.Size(), ETag: tag, LastModified: updatedAt}, nil
}

func (f *AssetFiles) Variant(source AssetContent, spec ImageVariant) (AssetContent, error) {
	spec = normalizeImageVariant(spec)
	if spec.Width == 0 && spec.Height == 0 && spec.MaxWidth == 0 && spec.MaxHeight == 0 && spec.Format == "" {
		return source, nil
	}
	if source.MIMEType == "image/gif" {
		return AssetContent{}, fmt.Errorf("GIF images can only be returned without resizing")
	}
	keyData, _ := json.Marshal(spec)
	keySum := sha256.Sum256(keyData)
	extension := ".jpg"
	mimeType := "image/jpeg"
	if spec.Format == "png" {
		extension, mimeType = ".png", "image/png"
	}
	relative := filepath.Join("derived", source.ETag, hex.EncodeToString(keySum[:8])+extension)
	target, err := secureAssetPath(f.Root(), relative)
	if err != nil {
		return AssetContent{}, err
	}
	if info, statErr := os.Stat(target); statErr == nil {
		return AssetContent{Path: target, MIMEType: mimeType, SizeBytes: info.Size(), ETag: source.ETag + "-" + hex.EncodeToString(keySum[:8]), LastModified: info.ModTime()}, nil
	}
	input, err := os.Open(source.Path)
	if err != nil {
		return AssetContent{}, err
	}
	decoded, _, err := image.Decode(input)
	_ = input.Close()
	if err != nil {
		return AssetContent{}, err
	}
	width, height := variantDimensions(decoded.Bounds().Dx(), decoded.Bounds().Dy(), spec)
	destination := image.NewRGBA(image.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(destination, destination.Bounds(), decoded, decoded.Bounds(), xdraw.Over, nil)
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return AssetContent{}, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".derived-*")
	if err != nil {
		return AssetContent{}, err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	_ = temporary.Chmod(0o600)
	if mimeType == "image/png" {
		err = png.Encode(temporary, destination)
	} else {
		err = jpeg.Encode(temporary, destination, &jpeg.Options{Quality: spec.Quality})
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return AssetContent{}, err
	}
	if err := os.Rename(temporaryName, target); err != nil {
		return AssetContent{}, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return AssetContent{}, err
	}
	return AssetContent{Path: target, MIMEType: mimeType, SizeBytes: info.Size(), ETag: source.ETag + "-" + hex.EncodeToString(keySum[:8]), LastModified: info.ModTime()}, nil
}

// LibraryCover loads source artwork only on a cache miss. The bounded locks
// serialize requests for the same cover through generation and variant creation.
func (f *AssetFiles) LibraryCover(tag string, spec ImageVariant, loadSources func() ([]AssetContent, error)) (AssetContent, error) {
	if strings.TrimSpace(tag) == "" {
		return AssetContent{}, os.ErrNotExist
	}
	key := sha256.Sum256([]byte(tag))
	lock := &f.coverLocks[int(key[0])%len(f.coverLocks)]
	lock.Lock()
	defer lock.Unlock()
	target, err := secureAssetPath(f.Root(), filepath.Join("derived", tag, "library-cover.jpg"))
	if err != nil {
		return AssetContent{}, err
	}
	if info, statErr := os.Stat(target); statErr == nil {
		base := AssetContent{Path: target, MIMEType: "image/jpeg", SizeBytes: info.Size(), ETag: tag, LastModified: info.ModTime()}
		return f.Variant(base, spec)
	}
	sources, err := loadSources()
	if err != nil {
		return AssetContent{}, err
	}
	if len(sources) == 0 {
		return AssetContent{}, os.ErrNotExist
	}
	return f.buildCollage(sources, tag, target, spec)
}

func (f *AssetFiles) Collage(sources []AssetContent, tag, libraryName string, spec ImageVariant) (AssetContent, error) {
	return f.LibraryCover(tag, spec, func() ([]AssetContent, error) { return sources, nil })
}

func (f *AssetFiles) buildCollage(sources []AssetContent, tag, target string, spec ImageVariant) (AssetContent, error) {
	sourceTags := make([]string, 0, len(sources))
	for _, source := range sources {
		sourceTags = append(sourceTags, source.ETag)
	}

	const width, height = 960, 540
	destination := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(destination, destination.Bounds(), &image.Uniform{C: color.Black}, image.Point{}, draw.Src)
	for index, source := range sources {
		file, openErr := os.Open(source.Path)
		if openErr != nil {
			return AssetContent{}, openErr
		}
		decoded, _, decodeErr := image.Decode(file)
		_ = file.Close()
		if decodeErr != nil {
			return AssetContent{}, decodeErr
		}
		tileWidth := width / len(sources)
		tile := image.Rect(index*tileWidth, 0, (index+1)*tileWidth, height)
		// The final tile absorbs integer division remainder so the collage
		// always covers the full canvas.
		if index == len(sources)-1 {
			tile.Max.X = width
		}
		// Fill the tile with the same artwork first so poster aspect ratios do
		// not create black bars. The centered contain pass below keeps the full
		// poster visible on top of that fill.
		xdraw.CatmullRom.Scale(destination, tile, decoded, centerCrop(decoded.Bounds(), tile.Dx(), tile.Dy()), xdraw.Over, nil)
		xdraw.CatmullRom.Scale(destination, fitInside(decoded.Bounds(), tile), decoded, decoded.Bounds(), xdraw.Over, nil)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return AssetContent{}, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".collage-*")
	if err != nil {
		return AssetContent{}, err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	_ = temporary.Chmod(0o600)
	if err = jpeg.Encode(temporary, destination, &jpeg.Options{Quality: 88}); err == nil {
		err = temporary.Close()
	} else {
		_ = temporary.Close()
	}
	if err != nil {
		return AssetContent{}, err
	}
	manifest, err := json.Marshal(sourceTags)
	if err != nil {
		return AssetContent{}, err
	}
	if err := atomicWriteFile(filepath.Join(filepath.Dir(target), "library-cover-sources.json"), manifest); err != nil {
		return AssetContent{}, err
	}
	if err := os.Rename(temporaryName, target); err != nil {
		return AssetContent{}, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return AssetContent{}, err
	}
	base := AssetContent{Path: target, MIMEType: "image/jpeg", SizeBytes: info.Size(), ETag: tag, LastModified: info.ModTime()}
	return f.Variant(base, spec)
}

// fitInside preserves the complete source image and centers it in the tile.
func fitInside(source image.Rectangle, tile image.Rectangle) image.Rectangle {
	sourceWidth, sourceHeight := source.Dx(), source.Dy()
	if sourceWidth <= 0 || sourceHeight <= 0 {
		return tile
	}
	width, height := tile.Dx(), tile.Dy()
	if int64(sourceWidth)*int64(height) > int64(sourceHeight)*int64(width) {
		height = sourceHeight * width / sourceWidth
	} else {
		width = sourceWidth * height / sourceHeight
	}
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	left := tile.Min.X + (tile.Dx()-width)/2
	top := tile.Min.Y + (tile.Dy()-height)/2
	return image.Rect(left, top, left+width, top+height)
}

func drawLibraryName(destination *image.RGBA, name string) {
	if strings.TrimSpace(name) == "" {
		return
	}
	face, err := libraryFont(112)
	if err != nil {
		return
	}
	drawer := &font.Drawer{Dst: destination, Src: image.NewUniform(color.White), Face: face}
	textWidth := drawer.MeasureString(name)
	if float64(textWidth)/64 > 0.95*float64(destination.Bounds().Dx()) {
		_ = face.Close()
		face, err = libraryFont(112 * (0.95 * float64(destination.Bounds().Dx()) / (float64(textWidth) / 64)))
		if err != nil {
			return
		}
		drawer.Face = face
		textWidth = drawer.MeasureString(name)
	}
	defer face.Close()
	metrics := face.Metrics()
	drawer.Dot.X = (fixed.I(destination.Bounds().Dx()) - textWidth) / 2
	drawer.Dot.Y = (fixed.I(destination.Bounds().Dy())-metrics.Ascent-metrics.Descent)/2 + metrics.Ascent
	drawer.DrawString(name)
}

func libraryFont(size float64) (font.Face, error) {
	paths := []string{
		"/usr/share/fonts/noto-cjk/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
		"/System/Library/Fonts/AppleSDGothicNeo.ttc",
	}
	var data []byte
	var err error
	for _, path := range paths {
		data, err = os.ReadFile(path)
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	collection, err := opentype.ParseCollection(data)
	if err != nil || collection.NumFonts() == 0 {
		return nil, fmt.Errorf("parse library font: %w", err)
	}
	f, err := collection.Font(0)
	if err != nil {
		return nil, err
	}
	return opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72})
}

func centerCrop(bounds image.Rectangle, targetWidth, targetHeight int) image.Rectangle {
	width, height := bounds.Dx(), bounds.Dy()
	if int64(width)*int64(targetHeight) > int64(height)*int64(targetWidth) {
		cropWidth := height * targetWidth / targetHeight
		if cropWidth < 1 {
			cropWidth = 1
		}
		left := bounds.Min.X + (width-cropWidth)/2
		return image.Rect(left, bounds.Min.Y, left+cropWidth, bounds.Max.Y)
	}
	cropHeight := width * targetHeight / targetWidth
	if cropHeight < 1 {
		cropHeight = 1
	}
	top := bounds.Min.Y + (height-cropHeight)/2
	return image.Rect(bounds.Min.X, top, bounds.Max.X, top+cropHeight)
}

func normalizeImageVariant(spec ImageVariant) ImageVariant {
	for _, value := range []*int{&spec.MaxWidth, &spec.MaxHeight, &spec.Width, &spec.Height} {
		if *value < 0 {
			*value = 0
		}
		if *value > maxDerivedSide {
			*value = maxDerivedSide
		}
	}
	if spec.Quality <= 0 || spec.Quality > 100 {
		spec.Quality = 90
	}
	spec.Format = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(spec.Format), "image/"))
	if spec.Format == "jpg" {
		spec.Format = "jpeg"
	}
	if spec.Format != "jpeg" && spec.Format != "png" {
		spec.Format = ""
	}
	return spec
}

func variantDimensions(sourceWidth, sourceHeight int, spec ImageVariant) (int, int) {
	width, height := spec.Width, spec.Height
	if width > 0 && height > 0 {
		return width, height
	}
	if width > 0 {
		return width, max(1, sourceHeight*width/sourceWidth)
	}
	if height > 0 {
		return max(1, sourceWidth*height/sourceHeight), height
	}
	width, height = sourceWidth, sourceHeight
	if spec.MaxWidth > 0 && width > spec.MaxWidth {
		height = max(1, height*spec.MaxWidth/width)
		width = spec.MaxWidth
	}
	if spec.MaxHeight > 0 && height > spec.MaxHeight {
		width = max(1, width*spec.MaxHeight/height)
		height = spec.MaxHeight
	}
	return width, height
}
