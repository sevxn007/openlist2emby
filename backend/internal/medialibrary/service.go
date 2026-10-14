package medialibrary

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/metadata"
)

type SettingsStore interface {
	Store
	GetSettings(context.Context) (domain.Settings, error)
}

type ClientFactory func(baseURL, adminKey string) OpenList

// MetadataProvider isolates TMDB-compatible metadata lookups from media
// library operations and makes manual refreshes deterministic to test.
type MetadataProvider interface {
	Search(context.Context, string, string, string) ([]domain.MediaSearchResult, error)
	Detail(context.Context, string, string, int64, *int, string) (domain.MediaDetail, error)
}

type MusicMetadataProvider interface {
	Search(context.Context, string, string, int) ([]domain.MusicTrack, error)
	Detail(context.Context, string, string, string) (domain.MusicDetail, error)
}

type MusicSourceProvider interface {
	Sources() []string
}

// CreditsProvider is intentionally optional so media indexing keeps using the
// existing lightweight metadata contract. It is only called on a user action.
type CreditsProvider interface {
	Credits(context.Context, string, domain.MediaCreditsQuery, string) ([]domain.MediaPerson, error)
}

// SeasonEpisodeCreditsProvider optionally resolves every episode's guest cast
// and crew with one season-level metadata request.
type SeasonEpisodeCreditsProvider interface {
	SeasonEpisodeCredits(context.Context, string, domain.MediaCreditsQuery, string) (map[int][]domain.MediaPerson, error)
}

// PersonProfileProvider supplies a person's profile image for Emby image
// requests. It is optional so existing metadata providers remain compatible.
type PersonProfileProvider interface {
	PersonProfile(context.Context, string, int64, string) (string, error)
}

type Service struct {
	store      SettingsStore
	clients    ClientFactory
	metadata   MetadataProvider
	sources    *metadata.Registry
	assets     *AssetFiles
	frames     VideoFrameExtractor
	coverMu    sync.RWMutex
	coverCache map[libraryCoverCacheKey]LibraryCover
}

type libraryCoverCacheKey struct {
	id         int64
	generation int64
}

func NewService(store SettingsStore, clients ClientFactory, metadata MetadataProvider) *Service {
	return &Service{store: store, clients: clients, metadata: metadata, sources: NewMetadataRegistry(metadata), coverCache: make(map[libraryCoverCacheKey]LibraryCover)}
}

func (s *Service) SetAssetFiles(assets *AssetFiles) { s.assets = assets }

func (s *Service) SetVideoFrameExtractor(frames VideoFrameExtractor) { s.frames = frames }

func (s *Service) AssetFiles() *AssetFiles { return s.assets }

func (s *Service) ListItemImagesForUser(ctx context.Context, access UserAccess, itemID int64) ([]LocalImage, error) {
	if _, err := s.GetItemForUser(ctx, access, itemID); err != nil {
		return nil, err
	}
	store, ok := s.store.(LocalizedMetadataStore)
	if !ok {
		return []LocalImage{}, nil
	}
	values, err := store.ListMediaItemImages(ctx, []int64{itemID})
	if err != nil {
		return nil, err
	}
	images := make([]LocalImage, 0, len(values[itemID]))
	for _, imageType := range []ImageType{ImagePrimary, ImageBackdrop} {
		if image := values[itemID][imageType]; image.SHA256 != "" {
			images = append(images, image)
		}
	}
	return images, nil
}

func (s *Service) GetItemImageForUser(ctx context.Context, access UserAccess, itemID int64, imageType ImageType, variant ImageVariant) (AssetContent, error) {
	if _, err := s.GetItemForUser(ctx, access, itemID); err != nil {
		return AssetContent{}, err
	}
	return s.GetPublicItemImage(ctx, itemID, imageType, "", variant)
}

// GetPublicItemImage returns only locally cached image bytes. Emby image URLs
// are long-lived public resources so clients can render them without copying
// an access token into an image URL or request header.
func (s *Service) GetPublicItemImage(ctx context.Context, itemID int64, imageType ImageType, tag string, variant ImageVariant) (AssetContent, error) {
	if s.assets == nil {
		return AssetContent{}, domain.ErrNotFound
	}
	if imageType != ImagePrimary && imageType != ImageBackdrop {
		return AssetContent{}, domain.ErrNotFound
	}
	store, ok := s.store.(LocalizedMetadataStore)
	if !ok {
		return AssetContent{}, domain.ErrNotFound
	}
	// Public image requests only need the item's kind and parent links. Avoid
	// the user-scoped lookup here because it also computes unplayed counts for
	// folders, turning a burst of image requests into many recursive queries.
	item, err := s.store.GetMediaItem(ctx, itemID)
	if err != nil {
		return AssetContent{}, err
	}
	type imageCandidate struct {
		itemID    int64
		imageType ImageType
	}
	candidates := []imageCandidate{{itemID: itemID, imageType: imageType}}
	if imageType == ImageBackdrop {
		candidates = append(candidates, imageCandidate{itemID: itemID, imageType: ImagePrimary})
	}
	if item.Kind == KindEpisode {
		if cover, coverErr := s.episodeSeriesCover(ctx, UserAccess{UserID: 1, IsAdmin: true}, item); coverErr == nil {
			candidates = append(candidates, imageCandidate{itemID: cover.ID, imageType: ImagePrimary})
		}
	}
	var image LocalImage
	found := false
	for _, candidate := range candidates {
		candidateImage, imageErr := store.GetMediaItemImage(ctx, candidate.itemID, candidate.imageType)
		if imageErr != nil || tag != "" && tag != candidateImage.SHA256 {
			continue
		}
		image = candidateImage
		found = true
		break
	}
	if !found {
		return AssetContent{}, domain.ErrNotFound
	}
	original, err := s.assets.Original(image.RelativePath, image.MIMEType, image.SHA256, image.UpdatedAt)
	if err != nil {
		return AssetContent{}, domain.ErrNotFound
	}
	content, err := s.assets.Variant(original, variant)
	if err != nil {
		return AssetContent{}, fmt.Errorf("%w: %v", ErrInvalidLibrary, err)
	}
	return content, nil
}

func (s *Service) episodeSeriesCover(ctx context.Context, access UserAccess, episode Item) (Item, error) {
	if episode.Kind != KindEpisode || episode.ParentID == nil {
		return Item{}, domain.ErrNotFound
	}
	season, err := s.GetItemForUser(ctx, access, *episode.ParentID)
	if err != nil || season.Kind != KindSeason || season.ParentID == nil {
		return Item{}, domain.ErrNotFound
	}
	series, err := s.GetItemForUser(ctx, access, *season.ParentID)
	if err != nil || series.Kind != KindSeries || series.ImageTag == "" {
		return Item{}, domain.ErrNotFound
	}
	return series, nil
}

