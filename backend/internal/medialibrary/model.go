package medialibrary

import (
	"context"
	"errors"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

const (
	ScanTaskType                     = "media.scan"
	UserID                           = "1"
	DefaultPlaybackCompletionPercent = 90
)

type ContentType string

const (
	ContentMovies  ContentType = "movies"
	ContentTVShows ContentType = "tvshows"
	ContentMusic   ContentType = "music"
)

func (t ContentType) Valid() bool {
	return t == ContentMovies || t == ContentTVShows || t == ContentMusic
}

type ItemKind string

const (
	KindMovie       ItemKind = "movie"
	KindSeries      ItemKind = "series"
	KindSeason      ItemKind = "season"
	KindEpisode     ItemKind = "episode"
	KindMusicArtist ItemKind = "music_artist"
	KindMusicAlbum  ItemKind = "music_album"
	KindAudio       ItemKind = "audio"
)

func (k ItemKind) Folder() bool {
	return k == KindSeries || k == KindSeason || k == KindMusicArtist || k == KindMusicAlbum
}

type Library struct {
	ID                        int64       `json:"id"`
	Name                      string      `json:"name"`
	ContentType               ContentType `json:"content_type"`
	SortOrder                 int         `json:"sort_order"`
	VideoScreenshotEnabled    bool        `json:"video_screenshot_enabled"`
	VideoScreenshotPercent    int         `json:"video_screenshot_percent"`
	PlaybackCompletionPercent int         `json:"playback_completion_percent"`
	HidePeopleWithoutImages   bool        `json:"hide_people_without_images"`
	// RootPath remains available for clients that have not adopted RootPaths.
	RootPath       string     `json:"root_path"`
	RootPaths      []string   `json:"root_paths"`
	ScanGeneration int64      `json:"-"`
	LastScannedAt  *time.Time `json:"last_scanned_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type LibraryInput struct {
	Name                      string      `json:"name"`
	ContentType               ContentType `json:"content_type"`
	RootPath                  string      `json:"root_path"`
	RootPaths                 []string    `json:"root_paths"`
	VideoScreenshotEnabled    *bool       `json:"video_screenshot_enabled,omitempty"`
	VideoScreenshotPercent    *int        `json:"video_screenshot_percent,omitempty"`
	PlaybackCompletionPercent *int        `json:"playback_completion_percent,omitempty"`
	HidePeopleWithoutImages   *bool       `json:"hide_people_without_images,omitempty"`
}

// MetadataBinding identifies a metadata source independently of local playback type.
type MetadataBinding struct {
	SourceType string `json:"source_type"`
	SourceID   int64  `json:"source_id"`
}

const MetadataSourceTMDBMovie = "tmdb_movie"

type MetadataBindingStore interface {
	GetMediaMetadataBinding(context.Context, int64) (*MetadataBinding, error)
}

type Item struct {
	MetadataPriorityKey    string           `json:"-"`
	MetadataBinding        *MetadataBinding `json:"metadata_binding,omitempty"`
	SeriesID               *int64           `json:"series_id,omitempty"`
	SeriesName             string           `json:"series_name,omitempty"`
	ID                     int64            `json:"id"`
	LibraryID              int64            `json:"library_id"`
	ParentID               *int64           `json:"parent_id,omitempty"`
	Kind                   ItemKind         `json:"kind"`
	Path                   string           `json:"path"`
	MediaPath              string           `json:"media_path,omitempty"`
	Name                   string           `json:"name"`
	SortName               string           `json:"sort_name"`
	Container              string           `json:"container,omitempty"`
	SizeBytes              int64            `json:"size_bytes"`
	ModifiedAt             *time.Time       `json:"modified_at,omitempty"`
	TVDBID                 *int64           `json:"tvdb_id,omitempty"`
	MetadataSource         string           `json:"metadata_source"`
	ResolvedMetadataSource string           `json:"resolved_metadata_source,omitempty"`
	TMDBID                 *int64           `json:"tmdb_id,omitempty"`
	IMDbID                 string           `json:"imdb_id,omitempty"`
	Genres                 []string         `json:"genres,omitempty"`
	SeasonNumber           *int             `json:"season_number,omitempty"`
	EpisodeNumber          *int             `json:"episode_number,omitempty"`
	ProductionYear         *int             `json:"production_year,omitempty"`
	PremiereDate           string           `json:"premiere_date,omitempty"`
	Overview               string           `json:"overview,omitempty"`
	CommunityRating        float64          `json:"community_rating,omitempty"`
	RuntimeTicks           int64            `json:"runtime_ticks,omitempty"`
	ImageURL               string           `json:"image_url,omitempty"`
	BackdropURL            string           `json:"backdrop_url,omitempty"`
	ImageSourceURL         string           `json:"image_source_url,omitempty"`
	BackdropSourceURL      string           `json:"backdrop_source_url,omitempty"`
	ImageTag               string           `json:"-"`
	BackdropTag            string           `json:"-"`
	Album                  string           `json:"album,omitempty"`
	AlbumArtist            string           `json:"album_artist,omitempty"`
	Artists                []string         `json:"artists,omitempty"`
	Lyrics                 string           `json:"lyrics,omitempty"`
	TranslatedLyrics       string           `json:"translated_lyrics,omitempty"`
	MusicSource            string           `json:"-"`
	MusicTrackID           string           `json:"-"`
	MusicPictureID         string           `json:"-"`
	MusicLyricID           string           `json:"-"`
	MusicMetadataKey       string           `json:"-"`
	TrackNumber            *int             `json:"track_number,omitempty"`
	DiscNumber             *int             `json:"disc_number,omitempty"`
	UserData               UserData         `json:"user_data"`
	CreatedAt              time.Time        `json:"created_at"`
	UpdatedAt              time.Time        `json:"updated_at"`
	// MetadataRefreshNeeded is set only while scanning. It is not persisted or
	// exposed to clients.
	MetadataRefreshNeeded bool `json:"-"`
	MetadataLocalized     bool `json:"-"`
	MetadataLocked        bool `json:"-"`
}

type ItemInput struct {
	MetadataPriorityKey    string
	LibraryID              int64
	ParentID               *int64
	Kind                   ItemKind
	Path                   string
	MediaPath              string
	Name                   string
	SortName               string
	Container              string
	SizeBytes              int64
	ModifiedAt             *time.Time
	TVDBID                 *int64 `json:"tvdb_id,omitempty"`
	MetadataSource         string `json:"metadata_source"`
	ResolvedMetadataSource string `json:"resolved_metadata_source,omitempty"`
	TMDBID                 *int64
	IMDbID                 string
	Genres                 []string
	SeasonNumber           *int
	EpisodeNumber          *int
	ProductionYear         *int
	PremiereDate           string
	Overview               string
	CommunityRating        float64
	RuntimeTicks           int64
	ImageURL               string
	BackdropURL            string
	Album                  string
	AlbumArtist            string
	Artists                []string
	TrackNumber            *int
	DiscNumber             *int
	MusicSource            string
	MusicTrackID           string
	MusicPictureID         string
	MusicLyricID           string
	MusicMetadataKey       string
	Lyrics                 string
	TranslatedLyrics       string
	ScanGeneration         int64
}

// MetadataInput is the editable, presentation-facing portion of an indexed
// media item. File location and scan bookkeeping remain owned by the scanner.
type MetadataInput struct {
	PreserveLock           bool             `json:"-"`
	MetadataPriorityKey    string           `json:"-"`
	MetadataBinding        *MetadataBinding `json:"metadata_binding,omitempty"`
	MetadataBindingSet     bool             `json:"-"`
	Name                   string           `json:"name"`
	SortName               string           `json:"sort_name"`
	TVDBID                 *int64           `json:"tvdb_id,omitempty"`
	MetadataSource         string           `json:"metadata_source"`
	ResolvedMetadataSource string           `json:"resolved_metadata_source,omitempty"`
	TMDBID                 *int64           `json:"tmdb_id"`
	IMDbID                 string           `json:"imdb_id"`
	Genres                 []string         `json:"genres"`
	SeasonNumber           *int             `json:"season_number"`
	EpisodeNumber          *int             `json:"episode_number"`
	ProductionYear         *int             `json:"production_year"`
	PremiereDate           string           `json:"premiere_date"`
	Overview               string           `json:"overview"`
	CommunityRating        float64          `json:"community_rating"`
	RuntimeTicks           int64            `json:"runtime_ticks"`
	ImageURL               string           `json:"image_url,omitempty"`
	BackdropURL            string           `json:"backdrop_url,omitempty"`
	ImageSourceURL         string           `json:"image_source_url"`
	BackdropSourceURL      string           `json:"backdrop_source_url"`
	Album                  string           `json:"album"`
	AlbumArtist            string           `json:"album_artist"`
	Artists                []string         `json:"artists"`
	TrackNumber            *int             `json:"track_number"`
	DiscNumber             *int             `json:"disc_number"`
	Lyrics                 string           `json:"lyrics"`
	TranslatedLyrics       string           `json:"translated_lyrics"`
}

type UserData struct {
	HideFromResume        bool       `json:"hide_from_resume,omitempty"`
	PlaybackPositionTicks int64      `json:"playback_position_ticks"`
	Played                bool       `json:"played"`
	PlayCount             int        `json:"play_count"`
	Favorite              bool       `json:"favorite"`
	LastPlayedAt          *time.Time `json:"last_played_at,omitempty"`
	UnplayedItemCount     *int       `json:"unplayed_item_count,omitempty"`
}

type PlayedStateStore interface {
	SetMediaPlayedStateForUser(context.Context, int64, int64, bool, time.Time) (UserData, error)
}

type PlaybackUpdate struct {
	PositionTicks int64
	Stopped       bool
}

// UserAccess carries only the authorization data media operations need. It is
// deliberately separate from transport sessions so the Emby server and Manager
// share the same access checks.
type UserAccess struct {
	UserID                 int64
	IsAdmin                bool
	FilterVisibleLibraries bool
}

type ItemQuery struct {
	LibraryID  *int64
	ParentID   *int64
	ParentSet  bool
	AncestorID *int64
	// MusicArtistIDs scopes music albums and audio tracks through the persisted
	// MusicArtist -> MusicAlbum -> Audio hierarchy.
	MusicArtistIDs []int64
	Kinds          []ItemKind
	Search         string
	Recursive      bool
	Latest         bool
	// SkipTotal avoids an expensive count when the transport does not expose it.
	SkipTotal    bool
	Resume       bool
	FavoriteOnly bool
	// PersonIDs is resolved entirely from persisted local people relations.
	PersonIDs      []int64
	SortBy         string
	SortDescending bool
	Start          int
	// Limit is the maximum number of items to return. Zero means no limit.
	Limit int
	// AllowedLibraryIDs is populated by Service for user-visible aggregate queries and must not be
	// supplied by transport clients.
	AllowedLibraryIDs []int64
}

type ImageType string

const (
	ImagePrimary  ImageType = "primary"
	ImageBackdrop ImageType = "backdrop"
)

type ImageSourceType string

const (
	ImageSourceRemote           ImageSourceType = "remote"
	ImageSourceUpload           ImageSourceType = "upload"
	ImageSourceAutoScreenshot   ImageSourceType = "auto_screenshot"
	ImageSourceManualScreenshot ImageSourceType = "manual_screenshot"
)

type LocalImage struct {
	ItemID                 int64
	ImageType              ImageType
	SourceType             ImageSourceType
	SourceURL              string
	SourceVideoFingerprint string
	RelativePath           string
	MIMEType               string
	SizeBytes              int64
	Width                  int
	Height                 int
	SHA256                 string
	UpdatedAt              time.Time
}

type LocalPerson struct {
	Person              domain.MediaPerson
	ProfileSourceURL    string
	ProfileRelativePath string
	ProfileMIMEType     string
	ProfileSizeBytes    int64
	ProfileWidth        int
	ProfileHeight       int
	ProfileSHA256       string
	UpdatedAt           time.Time
}

type LocalizedMetadataStore interface {
	GetMediaItemImage(context.Context, int64, ImageType) (LocalImage, error)
	ListMediaItemImages(context.Context, []int64) (map[int64]map[ImageType]LocalImage, error)
	UpsertMediaItemImage(context.Context, LocalImage) error
	ReplaceMediaItemPeople(context.Context, int64, []LocalPerson) error
	ListMediaItemPeople(context.Context, int64) ([]domain.MediaPerson, error)
	GetMediaPerson(context.Context, int64) (LocalPerson, error)
	SetMediaPersonProfile(context.Context, int64, LocalPerson) error
	SetMediaItemLocalized(context.Context, int64, bool) error
	ListMediaAssetReferences(context.Context) ([]string, []string, error)
}

type ItemPage struct {
	Items []Item `json:"items"`
	Total int    `json:"total"`
}

type NextUpQuery struct {
	LibraryID         *int64
	SeriesID          *int64
	ParentID          *int64
	Kinds             []ItemKind
	DateCutoff        *time.Time
	EnableResumable   bool
	EnableRewatching  bool
	Start             int
	Limit             int
	AllowedLibraryIDs []int64
}

type NextUpStore interface {
	ListMediaNextUpForUser(context.Context, int64, NextUpQuery) (ItemPage, error)
}

type ContinueWatchingSource string

const (
	ContinueWatchingResume ContinueWatchingSource = "resume"
	ContinueWatchingNextUp ContinueWatchingSource = "next_up"
)

type ContinueWatchingItem struct {
	Item   Item                   `json:"item"`
	Source ContinueWatchingSource `json:"source"`
}

type ContinueWatchingPage struct {
	Items []ContinueWatchingItem `json:"items"`
	Total int                    `json:"total"`
}

type ContinueWatchingStore interface {
	ListMediaContinueWatchingForUser(context.Context, int64, NextUpQuery) (ContinueWatchingPage, error)
}

type LibraryCover struct {
	Tag           string
	PrimaryItemID int64
	Items         []Item
}

type Store interface {
	ListLibraries(context.Context) ([]Library, error)
	GetLibrary(context.Context, int64) (Library, error)
	CreateLibrary(context.Context, LibraryInput) (Library, error)
	UpdateLibrary(context.Context, int64, LibraryInput) (Library, error)
	ReorderLibraries(context.Context, []int64) error
	DeleteLibrary(context.Context, int64) error
	BeginLibraryScan(context.Context, int64) (int64, error)
	UpsertMediaItem(context.Context, ItemInput) (Item, bool, error)
	FinishLibraryScan(context.Context, int64, int64) (int, error)
	GetMediaItem(context.Context, int64) (Item, error)
	UpdateMediaItemMetadata(context.Context, int64, MetadataInput) error
	ListMediaItems(context.Context, ItemQuery) (ItemPage, error)
	UpdateMediaUserData(context.Context, int64, UserData) (UserData, error)
	GetMediaItemForUser(context.Context, int64, int64) (Item, error)
	ListMediaItemsForUser(context.Context, int64, ItemQuery) (ItemPage, error)
	UpdateMediaUserDataForUser(context.Context, int64, int64, UserData) (UserData, error)
	ListAccessibleLibraryIDs(context.Context, int64) ([]int64, error)
	UserCanAccessLibrary(context.Context, int64, int64) (bool, error)
	UserCanEditLibrary(context.Context, int64, int64) (bool, error)
}

// MediaItemReassociationStore preserves a media item's identity when a scan
// discovers the same TMDB-backed item at a new path. Implementations must
// reject ambiguous candidates rather than guessing.
type MediaItemReassociationStore interface {
	ReassociateMediaItem(context.Context, int64, ItemInput) (Item, bool, error)
}

var (
	ErrInvalidLibrary             = errors.New("invalid media library")
	ErrDisabled                   = errors.New("media library is disabled")
	ErrLibraryEditForbidden       = errors.New("media library edit permission is required")
	ErrVideoScreenshotUnavailable = errors.New("video screenshots require ffmpeg and ffprobe")
	ErrVideoScreenshotFailed      = errors.New("video screenshot capture failed")
)

type ScanPayload struct {
	LibraryID   int64  `json:"library_id"`
	LibraryName string `json:"library_name,omitempty"`
}

type ScanResult struct {
	Added                int `json:"added"`
	Updated              int `json:"updated"`
	Removed              int `json:"removed"`
	Directories          int `json:"directories"`
	Files                int `json:"files"`
	MetadataFailed       int `json:"metadata_failed"`
	ScreenshotsGenerated int `json:"screenshots_generated"`
	ScreenshotsFailed    int `json:"screenshots_failed"`
}

type RemoteEntry struct {
	Name       string
	IsDir      bool
	Size       int64
	ModifiedAt *time.Time
	Thumbnail  string
}

type OpenList interface {
	ListDirectory(context.Context, string, bool) ([]RemoteEntry, error)
	ResolveDownloadURL(context.Context, string, string) (string, error)
}
