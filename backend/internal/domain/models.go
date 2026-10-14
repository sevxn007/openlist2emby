package domain

import (
	"time"
)

const (
	DefaultEmbyPort = 8096
)

type Settings struct {
	OpenListBaseURL     string
	OpenListAdminKey    string
	TMDBAPIKey          string
	TVDBAPIKey          string
	TVDBPIN             string
	MetadataPriority    []string
	MediaLibraryEnabled bool
	EmbyPort            int
	MediaMetadataPath   string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type TestConnectionResult struct {
	OpenListBaseURL string `json:"openlist_base_url"`
	Username        string `json:"username"`
}

type Session struct {
	Token     string
	UserID    int64
	Username  string
	Role      LocalUserRole
	ExpiresAt time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// LocalUserRole controls which parts of the Manager and media APIs a user can
// access. Keep this deliberately small: an administrator has all capabilities
// while a media user is restricted to explicitly granted libraries.
type LocalUserRole string

const (
	LocalUserRoleAdmin LocalUserRole = "admin"
	LocalUserRoleMedia LocalUserRole = "media"
)

func (r LocalUserRole) Valid() bool {
	return r == LocalUserRoleAdmin || r == LocalUserRoleMedia
}

func (r LocalUserRole) IsAdmin() bool { return r == LocalUserRoleAdmin }

func (s Session) IsAdmin() bool { return s.Role.IsAdmin() }

const OpenListAdminRole = 2

// LibraryAccessLevel controls what a media user may do in a granted library.
// Administrator grants select visible libraries without restricting access.
type LibraryAccessLevel string

const (
	LibraryAccessView LibraryAccessLevel = "view"
	LibraryAccessEdit LibraryAccessLevel = "edit"
)

func (l LibraryAccessLevel) Valid() bool {
	return l == LibraryAccessView || l == LibraryAccessEdit
}

type LibraryGrant struct {
	LibraryID int64              `json:"library_id"`
	Access    LibraryAccessLevel `json:"access"`
}

type LocalUser struct {
	ID             int64
	Username       string
	OpenListUserID int64
	InstanceID     string
	UpstreamStatus string
	Role           LocalUserRole
	Enabled        bool
	LibraryGrants  []LibraryGrant
	// LibraryGrantsConfigured distinguishes the administrator default of all
	// libraries from an explicitly configured empty visible set.
	LibraryGrantsConfigured bool
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

type OpenListUser struct {
	Username string
	Role     int
}

type MediaSearchResult struct {
	Source        string  `json:"source,omitempty"`
	ID            int64   `json:"id"`
	MediaType     string  `json:"media_type"`
	Title         string  `json:"title,omitempty"`
	Name          string  `json:"name,omitempty"`
	OriginalTitle string  `json:"original_title,omitempty"`
	Overview      string  `json:"overview,omitempty"`
	PosterPath    string  `json:"poster_path,omitempty"`
	PosterURL     string  `json:"poster_url,omitempty"`
	ReleaseDate   string  `json:"release_date,omitempty"`
	FirstAirDate  string  `json:"first_air_date,omitempty"`
	VoteAverage   float32 `json:"vote_average"`
	GenreIDs      []int64 `json:"genre_ids,omitempty"`
}

type MediaSeason struct {
	Source       string  `json:"source,omitempty"`
	ID           int64   `json:"id"`
	AirDate      string  `json:"air_date,omitempty"`
	EpisodeCount int     `json:"episode_count"`
	Name         string  `json:"name"`
	Overview     string  `json:"overview,omitempty"`
	PosterPath   string  `json:"poster_path,omitempty"`
	PosterURL    string  `json:"poster_url,omitempty"`
	SeasonNumber int     `json:"season_number"`
	VoteAverage  float32 `json:"vote_average"`
}

type MediaEpisode struct {
	Source        string        `json:"source,omitempty"`
	ID            int64         `json:"id"`
	AirDate       string        `json:"air_date,omitempty"`
	EpisodeNumber int           `json:"episode_number"`
	Name          string        `json:"name"`
	Overview      string        `json:"overview,omitempty"`
	Runtime       int           `json:"runtime,omitempty"`
	SeasonNumber  int           `json:"season_number"`
	StillPath     string        `json:"still_path,omitempty"`
	StillURL      string        `json:"still_url,omitempty"`
	VoteAverage   float32       `json:"vote_average"`
	VoteCount     int64         `json:"vote_count"`
	People        []MediaPerson `json:"people,omitempty"`
}

// MediaCreditsQuery identifies a provider resource. Seasons and episodes use
// the parent series external ID and local Aired Order numbers.
type MediaCreditsQuery struct {
	ExternalID    int64  `json:"external_id,omitempty"`
	Source        string `json:"source,omitempty"`
	MediaType     string `json:"media_type"`
	TMDBID        int64  `json:"tmdb_id"`
	SeasonNumber  *int   `json:"season_number,omitempty"`
	EpisodeNumber *int   `json:"episode_number,omitempty"`
}

// MediaPerson carries a provider identity; persisted ID is the stable local
// person ID used by Emby filters, independent of ExternalID.
type MediaPerson struct {
	Source      string `json:"source,omitempty"`
	ExternalID  int64  `json:"external_id,omitempty"`
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	ProfileURL  string `json:"profile_url,omitempty"`
	ProfileTag  string `json:"-"`
	Type        string `json:"type"`
	Role        string `json:"role,omitempty"`
	DisplayRole string `json:"display_role,omitempty"`
	SortOrder   int    `json:"sort_order"`
}

type MediaDetail struct {
	Source           string         `json:"source,omitempty"`
	ID               int64          `json:"id"`
	MediaType        string         `json:"media_type"`
	Title            string         `json:"title,omitempty"`
	Name             string         `json:"name,omitempty"`
	OriginalTitle    string         `json:"original_title,omitempty"`
	Overview         string         `json:"overview,omitempty"`
	PosterPath       string         `json:"poster_path,omitempty"`
	PosterURL        string         `json:"poster_url,omitempty"`
	BackdropPath     string         `json:"backdrop_path,omitempty"`
	BackdropURL      string         `json:"backdrop_url,omitempty"`
	IMDbID           string         `json:"imdb_id,omitempty"`
	Genres           []string       `json:"genres,omitempty"`
	Runtime          int            `json:"runtime,omitempty"`
	ReleaseDate      string         `json:"release_date,omitempty"`
	FirstAirDate     string         `json:"first_air_date,omitempty"`
	VoteAverage      float32        `json:"vote_average"`
	NumberOfSeasons  int            `json:"number_of_seasons,omitempty"`
	NumberOfEpisodes int            `json:"number_of_episodes,omitempty"`
	SeasonNumber     *int           `json:"season_number,omitempty"`
	Seasons          []MediaSeason  `json:"seasons,omitempty"`
	Episodes         []MediaEpisode `json:"episodes,omitempty"`
	People           []MediaPerson  `json:"people,omitempty"`
}

type MusicTrack struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Artists     []string `json:"artists"`
	Album       string   `json:"album"`
	PictureID   string   `json:"picture_id"`
	LyricID     string   `json:"lyric_id"`
	Source      string   `json:"source"`
	DurationMS  int64    `json:"duration_ms,omitempty"`
	TrackNumber int      `json:"track_number,omitempty"`
	DiscNumber  int      `json:"disc_number,omitempty"`
}

type MusicDetail struct {
	CoverURL         string `json:"cover_url,omitempty"`
	Lyrics           string `json:"lyrics,omitempty"`
	TranslatedLyrics string `json:"translated_lyrics,omitempty"`
}

type MusicAssets struct {
	Detail    MusicDetail
	CoverData []byte
	CoverMIME string
}

type AudioMetadata struct {
	Title     string
	Artist    string
	Album     string
	Cover     []byte
	CoverMIME string
	Lyrics    string
}

type OpenListFile struct {
	Name string
	Size int64
}

// OpenListTask is a task reported by OpenList's task manager.
type OpenListTask struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Name       string     `json:"name"`
	State      int        `json:"state"`
	Status     string     `json:"status"`
	Progress   float64    `json:"progress"`
	StartTime  *time.Time `json:"start_time,omitempty"`
	EndTime    *time.Time `json:"end_time,omitempty"`
	TotalBytes int64      `json:"total_bytes"`
	Error      string     `json:"error,omitempty"`
}

type TransferProgress func(transferred, total int64)

type MusicEmbedResult struct {
	EmbeddedCover  bool `json:"embedded_cover"`
	EmbeddedLyrics bool `json:"embedded_lyrics"`
}