func (s *Service) LibraryCoverForUser(ctx context.Context, access UserAccess, library Library) (LibraryCover, error) {
	key := libraryCoverCacheKey{id: library.ID, generation: library.ScanGeneration}
	s.coverMu.RLock()
	cached, ok := s.coverCache[key]
	s.coverMu.RUnlock()
	if ok {
		return cached, nil
	}
	kinds := []ItemKind{}
	switch library.ContentType {
	case ContentMovies:
		kinds = []ItemKind{KindMovie}
	case ContentTVShows:
		kinds = []ItemKind{KindSeries}
	case ContentMusic:
		kinds = []ItemKind{KindMusicArtist, KindMusicAlbum, KindAudio}
	}
	cover := LibraryCover{Items: []Item{}}
	const pageSize = 40
	for start := 0; len(cover.Items) < 8; start += pageSize {
		page, err := s.ListItemsForUser(ctx, access, ItemQuery{
			LibraryID: &library.ID, Kinds: kinds, Start: start, Limit: pageSize, SkipTotal: true,
		})
		if err != nil {
			return LibraryCover{}, err
		}
		for _, item := range page.Items {
			if item.ImageURL != "" && item.ImageTag != "" {
				cover.Items = append(cover.Items, item)
				if len(cover.Items) == 8 {
					break
				}
			}
		}
		if start+len(page.Items) >= page.Total || len(page.Items) == 0 {
			break
		}
	}
	if len(cover.Items) == 0 {
		return LibraryCover{}, domain.ErrNotFound
	}
	// Keep the selection stable for a library while distributing the first
	// four slots across the available items instead of always using the query
	// order. A stable order is required because Emby requests the DTO and the
	// image separately, and both requests must produce the same tag.
	seed := strconv.FormatInt(library.ID, 10) + "\x00" + library.Name + "\x00" + strconv.FormatInt(library.ScanGeneration, 10)
	sort.SliceStable(cover.Items, func(i, j int) bool {
		left := sha256.Sum256([]byte(seed + "\x00" + strconv.FormatInt(cover.Items[i].ID, 10)))
		right := sha256.Sum256([]byte(seed + "\x00" + strconv.FormatInt(cover.Items[j].ID, 10)))
		return string(left[:]) < string(right[:])
	})
	if len(cover.Items) > 4 {
		cover.Items = cover.Items[:4]
	}
	cover.PrimaryItemID = cover.Items[0].ID
	hash := sha256.New()
	// Keep the layout version in the tag so clients and derived files do not
	// reuse a cover generated with an older collage arrangement.
	_, _ = io.WriteString(hash, "openlist-library-cover-v6-four-primary-posters-filled\x00"+library.Name+"\x00")
	for _, item := range cover.Items {
		_, _ = io.WriteString(hash, strconv.FormatInt(item.ID, 10)+"\x00"+item.BackdropTag+"\x00"+item.ImageTag+"\x00")
	}
	cover.Tag = hex.EncodeToString(hash.Sum(nil))
	s.coverMu.Lock()
	// Keep the cache bounded when repeated scans create new generations.
	if len(s.coverCache) > 128 {
		for oldKey := range s.coverCache {
			if oldKey.id == library.ID || oldKey.generation < library.ScanGeneration-2 {
				delete(s.coverCache, oldKey)
			}
		}
	}
	s.coverCache[key] = cover
	s.coverMu.Unlock()
	return cover, nil
}

func (s *Service) GetPublicLibraryCover(ctx context.Context, libraryID int64, tag string, variant ImageVariant) (AssetContent, error) {
	library, err := s.GetLibrary(ctx, libraryID)
	if err != nil {
		return AssetContent{}, err
	}
	cover, err := s.LibraryCoverForUser(ctx, UserAccess{UserID: 1, IsAdmin: true}, library)
	if err != nil || tag != "" && tag != cover.Tag || s.assets == nil {
		return AssetContent{}, domain.ErrNotFound
	}
	return s.assets.LibraryCover(cover.Tag, variant, func() ([]AssetContent, error) {
		sources := make([]AssetContent, 0, len(cover.Items))
		for _, item := range cover.Items {
			source, sourceErr := s.GetPublicItemImage(ctx, item.ID, ImagePrimary, item.ImageTag, ImageVariant{})
			if sourceErr != nil {
				return nil, sourceErr
			}
			sources = append(sources, source)
		}
		return sources, nil
	})
}

func (s *Service) UploadItemImageForUser(ctx context.Context, access UserAccess, itemID int64, imageType ImageType, reader io.Reader) (Item, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return Item{}, err
	}
	if s.assets == nil || (imageType != ImagePrimary && imageType != ImageBackdrop) {
		return Item{}, fmt.Errorf("%w: unsupported image type", ErrInvalidLibrary)
	}
	item, err := s.GetItemForUser(ctx, access, itemID)
	if err != nil {
		return Item{}, err
	}
	if err := s.authorizeLibraryEdit(ctx, access, item.LibraryID); err != nil {
		return Item{}, err
	}
	store, ok := s.store.(LocalizedMetadataStore)
	if !ok {
		return Item{}, fmt.Errorf("media image storage is unavailable")
	}
	asset, err := s.assets.StoreItemImage(ctx, itemID, imageType, reader)
	if err != nil {
		return Item{}, fmt.Errorf("%w: %v", ErrInvalidLibrary, err)
	}
	if err := store.UpsertMediaItemImage(ctx, LocalImage{
		ItemID: itemID, ImageType: imageType, SourceType: ImageSourceUpload, RelativePath: asset.RelativePath,
		MIMEType: asset.MIMEType, SizeBytes: asset.SizeBytes, Width: asset.Width,
		Height: asset.Height, SHA256: asset.SHA256, UpdatedAt: asset.UpdatedAt,
	}); err != nil {
		return Item{}, err
	}
	paths, tags, err := store.ListMediaAssetReferences(ctx)
	if err == nil {
		_ = s.assets.GarbageCollect(paths, tags)
	}
	return s.GetItemForUser(ctx, access, itemID)
}

func (s *Service) GetPersonImageForUser(ctx context.Context, access UserAccess, personID int64, variant ImageVariant) (AssetContent, error) {
	if !access.IsAdmin {
		page, err := s.ListItemsForUser(ctx, access, ItemQuery{PersonIDs: []int64{personID}, Limit: 1})
		if err != nil || page.Total == 0 {
			return AssetContent{}, domain.ErrNotFound
		}
	}
	return s.GetPublicPersonImage(ctx, personID, variant)
}

// GetPublicPersonImage returns only an already cached profile image. Public
// Emby image requests must never trigger metadata-provider network calls.
func (s *Service) GetPublicPersonImage(ctx context.Context, personID int64, variant ImageVariant) (AssetContent, error) {
	return s.getPublicPersonImage(ctx, personID, "", variant)
}

// GetPublicPersonImageWithTag is used by Emby clients that pass the cached
// profile hash as PrimaryImageTag. Matching the tag prevents a media item and
// a person with the same numeric ID from returning the wrong image.
func (s *Service) GetPublicPersonImageWithTag(ctx context.Context, personID int64, tag string, variant ImageVariant) (AssetContent, error) {
	return s.getPublicPersonImage(ctx, personID, tag, variant)
}

func (s *Service) getPublicPersonImage(ctx context.Context, personID int64, tag string, variant ImageVariant) (AssetContent, error) {
	if s.assets == nil {
		return AssetContent{}, domain.ErrNotFound
	}
	store, ok := s.store.(LocalizedMetadataStore)
	if !ok {
		return AssetContent{}, domain.ErrNotFound
	}
	person, err := store.GetMediaPerson(ctx, personID)
	if err != nil || person.ProfileSHA256 == "" || (tag != "" && person.ProfileSHA256 != tag) {
		return AssetContent{}, domain.ErrNotFound
	}
	original, err := s.assets.Original(person.ProfileRelativePath, person.ProfileMIMEType, person.ProfileSHA256, person.UpdatedAt)
	if err != nil {
		return AssetContent{}, domain.ErrNotFound
	}
	content, err := s.assets.Variant(original, variant)
	if err != nil {
		return AssetContent{}, fmt.Errorf("%w: %v", ErrInvalidLibrary, err)
	}
	return content, nil
}

