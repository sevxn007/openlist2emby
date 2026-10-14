package httpapi

import (
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

const sessionCookieName = "openlist2emby_session"

type envelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

type setupRequest struct {
	OpenListBaseURL  string `json:"openlist_base_url"`
	OpenListAdminKey string `json:"openlist_admin_key"`
	Username         string `json:"username"`
	Password         string `json:"password"`
	OTPCode          string `json:"otp_code"`
}

type settingsRequest struct {
	TVDBAPIKey       string   `json:"tvdb_api_key"`
	TVDBPIN          string   `json:"tvdb_pin"`
	ClearTVDBAPIKey  bool     `json:"clear_tvdb_api_key"`
	ClearTVDBPIN     bool     `json:"clear_tvdb_pin"`
	MetadataPriority []string `json:"metadata_priority"`

	OpenListBaseURL     string `json:"openlist_base_url"`
	OpenListAdminKey    string `json:"openlist_admin_key"`
	TMDBAPIKey          string `json:"tmdb_api_key"`
	MediaLibraryEnabled *bool  `json:"media_library_enabled"`
	EmbyPort            *int   `json:"emby_port"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	OTPCode  string `json:"otp_code"`
}

type testMediaRequest struct {
	Source     string `json:"source"`
	TVDBAPIKey string `json:"tvdb_api_key"`
	TVDBPIN    string `json:"tvdb_pin"`

	TMDBAPIKey string `json:"tmdb_api_key"`
}

type statusResponse struct {
	SetupRequired       bool   `json:"setup_required"`
	OpenListBaseURL     string `json:"openlist_base_url,omitempty"`
	AdminKeyConfigured  bool   `json:"openlist_admin_key_set,omitempty"`
	MediaLibraryEnabled bool   `json:"media_library_enabled,omitempty"`
	EmbyPort            int    `json:"emby_port,omitempty"`
}

type currentUserResponse struct {
	ID                 int64     `json:"id"`
	Username           string    `json:"username"`
	Role               string    `json:"role"`
	ExpiresAt          time.Time `json:"expires_at"`
	IsAdmin            bool      `json:"is_admin"`
	EditableLibraryIDs []int64   `json:"editable_library_ids"`
}

type userUpdateRequest struct {
	Role          *string                `json:"role"`
	Enabled       *bool                  `json:"enabled"`
	LibraryGrants *[]domain.LibraryGrant `json:"library_grants"`
}

type userResponse struct {
	OpenListUserID int64                 `json:"openlist_user_id"`
	UpstreamStatus string                `json:"upstream_status"`
	ID             int64                 `json:"id"`
	Username       string                `json:"username"`
	Role           string                `json:"role"`
	Enabled        bool                  `json:"enabled"`
	LibraryGrants  []domain.LibraryGrant `json:"library_grants"`
	CreatedAt      time.Time             `json:"created_at"`
	UpdatedAt      time.Time             `json:"updated_at"`
}

type mediaSearchResponse struct {
	Results []domain.MediaSearchResult `json:"results"`
}

type musicSearchResponse struct {
	Results []domain.MusicTrack `json:"results"`
}

type musicEmbedRequest struct {
	Path        string `json:"path"`
	Source      string `json:"source"`
	PictureID   string `json:"picture_id"`
	LyricID     string `json:"lyric_id"`
	EmbedCover  bool   `json:"embed_cover"`
	EmbedLyrics bool   `json:"embed_lyrics"`
}

type musicEmbedErrorEvent struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