func (s *Service) Enabled(ctx context.Context) (bool, error) {
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		return false, err
	}
	return settings.MediaLibraryEnabled, nil
}

func (s *Service) ListLibraries(ctx context.Context) ([]Library, error) {
	return s.ListLibrariesForUser(ctx, UserAccess{UserID: 1, IsAdmin: true})
}

func (s *Service) ListLibrariesForUser(ctx context.Context, access UserAccess) ([]Library, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return nil, err
	}
	libraries, err := s.store.ListLibraries(ctx)
	if err != nil || access.IsAdmin && !access.FilterVisibleLibraries {
		return libraries, err
	}
	ids, err := s.store.ListAccessibleLibraryIDs(ctx, access.UserID)
	if err != nil {
		return nil, err
	}
	allowed := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		allowed[id] = struct{}{}
	}
	filtered := make([]Library, 0, len(ids))
	for _, library := range libraries {
		if _, ok := allowed[library.ID]; ok {
			filtered = append(filtered, library)
		}
	}
	return filtered, nil
}

func (s *Service) GetLibrary(ctx context.Context, id int64) (Library, error) {
	return s.GetLibraryForUser(ctx, UserAccess{UserID: 1, IsAdmin: true}, id)
}

func (s *Service) GetLibraryForUser(ctx context.Context, access UserAccess, id int64) (Library, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return Library{}, err
	}
	if err := s.authorizeLibrary(ctx, access, id); err != nil {
		return Library{}, err
	}
	return s.store.GetLibrary(ctx, id)
}

func (s *Service) CreateLibrary(ctx context.Context, input LibraryInput) (Library, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return Library{}, err
	}
	normalized, err := normalizeLibrary(input, nil)
	if err != nil {
		return Library{}, err
	}
	if err := s.requireVideoFrameExtractor(normalized); err != nil {
		return Library{}, err
	}
	return s.store.CreateLibrary(ctx, normalized)
}

func (s *Service) UpdateLibrary(ctx context.Context, id int64, input LibraryInput) (Library, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return Library{}, err
	}
	existing, err := s.store.GetLibrary(ctx, id)
	if err != nil {
		return Library{}, err
	}
	normalized, err := normalizeLibrary(input, &existing)
	if err != nil {
		return Library{}, err
	}
	if err := s.requireVideoFrameExtractor(normalized); err != nil {
		return Library{}, err
	}
	return s.store.UpdateLibrary(ctx, id, normalized)
}

func (s *Service) ReorderLibraries(ctx context.Context, libraryIDs []int64) error {
	if err := s.requireEnabled(ctx); err != nil {
		return err
	}
	return s.store.ReorderLibraries(ctx, libraryIDs)
}

func (s *Service) DeleteLibrary(ctx context.Context, id int64) error {
	if err := s.requireEnabled(ctx); err != nil {
		return err
	}
	if err := s.store.DeleteLibrary(ctx, id); err != nil {
		return err
	}
	if store, ok := s.store.(LocalizedMetadataStore); ok && s.assets != nil {
		paths, tags, err := store.ListMediaAssetReferences(ctx)
		if err == nil {
			_ = s.assets.GarbageCollect(paths, tags)
		}
	}
	return nil
}

func (s *Service) GetItem(ctx context.Context, id int64) (Item, error) {
	return s.GetItemForUser(ctx, UserAccess{UserID: 1, IsAdmin: true}, id)
}

func (s *Service) ResolveMusicMediaPath(ctx context.Context, id int64) (string, error) {
	item, err := s.GetItem(ctx, id)
	if err != nil {
		return "", err
	}
	if item.Kind != KindAudio || strings.TrimSpace(item.MediaPath) == "" {
		return "", fmt.Errorf("%w: item is not playable audio", ErrInvalidLibrary)
	}
	return item.MediaPath, nil
}

func (s *Service) GetItemForUser(ctx context.Context, access UserAccess, id int64) (Item, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return Item{}, err
	}
	item, err := s.store.GetMediaItemForUser(ctx, access.UserID, id)
	if err != nil {
		return Item{}, err
	}
	if err := s.authorizeLibrary(ctx, access, item.LibraryID); err != nil {
		return Item{}, err
	}
	return s.projectLocalAssets(ctx, item)
}

func (s *Service) UpdateItemMetadataForUser(ctx context.Context, access UserAccess, id int64, input MetadataInput) (Item, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return Item{}, err
	}
	normalized, err := normalizeMetadata(input)
	if err != nil {
		return Item{}, err
	}
	item, err := s.GetItemForUser(ctx, access, id)
	if err != nil {
		return Item{}, err
	}
	if err := s.authorizeLibraryEdit(ctx, access, item.LibraryID); err != nil {
		return Item{}, err
	}
	if normalized.MetadataBindingSet {
		return Item{}, fmt.Errorf("%w: episode movie bindings are no longer supported", ErrInvalidLibrary)
	}
	if normalized.MetadataSource == "" {
		normalized.MetadataSource = item.MetadataSource
	}
	if normalized.ResolvedMetadataSource == "" {
		normalized.ResolvedMetadataSource = item.ResolvedMetadataSource
	}
	normalized.MetadataPriorityKey = item.MetadataPriorityKey
	if err := s.store.UpdateMediaItemMetadata(ctx, id, normalized); err != nil {
		return Item{}, err
	}
	if err := s.localizeStoredItem(ctx, id); err != nil {
		return Item{}, err
	}
	return s.GetItemForUser(ctx, access, id)
}

// SetItemRemoteImageForUser preserves all metadata fields while replacing one image.
func (s *Service) SetItemRemoteImageForUser(ctx context.Context, access UserAccess, id int64, imageType, imageURL string) (Item, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return Item{}, err
	}
	item, err := s.GetItemForUser(ctx, access, id)
	if err != nil {
		return Item{}, err
	}
	if err := s.authorizeLibraryEdit(ctx, access, item.LibraryID); err != nil {
		return Item{}, err
	}
	if imageType != "primary" && imageType != "backdrop" {
		return Item{}, ErrInvalidLibrary
	}
	imageStore, ok := s.store.(interface {
		UpdateMediaItemImageURL(context.Context, int64, ImageType, string) error
	})
	if !ok {
		return Item{}, fmt.Errorf("media image storage is unavailable")
	}
	var download AssetDownload
	if s.assets != nil {
		settings, err := s.store.GetSettings(ctx)
		if err != nil {
			return Item{}, err
		}
		download, err = s.assets.DownloadItemImage(ctx, id, ImageType(imageType), imageURL, hostName(settings.OpenListBaseURL))
		if err != nil {
			return Item{}, fmt.Errorf("save selected media image: %w", err)
		}
	}
	if err := imageStore.UpdateMediaItemImageURL(ctx, id, ImageType(imageType), imageURL); err != nil {
		return Item{}, err
	}
	if s.assets != nil {
		store, ok := s.store.(LocalizedMetadataStore)
		if !ok {
			return Item{}, fmt.Errorf("media image storage is unavailable")
		}
		if err := store.UpsertMediaItemImage(ctx, LocalImage{
			ItemID: id, ImageType: ImageType(imageType), SourceType: ImageSourceRemote, SourceURL: imageURL,
			RelativePath: download.RelativePath, MIMEType: download.MIMEType, SizeBytes: download.SizeBytes,
			Width: download.Width, Height: download.Height, SHA256: download.SHA256, UpdatedAt: download.UpdatedAt,
		}); err != nil {
			return Item{}, err
		}
		paths, tags, err := store.ListMediaAssetReferences(ctx)
		if err == nil {
			_ = s.assets.GarbageCollect(paths, tags)
		}
	}
	return s.GetItemForUser(ctx, access, id)
}

func (s *Service) localizeStoredItem(ctx context.Context, itemID int64) error {
	if s.assets == nil {
		return nil
	}
	item, err := s.store.GetMediaItem(ctx, itemID)
	if err != nil {
		return err
	}
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		return err
	}
	handler := &TaskHandler{store: s.store, metadata: s.metadata, sources: s.sources, assets: s.assets}
	input := ItemInput{
		LibraryID: item.LibraryID, ParentID: item.ParentID, Kind: item.Kind, Path: item.Path, MediaPath: item.MediaPath,
		Name: item.Name, SortName: item.SortName, Container: item.Container, SizeBytes: item.SizeBytes, ModifiedAt: item.ModifiedAt,
		TVDBID: item.TVDBID, MetadataSource: item.MetadataSource, ResolvedMetadataSource: item.ResolvedMetadataSource, TMDBID: item.TMDBID, IMDbID: item.IMDbID, Genres: item.Genres, SeasonNumber: item.SeasonNumber, EpisodeNumber: item.EpisodeNumber,
		ProductionYear: item.ProductionYear, PremiereDate: item.PremiereDate, Overview: item.Overview,
		CommunityRating: item.CommunityRating, RuntimeTicks: item.RuntimeTicks, ImageURL: item.ImageURL, BackdropURL: item.BackdropURL,
		Album: item.Album, AlbumArtist: item.AlbumArtist, Artists: item.Artists, TrackNumber: item.TrackNumber, DiscNumber: item.DiscNumber,
		Lyrics: item.Lyrics, TranslatedLyrics: item.TranslatedLyrics,
	}
	if !handler.localizeScannedItemWithPolicy(ctx, item, input, settings.TMDBAPIKey, settings.OpenListBaseURL, newIndexTracker(&ScanResult{}), true, true) {
		return fmt.Errorf("localize media metadata: one or more resources could not be saved")
	}
	if store, ok := s.store.(LocalizedMetadataStore); ok {
		paths, tags, err := store.ListMediaAssetReferences(ctx)
		if err == nil {
			_ = s.assets.GarbageCollect(paths, tags)
		}
	}
	return nil
}

func (s *Service) listDescendantsForUser(ctx context.Context, access UserAccess, ancestorID int64) ([]Item, error) {
	const pageSize = 500
	items := make([]Item, 0)
	for start := 0; ; start += pageSize {
		page, err := s.ListItemsForUser(ctx, access, ItemQuery{
			AncestorID: &ancestorID, Recursive: true, Start: start, Limit: pageSize,
		})
		if err != nil {
			return nil, err
		}
		items = append(items, page.Items...)
		if len(page.Items) == 0 || len(items) >= page.Total {
			return items, nil
		}
	}
}

func metadataInputFromItem(item Item) MetadataInput {
	// Unprojected store results retain remote URLs directly; projected results
	// carry a source separately from the authenticated local image route.
	if item.ImageSourceURL == "" && (strings.HasPrefix(item.ImageURL, "https://") || strings.HasPrefix(item.ImageURL, "http://")) {
		item.ImageSourceURL = item.ImageURL
	}
	if item.BackdropSourceURL == "" && (strings.HasPrefix(item.BackdropURL, "https://") || strings.HasPrefix(item.BackdropURL, "http://")) {
		item.BackdropSourceURL = item.BackdropURL
	}
	return MetadataInput{
		Name: item.Name, SortName: item.SortName, TVDBID: item.TVDBID, MetadataSource: item.MetadataSource, ResolvedMetadataSource: item.ResolvedMetadataSource, TMDBID: item.TMDBID, IMDbID: item.IMDbID,
		Genres: item.Genres, SeasonNumber: item.SeasonNumber, EpisodeNumber: item.EpisodeNumber,
		ProductionYear: item.ProductionYear, PremiereDate: item.PremiereDate, Overview: item.Overview,
		CommunityRating: item.CommunityRating, RuntimeTicks: item.RuntimeTicks, ImageURL: item.ImageSourceURL,
		BackdropURL: item.BackdropSourceURL, ImageSourceURL: item.ImageSourceURL, BackdropSourceURL: item.BackdropSourceURL, Album: item.Album, AlbumArtist: item.AlbumArtist,
		Artists: item.Artists, TrackNumber: item.TrackNumber, DiscNumber: item.DiscNumber, Lyrics: item.Lyrics, TranslatedLyrics: item.TranslatedLyrics,
	}
}

func applyTMDBDetail(input *MetadataInput, detail domain.MediaDetail, tmdbID int64) {
	setMetadataIdentity(input, detail.Source, tmdbID)
	input.IMDbID = strings.TrimSpace(detail.IMDbID)
	input.Genres = detail.Genres
	if title := strings.TrimSpace(detail.Title); title != "" {
		input.Name, input.SortName = title, title
	} else if name := strings.TrimSpace(detail.Name); name != "" {
		input.Name, input.SortName = name, name
	}
	input.Overview = detail.Overview
	input.CommunityRating = float64(detail.VoteAverage)
	input.ImageURL = detail.PosterURL
	input.BackdropURL = detail.BackdropURL
	// Fetched artwork replaces the previous localized image source as well.
	input.ImageSourceURL = ""
	input.BackdropSourceURL = ""
	input.RuntimeTicks = int64(detail.Runtime) * 60 * 10_000_000
	input.PremiereDate = firstMetadataDate(detail.ReleaseDate, detail.FirstAirDate)
	input.ProductionYear = metadataProductionYear(input.PremiereDate)
}

func applyTMDBSeasonDetail(input *MetadataInput, detail domain.MediaDetail) {
	if detail.ID > 0 {
		setMetadataIdentity(input, detail.Source, detail.ID)
	}
	if name := strings.TrimSpace(detail.Name); name != "" {
		input.Name = name
	}
	input.Overview = detail.Overview
	input.CommunityRating = float64(detail.VoteAverage)
	input.ImageURL = detail.PosterURL
	input.BackdropURL = detail.BackdropURL
	// Fetched artwork replaces the previous localized image source as well.
	input.ImageSourceURL = ""
	input.BackdropSourceURL = ""
	input.PremiereDate = firstMetadataDate(detail.ReleaseDate, detail.FirstAirDate)
	input.ProductionYear = metadataProductionYear(input.PremiereDate)
}

func applyTMDBEpisodeDetail(input *MetadataInput, detail domain.MediaEpisode) {
	if detail.ID > 0 {
		setMetadataIdentity(input, detail.Source, detail.ID)
	}
	if name := strings.TrimSpace(detail.Name); name != "" {
		input.Name = name
	}
	input.Overview = detail.Overview
	input.CommunityRating = float64(detail.VoteAverage)
	input.RuntimeTicks = int64(detail.Runtime) * 60 * 10_000_000
	if still := strings.TrimSpace(detail.StillURL); still != "" {
		// Clear the previous source marker so normalizeMetadata keeps the new
		// still instead of restoring an older fallback image.
		input.ImageURL = still
		input.ImageSourceURL = ""
	}
	input.PremiereDate = strings.TrimSpace(detail.AirDate)
	input.ProductionYear = metadataProductionYear(input.PremiereDate)
}

func findEpisodeDetail(episodes []domain.MediaEpisode, number int) (domain.MediaEpisode, bool) {
	for _, episode := range episodes {
		if episode.EpisodeNumber == number {
			return episode, true
		}
	}
	return domain.MediaEpisode{}, false
}

func firstMetadataDate(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func metadataProductionYear(value string) *int {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil
	}
	year := parsed.Year()
	return &year
}

func (s *Service) ListItems(ctx context.Context, query ItemQuery) (ItemPage, error) {
	return s.ListItemsForUser(ctx, UserAccess{UserID: 1, IsAdmin: true}, query)
}

func (s *Service) ListItemsForUser(ctx context.Context, access UserAccess, query ItemQuery) (ItemPage, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return ItemPage{}, err
	}
	if query.LibraryID != nil {
		if err := s.authorizeLibrary(ctx, access, *query.LibraryID); err != nil {
			return ItemPage{}, err
		}
	}
	if query.ParentID != nil {
		parent, err := s.GetItemForUser(ctx, access, *query.ParentID)
		if err != nil {
			return ItemPage{}, err
		}
		if query.LibraryID != nil && parent.LibraryID != *query.LibraryID {
			return ItemPage{}, domain.ErrNotFound
		}
		if query.LibraryID == nil {
			libraryID := parent.LibraryID
			query.LibraryID = &libraryID
		}
	}
	if query.AncestorID != nil {
		if _, err := s.GetItemForUser(ctx, access, *query.AncestorID); err != nil {
			return ItemPage{}, err
		}
	}

	if len(query.MusicArtistIDs) > 0 {
		if err := validateMusicArtistIDs(ctx, s, access, query.MusicArtistIDs); err != nil {
			return ItemPage{}, err
		}
	}
	if !access.IsAdmin || access.FilterVisibleLibraries && !itemQueryHasDirectScope(query) {
		ids, err := s.store.ListAccessibleLibraryIDs(ctx, access.UserID)
		if err != nil {
			return ItemPage{}, err
		}
		query.AllowedLibraryIDs = ids
	}
	page, err := s.store.ListMediaItemsForUser(ctx, access.UserID, query)
	if err != nil {
		return ItemPage{}, err
	}
	if err := s.projectLocalAssetPage(ctx, page.Items); err != nil {
		return ItemPage{}, err
	}
	return page, nil
}

func itemQueryHasDirectScope(query ItemQuery) bool {
	return query.LibraryID != nil || query.ParentID != nil || query.AncestorID != nil || len(query.MusicArtistIDs) > 0
}

func validateMusicArtistIDs(ctx context.Context, service *Service, access UserAccess, ids []int64) error {
	for _, id := range ids {
		item, err := service.GetItemForUser(ctx, access, id)
		if err != nil {
			return err
		}
		if item.Kind != KindMusicArtist {
			return domain.ErrNotFound
		}
	}
	return nil
}

func (s *Service) ListItemPeopleForUser(ctx context.Context, access UserAccess, itemID int64, language string) ([]domain.MediaPerson, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return nil, err
	}
	item, err := s.GetItemForUser(ctx, access, itemID)
	if err != nil {
		return nil, err
	}
	if s.assets == nil {
		return s.listRemoteItemPeople(ctx, access, item, language)
	}
	store, ok := s.store.(LocalizedMetadataStore)
	if !ok {
		return []domain.MediaPerson{}, nil
	}
	var seasonPeople []domain.MediaPerson
	if item.Kind == KindEpisode && item.ParentID != nil {
		seasonPeople, err = store.ListMediaItemPeople(ctx, *item.ParentID)
		if err != nil {
			return nil, err
		}
	}
	people, err := store.ListMediaItemPeople(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	if item.Kind == KindEpisode {
		people = mergeMediaPeople(seasonPeople, people)
	}
	people, err = s.filterPeopleForLibrary(ctx, item.LibraryID, people, true)
	if err != nil {
		return nil, err
	}
	s.projectPeopleURLs(people)
	if item.Kind == KindEpisode {
		return people, nil
	}
	if people == nil {
		return []domain.MediaPerson{}, nil
	}
	return people, nil
}

// ListEpisodePeopleForUser returns only the credits attached to an episode.
// Callers that already loaded season people can use this to append guest stars
// without fetching the season credits once per episode.
func (s *Service) ListEpisodePeopleForUser(ctx context.Context, access UserAccess, itemID int64, language string) ([]domain.MediaPerson, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return nil, err
	}
	item, err := s.GetItemForUser(ctx, access, itemID)
	if err != nil {
		return nil, err
	}
	if item.Kind != KindEpisode {
		return []domain.MediaPerson{}, nil
	}
	if s.assets == nil {
		provider, apiKey, available, providerErr := s.creditsProvider(ctx)
		if providerErr != nil || !available {
			return []domain.MediaPerson{}, providerErr
		}
		query, found, queryErr := s.creditsQueryForItem(ctx, access, item)
		if queryErr != nil || !found {
			return []domain.MediaPerson{}, queryErr
		}
		people, peopleErr := provider.Credits(ctx, apiKey, query, language)
		if peopleErr != nil {
			return nil, peopleErr
		}
		return s.filterPeopleForLibrary(ctx, item.LibraryID, people, false)
	}
	store, ok := s.store.(LocalizedMetadataStore)
	if !ok {
		return []domain.MediaPerson{}, nil
	}
	people, err := store.ListMediaItemPeople(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	people, err = s.filterPeopleForLibrary(ctx, item.LibraryID, people, true)
	if err != nil {
		return nil, err
	}
	if people == nil {
		return []domain.MediaPerson{}, nil
	}
	s.projectPeopleURLs(people)
	return people, nil
}

func (s *Service) GetPersonProfileForUser(ctx context.Context, access UserAccess, personID int64, language string) (string, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return "", err
	}
	if s.assets == nil {
		provider, ok := s.metadata.(PersonProfileProvider)
		if !ok || provider == nil || personID <= 0 {
			return "", nil
		}
		settings, err := s.store.GetSettings(ctx)
		if err != nil || strings.TrimSpace(settings.TMDBAPIKey) == "" {
			return "", err
		}
		return provider.PersonProfile(ctx, settings.TMDBAPIKey, personID, language)
	}
	store, ok := s.store.(LocalizedMetadataStore)
	if !ok || personID <= 0 {
		return "", nil
	}
	person, err := store.GetMediaPerson(ctx, personID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return "", nil
		}
		return "", err
	}
	if person.ProfileSHA256 == "" {
		return "", nil
	}
	if !access.IsAdmin {
		page, listErr := s.ListItemsForUser(ctx, access, ItemQuery{PersonIDs: []int64{personID}, Limit: 1})
		if listErr != nil || page.Total == 0 {
			return "", domain.ErrNotFound
		}
	}
	return fmt.Sprintf("/api/manager/media-library/people/%d/images/primary?tag=%s", personID, url.QueryEscape(person.ProfileSHA256)), nil
}

func (s *Service) projectLocalAssets(ctx context.Context, item Item) (Item, error) {
	items := []Item{item}
	if err := s.projectLocalAssetPage(ctx, items); err != nil {
		return Item{}, err
	}
	return items[0], nil
}

func (s *Service) projectLocalAssetPage(ctx context.Context, items []Item) error {
	if s.assets == nil {
		return nil
	}
	store, ok := s.store.(LocalizedMetadataStore)
	if !ok || len(items) == 0 {
		return nil
	}
	ids := make([]int64, len(items))
	for i := range items {
		ids[i] = items[i].ID
	}
	images, err := store.ListMediaItemImages(ctx, ids)
	if err != nil {
		return err
	}
	for i := range items {
		items[i].ImageSourceURL = items[i].ImageURL
		items[i].BackdropSourceURL = items[i].BackdropURL
		items[i].ImageURL, items[i].BackdropURL = "", ""
		if image := images[items[i].ID][ImagePrimary]; image.SHA256 != "" {
			items[i].ImageTag = image.SHA256
			items[i].ImageSourceURL = image.SourceURL
			items[i].ImageURL = fmt.Sprintf("/api/manager/media-library/items/%d/images/primary?tag=%s", items[i].ID, url.QueryEscape(image.SHA256))
		}
		if image := images[items[i].ID][ImageBackdrop]; image.SHA256 != "" {
			items[i].BackdropTag = image.SHA256
			items[i].BackdropSourceURL = image.SourceURL
			items[i].BackdropURL = fmt.Sprintf("/api/manager/media-library/items/%d/images/backdrop?tag=%s", items[i].ID, url.QueryEscape(image.SHA256))
		}
	}
	return nil
}

func (s *Service) listRemoteItemPeople(ctx context.Context, access UserAccess, item Item, language string) ([]domain.MediaPerson, error) {
	provider, apiKey, available, err := s.creditsProvider(ctx)
	if err != nil || !available {
		return []domain.MediaPerson{}, err
	}
	query, found, err := s.creditsQueryForItem(ctx, access, item)
	if err != nil || !found {
		return []domain.MediaPerson{}, err
	}
	var seasonPeople []domain.MediaPerson
	if item.Kind == KindEpisode && item.ParentID != nil {
		season, seasonErr := s.GetItemForUser(ctx, access, *item.ParentID)
		if seasonErr != nil {
			return nil, seasonErr
		}
		seasonQuery, seasonFound, seasonErr := s.creditsQueryForItem(ctx, access, season)
		if seasonErr != nil {
			return nil, seasonErr
		}
		if seasonFound {
			seasonPeople, seasonErr = provider.Credits(ctx, apiKey, seasonQuery, language)
			if seasonErr != nil {
				return nil, seasonErr
			}
		}
	}
	people, err := provider.Credits(ctx, apiKey, query, language)
	if err != nil {
		return nil, err
	}
	if item.Kind == KindEpisode {
		people = mergeMediaPeople(seasonPeople, people)
	}
	return s.filterPeopleForLibrary(ctx, item.LibraryID, people, false)
}

func (s *Service) filterPeopleForLibrary(ctx context.Context, libraryID int64, people []domain.MediaPerson, localCache bool) ([]domain.MediaPerson, error) {
	if len(people) == 0 {
		return people, nil
	}
	library, err := s.store.GetLibrary(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	return filterPeopleByImageAvailability(library.HidePeopleWithoutImages, people, localCache), nil
}

func filterPeopleByImageAvailability(hideWithoutImages bool, people []domain.MediaPerson, localCache bool) []domain.MediaPerson {
	if !hideWithoutImages || len(people) == 0 {
		return people
	}
	filtered := make([]domain.MediaPerson, 0, len(people))
	for _, person := range people {
		hasImage := person.ProfileTag != ""
		if !localCache {
			hasImage = strings.TrimSpace(person.ProfileURL) != ""
		}
		if hasImage {
			filtered = append(filtered, person)
		}
	}
	return filtered
}

func (s *Service) projectPeopleURLs(people []domain.MediaPerson) {
	for i := range people {
		tag := people[i].ProfileTag
		people[i].ProfileURL = ""
		if tag != "" {
			people[i].ProfileURL = fmt.Sprintf("/api/manager/media-library/people/%d/images/primary?tag=%s", people[i].ID, url.QueryEscape(tag))
		}
	}
}

func (s *Service) creditsProvider(ctx context.Context) (CreditsProvider, string, bool, error) {
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		return nil, "", false, err
	}
	return sourceCredits{registry: s.sourceRegistry(), settings: settings}, "", true, nil
}

func (s *Service) creditsQueryForItem(ctx context.Context, access UserAccess, item Item) (domain.MediaCreditsQuery, bool, error) {
	if (item.Kind == KindMovie || item.Kind == KindSeries) && resolvedID(item) > 0 {
		kind := "movie"
		if item.Kind == KindSeries {
			kind = "tv"
		}
		return domain.MediaCreditsQuery{Source: resolvedSource(item), ExternalID: resolvedID(item), TMDBID: resolvedID(item), MediaType: kind}, true, nil
	}
	if item.ParentID == nil || (item.Kind != KindSeason && item.Kind != KindEpisode) {
		return domain.MediaCreditsQuery{}, false, nil
	}
	season := item
	if item.Kind == KindEpisode {
		var err error
		season, err = s.GetItemForUser(ctx, access, *item.ParentID)
		if err != nil {
			return domain.MediaCreditsQuery{}, false, err
		}
	}
	if season.Kind != KindSeason || season.ParentID == nil || season.SeasonNumber == nil {
		return domain.MediaCreditsQuery{}, false, nil
	}
	series, err := s.GetItemForUser(ctx, access, *season.ParentID)
	if err != nil {
		return domain.MediaCreditsQuery{}, false, err
	}
	if series.Kind != KindSeries || resolvedID(series) == 0 {
		return domain.MediaCreditsQuery{}, false, nil
	}
	query := domain.MediaCreditsQuery{Source: resolvedSource(series), ExternalID: resolvedID(series), MediaType: "tv", TMDBID: resolvedID(series), SeasonNumber: season.SeasonNumber}
	if item.Kind == KindEpisode {
		if item.EpisodeNumber == nil {
			return domain.MediaCreditsQuery{}, false, nil
		}
		query.EpisodeNumber = item.EpisodeNumber
	}
	return query, true, nil
}

func mergeMediaPeople(groups ...[]domain.MediaPerson) []domain.MediaPerson {
	byID := make(map[int64]domain.MediaPerson)
	for _, group := range groups {
		for _, person := range group {
			if person.ID <= 0 {
				continue
			}
			existing, found := byID[person.ID]
			if !found {
				byID[person.ID] = person
				continue
			}
			if existing.Name == "" {
				existing.Name = person.Name
			}
			if existing.ProfileURL == "" {
				existing.ProfileURL = person.ProfileURL
			}
			if existing.ProfileTag == "" {
				existing.ProfileTag = person.ProfileTag
			}
			if existing.Type == "" {
				existing.Type = person.Type
			}
			if person.Role != "" && !strings.Contains(existing.Role, person.Role) {
				if existing.Role == "" {
					existing.Role = person.Role
				} else {
					existing.Role += " · " + person.Role
				}
			}
			if existing.DisplayRole == "" {
				existing.DisplayRole = person.DisplayRole
			}
			if person.SortOrder < existing.SortOrder {
				existing.SortOrder = person.SortOrder
			}
			byID[person.ID] = existing
		}
	}
	result := make([]domain.MediaPerson, 0, len(byID))
	for _, person := range byID {
		result = append(result, person)
	}
	sort.SliceStable(result, func(left, right int) bool {
		if result[left].SortOrder == result[right].SortOrder {
			return result[left].Name < result[right].Name
		}
		return result[left].SortOrder < result[right].SortOrder
	})
	return result
}

// MergeMediaPeople combines credits from a parent season and an episode.
func MergeMediaPeople(groups ...[]domain.MediaPerson) []domain.MediaPerson {
	return mergeMediaPeople(groups...)
}

func (s *Service) UpdateUserData(ctx context.Context, itemID int64, data UserData) (UserData, error) {
	return s.UpdateUserDataForUser(ctx, UserAccess{UserID: 1, IsAdmin: true}, itemID, data)
}

func (s *Service) UpdateUserDataForUser(ctx context.Context, access UserAccess, itemID int64, data UserData) (UserData, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return UserData{}, err
	}
	if data.PlaybackPositionTicks < 0 || data.PlayCount < 0 {
		return UserData{}, fmt.Errorf("%w: invalid playback state", ErrInvalidLibrary)
	}
	item, err := s.GetItemForUser(ctx, access, itemID)
	if err != nil {
		return UserData{}, err
	}
	if item.Kind == KindAudio {
		data.PlaybackPositionTicks = 0
	}
	return s.store.UpdateMediaUserDataForUser(ctx, access.UserID, item.ID, data)
}

func (s *Service) UpdatePlayback(ctx context.Context, itemID int64, update PlaybackUpdate) (UserData, error) {
	return s.UpdatePlaybackForUser(ctx, UserAccess{UserID: 1, IsAdmin: true}, itemID, update)
}

func (s *Service) UpdatePlaybackForUser(ctx context.Context, access UserAccess, itemID int64, update PlaybackUpdate) (UserData, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return UserData{}, err
	}
	if update.PositionTicks < 0 {
		return UserData{}, fmt.Errorf("%w: playback position must not be negative", ErrInvalidLibrary)
	}
	item, err := s.GetItemForUser(ctx, access, itemID)
	if err != nil {
		return UserData{}, err
	}
	if item.Kind.Folder() || strings.TrimSpace(item.MediaPath) == "" {
		return UserData{}, fmt.Errorf("%w: item is not playable", ErrInvalidLibrary)
	}

	data := item.UserData
	if item.Kind == KindAudio {
		data.PlaybackPositionTicks = 0
	} else {
		data.PlaybackPositionTicks = update.PositionTicks
	}
	now := time.Now().UTC()
	data.LastPlayedAt = &now
	if update.Stopped && item.RuntimeTicks > 0 {
		library, err := s.store.GetLibrary(ctx, item.LibraryID)
		if err != nil {
			return UserData{}, err
		}
		if float64(update.PositionTicks)/float64(item.RuntimeTicks) >= float64(library.PlaybackCompletionPercent)/100 {
			return s.SetPlayedForUser(ctx, access, itemID, true)
		}
	}
	return s.store.UpdateMediaUserDataForUser(ctx, access.UserID, itemID, data)
}

func (s *Service) ResolveDownloadURL(ctx context.Context, itemID int64, userAgent string) (string, error) {
	return s.ResolveDownloadURLForUser(ctx, UserAccess{UserID: 1, IsAdmin: true}, itemID, userAgent)
}

func (s *Service) ResolveDownloadURLForUser(ctx context.Context, access UserAccess, itemID int64, userAgent string) (string, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return "", err
	}
	item, err := s.GetItemForUser(ctx, access, itemID)
	if err != nil {
		return "", err
	}
	if item.Kind.Folder() || strings.TrimSpace(item.MediaPath) == "" {
		return "", fmt.Errorf("%w: item is not playable", ErrInvalidLibrary)
	}
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		return "", err
	}
	return s.clients(settings.OpenListBaseURL, settings.OpenListAdminKey).ResolveDownloadURL(ctx, item.MediaPath, userAgent)
}

func (s *Service) authorizeLibrary(ctx context.Context, access UserAccess, libraryID int64) error {
	if access.IsAdmin {
		return nil
	}
	if access.UserID <= 0 {
		return domain.ErrNotFound
	}
	allowed, err := s.store.UserCanAccessLibrary(ctx, access.UserID, libraryID)
	if err != nil {
		return err
	}
	if !allowed {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Service) authorizeLibraryEdit(ctx context.Context, access UserAccess, libraryID int64) error {
	if access.IsAdmin {
		return nil
	}
	return ErrLibraryEditForbidden
}

func (s *Service) requireEnabled(ctx context.Context) error {
	enabled, err := s.Enabled(ctx)
	if err != nil {
		return err
	}
	if !enabled {
		return ErrDisabled
	}
	return nil
}

func normalizeLibrary(input LibraryInput, existing *Library) (LibraryInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 100 {
		return LibraryInput{}, fmt.Errorf("%w: name is required and must not exceed 100 characters", ErrInvalidLibrary)
	}
	if !input.ContentType.Valid() {
		return LibraryInput{}, fmt.Errorf("%w: unsupported content type", ErrInvalidLibrary)
	}
	legacyRootPath := strings.TrimSpace(input.RootPath)
	if legacyRootPath != "" {
		if !strings.HasPrefix(legacyRootPath, "/") {
			return LibraryInput{}, fmt.Errorf("%w: root path must be absolute", ErrInvalidLibrary)
		}
		legacyRootPath = path.Clean(legacyRootPath)
	}
	rootPaths := input.RootPaths
	if len(rootPaths) == 0 && legacyRootPath != "" {
		rootPaths = []string{legacyRootPath}
	}
	if len(rootPaths) == 0 {
		return LibraryInput{}, fmt.Errorf("%w: at least one root path is required", ErrInvalidLibrary)
	}
	normalizedRoots := make([]string, 0, len(rootPaths))
	seen := make(map[string]struct{}, len(rootPaths))
	for _, rootPath := range rootPaths {
		rootPath = strings.TrimSpace(rootPath)
		if rootPath == "" || !strings.HasPrefix(rootPath, "/") {
			return LibraryInput{}, fmt.Errorf("%w: root path must be absolute", ErrInvalidLibrary)
		}
		rootPath = path.Clean(rootPath)
		if _, exists := seen[rootPath]; exists {
			return LibraryInput{}, fmt.Errorf("%w: root paths must be unique", ErrInvalidLibrary)
		}
		for _, existing := range normalizedRoots {
			if rootPathsOverlap(existing, rootPath) {
				return LibraryInput{}, fmt.Errorf("%w: root paths must not overlap", ErrInvalidLibrary)
			}
		}
		seen[rootPath] = struct{}{}
		normalizedRoots = append(normalizedRoots, rootPath)
	}
	if legacyRootPath != "" && legacyRootPath != normalizedRoots[0] {
		return LibraryInput{}, fmt.Errorf("%w: root_path must match the first root_paths entry", ErrInvalidLibrary)
	}
	input.RootPaths = normalizedRoots
	input.RootPath = normalizedRoots[0]
	enabled := false
	percent := 10
	if existing != nil {
		enabled = existing.VideoScreenshotEnabled
		percent = existing.VideoScreenshotPercent
	}
	if input.VideoScreenshotEnabled != nil {
		enabled = *input.VideoScreenshotEnabled
	}
	if input.VideoScreenshotPercent != nil {
		percent = *input.VideoScreenshotPercent
	}
	if percent < 1 || percent > 99 {
		return LibraryInput{}, fmt.Errorf("%w: video screenshot percent must be between 1 and 99", ErrInvalidLibrary)
	}
	if input.ContentType == ContentMusic {
		enabled = false
	}
	input.VideoScreenshotEnabled = &enabled
	input.VideoScreenshotPercent = &percent
	completionPercent := DefaultPlaybackCompletionPercent
	if existing != nil && existing.PlaybackCompletionPercent != 0 {
		completionPercent = existing.PlaybackCompletionPercent
	}
	if input.PlaybackCompletionPercent != nil {
		completionPercent = *input.PlaybackCompletionPercent
	}
	if completionPercent < 1 || completionPercent > 100 {
		return LibraryInput{}, fmt.Errorf("%w: playback completion percent must be between 1 and 100", ErrInvalidLibrary)
	}
	input.PlaybackCompletionPercent = &completionPercent
	return input, nil
}

func (s *Service) requireVideoFrameExtractor(input LibraryInput) error {
	if input.VideoScreenshotEnabled == nil || !*input.VideoScreenshotEnabled {
		return nil
	}
	if s.frames == nil {
		return ErrVideoScreenshotUnavailable
	}
	return s.frames.Available()
}

func rootPathsOverlap(left, right string) bool {
	return left == "/" || right == "/" || left == right ||
		strings.HasPrefix(left, right+"/") || strings.HasPrefix(right, left+"/")
}

func normalizeMetadata(input MetadataInput) (MetadataInput, error) {
	if input.MetadataSource != "" && input.MetadataSource != "auto" && input.MetadataSource != "tmdb" && input.MetadataSource != "tvdb" {
		return MetadataInput{}, fmt.Errorf("%w: invalid metadata source", ErrInvalidLibrary)
	}
	if input.TVDBID != nil && *input.TVDBID <= 0 {
		return MetadataInput{}, fmt.Errorf("%w: TVDB ID must be positive", ErrInvalidLibrary)
	}

	if strings.TrimSpace(input.ImageSourceURL) != "" || input.ImageURL == "" {
		input.ImageURL = input.ImageSourceURL
	}
	if strings.TrimSpace(input.BackdropSourceURL) != "" || input.BackdropURL == "" {
		input.BackdropURL = input.BackdropSourceURL
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 300 {
		return MetadataInput{}, fmt.Errorf("%w: name is required and must not exceed 300 characters", ErrInvalidLibrary)
	}
	input.SortName = strings.TrimSpace(input.SortName)
	if len(input.SortName) > 300 {
		return MetadataInput{}, fmt.Errorf("%w: sort name must not exceed 300 characters", ErrInvalidLibrary)
	}
	if input.SortName == "" {
		input.SortName = input.Name
	}
	input.IMDbID = strings.TrimSpace(input.IMDbID)
	if len(input.IMDbID) > 64 {
		return MetadataInput{}, fmt.Errorf("%w: IMDb ID must not exceed 64 characters", ErrInvalidLibrary)
	}
	input.PremiereDate = strings.TrimSpace(input.PremiereDate)
	if input.PremiereDate != "" {
		if _, err := time.Parse("2006-01-02", input.PremiereDate); err != nil {
			return MetadataInput{}, fmt.Errorf("%w: premiere date must use YYYY-MM-DD", ErrInvalidLibrary)
		}
	}
	input.Overview = strings.TrimSpace(input.Overview)
	if len(input.Overview) > 20_000 {
		return MetadataInput{}, fmt.Errorf("%w: overview must not exceed 20000 characters", ErrInvalidLibrary)
	}
	input.Album = strings.TrimSpace(input.Album)
	input.AlbumArtist = strings.TrimSpace(input.AlbumArtist)
	if len(input.Album) > 300 || len(input.AlbumArtist) > 300 {
		return MetadataInput{}, fmt.Errorf("%w: album metadata must not exceed 300 characters", ErrInvalidLibrary)
	}
	input.Lyrics = strings.TrimSpace(input.Lyrics)
	input.TranslatedLyrics = strings.TrimSpace(input.TranslatedLyrics)
	if len(input.Lyrics) > 500_000 || len(input.TranslatedLyrics) > 500_000 {
		return MetadataInput{}, fmt.Errorf("%w: lyrics must not exceed 500000 characters", ErrInvalidLibrary)
	}
	var err error
	if input.Genres, err = normalizeMetadataValues(input.Genres, "genres"); err != nil {
		return MetadataInput{}, err
	}
	if input.Artists, err = normalizeMetadataValues(input.Artists, "artists"); err != nil {
		return MetadataInput{}, err
	}
	if input.TMDBID != nil && *input.TMDBID <= 0 {
		return MetadataInput{}, fmt.Errorf("%w: TMDB ID must be positive", ErrInvalidLibrary)
	}
	for _, value := range []*int{input.SeasonNumber, input.EpisodeNumber, input.TrackNumber, input.DiscNumber} {
		if value != nil && *value < 0 {
			return MetadataInput{}, fmt.Errorf("%w: sequence numbers must not be negative", ErrInvalidLibrary)
		}
	}
	if input.ProductionYear != nil && (*input.ProductionYear < 1 || *input.ProductionYear > 9999) {
		return MetadataInput{}, fmt.Errorf("%w: production year must be between 1 and 9999", ErrInvalidLibrary)
	}
	if input.CommunityRating < 0 || input.CommunityRating > 10 {
		return MetadataInput{}, fmt.Errorf("%w: community rating must be between 0 and 10", ErrInvalidLibrary)
	}
	input.CommunityRating = math.Round(input.CommunityRating*10) / 10
	if input.RuntimeTicks < 0 {
		return MetadataInput{}, fmt.Errorf("%w: runtime must not be negative", ErrInvalidLibrary)
	}
	if input.ImageURL, err = normalizeMetadataURL(input.ImageURL); err != nil {
		return MetadataInput{}, err
	}
	if input.BackdropURL, err = normalizeMetadataURL(input.BackdropURL); err != nil {
		return MetadataInput{}, err
	}
	return input, nil
}

func normalizeMetadataValues(values []string, field string) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if len(value) > 100 {
			return nil, fmt.Errorf("%w: %s entries must not exceed 100 characters", ErrInvalidLibrary, field)
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
		if len(result) > 20 {
			return nil, fmt.Errorf("%w: at most 20 %s entries are allowed", ErrInvalidLibrary, field)
		}
	}
	return result, nil
}

func normalizeMetadataURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("%w: image URLs must use HTTP or HTTPS", ErrInvalidLibrary)
	}
	return value, nil
}

func IsDisabled(err error) bool {
	return errors.Is(err, ErrDisabled)
}

func (s *Service) sourceRegistry() *metadata.Registry {
	if s.sources != nil {
		return s.sources
	}
	return NewMetadataRegistry(s.metadata)
}
