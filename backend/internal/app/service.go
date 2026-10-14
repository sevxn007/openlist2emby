package app

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/client/tvdb"
	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/metadata"
	"gorm.io/gorm"
)

const defaultSessionTTL = 24 * time.Hour
const embySessionTTL = 30 * 24 * time.Hour

type Service struct {
	metadataSources          *metadata.Registry
	identity                 IdentityClient
	identityMu               sync.Mutex
	authorizationMu          sync.Mutex
	store                    Store
	openList                 OpenListClient
	media                    MediaService
	music                    MusicService
	audio                    AudioTagger
	mediaRuntime             MediaLibraryRuntime
	tokenRevoker             UserTokenRevoker
	sessionTTL               time.Duration
	defaultMediaMetadataPath string
}

func (s *Service) SetDefaultMediaMetadataPath(path string) {
	s.defaultMediaMetadataPath = strings.TrimSpace(path)
}

type SetupInput struct {
	OpenListBaseURL  string
	OpenListAdminKey string
	Username         string
	Password         string
	OTPCode          string
}

type SettingsInput struct {
	TVDBAPIKey       string
	TVDBPIN          string
	ClearTVDBAPIKey  bool
	ClearTVDBPIN     bool
	MetadataPriority []string

	OpenListBaseURL     string
	OpenListAdminKey    string
	TMDBAPIKey          string
	MediaLibraryEnabled *bool
	EmbyPort            *int
}

type Status struct {
	SetupRequired       bool
	OpenListBaseURL     string
	AdminKeyConfigured  bool
	MediaLibraryEnabled bool
	EmbyPort            int
}

type SettingsView struct {
	TVDBAPIKeySet    bool     `json:"tvdb_api_key_set"`
	TVDBPINSet       bool     `json:"tvdb_pin_set"`
	TVDBAPIKey       string   `json:"tvdb_api_key"`
	TVDBPIN          string   `json:"tvdb_pin"`
	MetadataPriority []string `json:"metadata_priority"`

	OpenListBaseURL     string `json:"openlist_base_url"`
	OpenListAdminKeySet bool   `json:"openlist_admin_key_set"`
	OpenListAdminKey    string `json:"openlist_admin_key"`
	TMDBAPIKeySet       bool   `json:"tmdb_api_key_set"`
	TMDBAPIKey          string `json:"tmdb_api_key"`
	MediaLibraryEnabled bool   `json:"media_library_enabled"`
	EmbyPort            int    `json:"emby_port"`
	MediaMetadataPath   string `json:"media_metadata_path"`
}

var openListTaskKinds = []string{
	"upload",
	"copy",
	"move",
	"offline_download",
	"offline_download_transfer",
	"decompress",
	"decompress_upload",
}

func (s *Service) SetMediaLibraryRuntime(runtime MediaLibraryRuntime) {
	s.mediaRuntime = runtime
}

func (s *Service) SetUserTokenRevoker(revoker UserTokenRevoker) {
	s.tokenRevoker = revoker
}

type LoginResult struct {
	Session domain.Session
	User    domain.LocalUser
}

type UserUpdateInput struct {
	Role          *domain.LocalUserRole
	Enabled       *bool
	LibraryGrants *[]domain.LibraryGrant
}

type MusicEmbedInput struct {
	ItemID      int64  `json:"item_id,omitempty"`
	Path        string `json:"path"`
	Source      string `json:"source"`
	PictureID   string `json:"picture_id"`
	LyricID     string `json:"lyric_id"`
	EmbedCover  bool   `json:"embed_cover"`
	EmbedLyrics bool   `json:"embed_lyrics"`
}

type MusicEmbedStage string

const (
	MusicEmbedStagePreparing        MusicEmbedStage = "preparing"
	MusicEmbedStageFetchingMetadata MusicEmbedStage = "fetching_metadata"
	MusicEmbedStageDownloading      MusicEmbedStage = "downloading"
	MusicEmbedStageEmbedding        MusicEmbedStage = "embedding"
	MusicEmbedStageUploading        MusicEmbedStage = "uploading"
	MusicEmbedStageCompleted        MusicEmbedStage = "completed"
)

type MusicEmbedProgress struct {
	Stage   MusicEmbedStage `json:"stage"`
	Percent int             `json:"percent"`
}

type MusicEmbedProgressReporter func(MusicEmbedProgress)

func NewService(
	store Store,
	openList OpenListClient,
	media MediaService,
	music MusicService,
	audio AudioTagger,
) *Service {
	return &Service{
		metadataSources: newMetadataRegistry(media),
		store:           store,
		openList:        openList,
		media:           media,
		music:           music,
		audio:           audio,
		sessionTTL:      defaultSessionTTL,
	}
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	settings, err := s.store.GetSettings(ctx)
	if errors.Is(err, domain.ErrNotSetup) {
		return Status{SetupRequired: true}, nil
	}
	if err != nil {
		return Status{}, err
	}
	_, userErr := s.store.IdentityInstance(ctx, settings.OpenListBaseURL)
	if errors.Is(userErr, domain.ErrNotFound) {
		return Status{}, fmt.Errorf("OpenList identity binding is missing; initialize a fresh installation")
	}
	if userErr != nil {
		return Status{}, userErr
	}
	return Status{
		OpenListBaseURL:     settings.OpenListBaseURL,
		AdminKeyConfigured:  settings.OpenListAdminKey != "",
		MediaLibraryEnabled: settings.MediaLibraryEnabled,
		EmbyPort:            settings.EmbyPort,
	}, nil
}

func (s *Service) Setup(ctx context.Context, input SetupInput) error {
	if _, err := s.store.GetSettings(ctx); err == nil {
		return domain.ErrAlreadySetup
	} else if !errors.Is(err, domain.ErrNotSetup) {
		return err
	}
	if strings.TrimSpace(input.Username) == "" || input.Password == "" {
		return badRequest("OpenList username and password required")
	}
	baseURL, err := normalizeBaseURL(input.OpenListBaseURL)
	if err != nil {
		return err
	}
	adminKey := strings.TrimSpace(input.OpenListAdminKey)
	if adminKey == "" {
		return badRequest("openlist admin key is required")
	}
	keyUser, err := s.identityClient().Current(ctx, baseURL, adminKey)
	if err != nil {
		return err
	}
	if keyUser.Role != 2 {
		return unauthorized("OpenList administrator key required")
	}
	_, identity, err := s.identityClient().Login(ctx, baseURL, strings.TrimSpace(input.Username), input.Password, input.OTPCode)
	if err != nil {
		return err
	}
	if identity.Role != 2 || identity.Disabled {
		return unauthorized("OpenList administrator required")
	}
	if err := s.store.SaveIdentitySetup(ctx, baseURL, adminKey, identity); err != nil {
		return err
	}
	if setter, ok := s.store.(interface {
		SetMediaMetadataPath(context.Context, string) error
	}); ok && s.defaultMediaMetadataPath != "" {
		return setter.SetMediaMetadataPath(ctx, s.defaultMediaMetadataPath)
	}
	return nil
}

func (s *Service) Settings(ctx context.Context) (SettingsView, error) {
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		return SettingsView{}, err
	}
	view := maskSettings(settings)
	if view.MediaMetadataPath == "" {
		view.MediaMetadataPath = s.defaultMediaMetadataPath
	}
	return view, nil
}

func (s *Service) UpdateSettings(ctx context.Context, input SettingsInput) (SettingsView, error) {
	baseURL, err := normalizeBaseURL(input.OpenListBaseURL)
	if err != nil {
		return SettingsView{}, err
	}
	current, err := s.store.GetSettings(ctx)
	if err != nil {
		return SettingsView{}, err
	}
	if baseURL != current.OpenListBaseURL {
		return SettingsView{}, badRequest("changing OpenList instances requires a fresh installation")
	}
	priority := input.MetadataPriority
	if priority != nil {
		if err := s.metadataRegistry().ValidatePriority(priority); err != nil {
			return SettingsView{}, badRequest("metadata priority must contain unique registered sources")
		}
	} else {
		priority = metadata.Priority(current)
	}
	tvdbKey, tvdbPIN := current.TVDBAPIKey, current.TVDBPIN
	if v := strings.TrimSpace(input.TVDBAPIKey); v != "" && v != "********" {
		tvdbKey = v
	}
	if v := strings.TrimSpace(input.TVDBPIN); v != "" && v != "********" {
		tvdbPIN = v
	}
	if input.ClearTVDBAPIKey {
		tvdbKey = ""
	}
	if input.ClearTVDBPIN {
		tvdbPIN = ""
	}
	enabled := current.MediaLibraryEnabled
	if input.MediaLibraryEnabled != nil {
		enabled = *input.MediaLibraryEnabled
	}
	port := current.EmbyPort
	if port == 0 {
		port = domain.DefaultEmbyPort
	}
	if input.EmbyPort != nil {
		port = *input.EmbyPort
	}
	if port < 1 || port > 65535 {
		return SettingsView{}, badRequest("emby port must be between 1 and 65535")
	}
	if s.mediaRuntime != nil {
		if err := s.mediaRuntime.Reconfigure(ctx, enabled, port); err != nil {
			return SettingsView{}, badRequest(err.Error())
		}
	}
	settingsErr := s.store.UpdateSettings(ctx, baseURL, strings.TrimSpace(input.OpenListAdminKey), strings.TrimSpace(input.TMDBAPIKey))
	if settingsErr != nil {
		if s.mediaRuntime != nil {
			_ = s.mediaRuntime.Reconfigure(context.Background(), current.MediaLibraryEnabled, current.EmbyPort)
		}
		return SettingsView{}, settingsErr
	}
	if err := s.store.SetMediaLibraryConfiguration(ctx, enabled, port); err != nil {
		if s.mediaRuntime != nil {
			_ = s.mediaRuntime.Reconfigure(context.Background(), current.MediaLibraryEnabled, current.EmbyPort)
		}
		return SettingsView{}, err
	}
	if setter, ok := s.store.(interface {
		SetMetadataConfiguration(context.Context, string, string, []string) error
	}); ok {
		if err := setter.SetMetadataConfiguration(ctx, tvdbKey, tvdbPIN, priority); err != nil {
			return SettingsView{}, err
		}
	}
	return s.Settings(ctx)
}

func (s *Service) TestSettings(ctx context.Context, input SettingsInput) (domain.TestConnectionResult, error) {
	baseURL, err := normalizeBaseURL(input.OpenListBaseURL)
	if err != nil {
		return domain.TestConnectionResult{}, err
	}
	adminKey := strings.TrimSpace(input.OpenListAdminKey)
	if adminKey == "" {
		settings, err := s.store.GetSettings(ctx)
		if err != nil {
			return domain.TestConnectionResult{}, err
		}
		if baseURL != settings.OpenListBaseURL {
			return domain.TestConnectionResult{}, badRequest("provide an administrator key when testing a different OpenList instance")
		}
		adminKey = settings.OpenListAdminKey
	}
	if adminKey == "" {
		return domain.TestConnectionResult{}, badRequest("openlist admin key is required")
	}
	return s.openList.TestConnection(ctx, baseURL, adminKey)
}

func (s *Service) ListUsers(ctx context.Context) ([]domain.LocalUser, error) {
	users, err := s.store.ListLocalUsers(ctx)
	if err != nil {
		return nil, err
	}
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	statusByID := map[int64]string{}
	complete := false
	for page := 1; page <= 10000; page++ {
		result, queryErr := s.identityClient().Users(ctx, settings.OpenListBaseURL, settings.OpenListAdminKey, page, 100)
		if queryErr != nil {
			break
		}
		for _, identity := range result.Content {
			status := "active"
			if identity.Disabled || identity.Role == 1 {
				status = "disabled"
			}
			statusByID[identity.ID] = status
		}
		if page*100 >= result.Total {
			complete = true
			break
		}
		if len(result.Content) == 0 {
			break
		}
	}
	for i := range users {
		users[i].UpstreamStatus = "unavailable"
		if complete {
			if status, ok := statusByID[users[i].OpenListUserID]; ok {
				users[i].UpstreamStatus = status
			}
		}
	}
	return users, nil
}

func (s *Service) GetUser(ctx context.Context, userID int64) (domain.LocalUser, error) {
	return s.store.GetLocalUserByID(ctx, userID)
}

func (s *Service) UpdateUser(ctx context.Context, actor domain.Session, userID int64, input UserUpdateInput) (domain.LocalUser, error) {
	s.authorizationMu.Lock()
	defer s.authorizationMu.Unlock()
	current, err := s.store.GetLocalUserByID(ctx, userID)
	if err != nil {
		return domain.LocalUser{}, err
	}
	updated := current
	if input.Role != nil {
		if !input.Role.Valid() {
			return domain.LocalUser{}, badRequest("role must be admin or media")
		}
		updated.Role = *input.Role
	}
	if input.Enabled != nil {
		updated.Enabled = *input.Enabled
	}
	if actor.UserID == userID && !updated.Enabled {
		return domain.LocalUser{}, badRequest("you cannot disable your own account")
	}
	if current.Role.IsAdmin() && current.Enabled && (!updated.Role.IsAdmin() || !updated.Enabled) {
		if err := s.requireOtherAdministrator(ctx, userID); err != nil {
			return domain.LocalUser{}, err
		}
	}
	if updated.Enabled && (!current.Enabled || (updated.Role.IsAdmin() && !current.Role.IsAdmin())) {
		if err := s.validateBoundIdentity(ctx, current); err != nil {
			return domain.LocalUser{}, err
		}
	}

	grants := current.LibraryGrants
	grantsConfigured := current.LibraryGrantsConfigured
	if input.LibraryGrants != nil {
		grants = *input.LibraryGrants
		grantsConfigured = true
	}
	if !updated.Role.IsAdmin() {
		grantsConfigured = true
	}
	updated.LibraryGrants = grants
	updated.LibraryGrantsConfigured = grantsConfigured
	user, err := s.store.UpdateUser(ctx, updated)
	if err != nil {
		return domain.LocalUser{}, normalizeUserMutationError(err)
	}
	if !user.Enabled {
		if err := s.revokeUserCredentials(ctx, user.ID); err != nil {
			return domain.LocalUser{}, err
		}
	}
	return user, nil
}

func (s *Service) revokeUserCredentials(ctx context.Context, userID int64) error {
	if err := s.store.RevokeUserSessions(ctx, userID); err != nil {
		return err
	}
	if s.tokenRevoker != nil {
		return s.tokenRevoker.RevokeUserTokens(ctx, userID)
	}
	return nil
}

func isUniqueUserError(err error) bool {
	return errors.Is(err, gorm.ErrDuplicatedKey) ||
		(err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint"))
}

func normalizeUserMutationError(err error) error {
	if err == nil || errors.Is(err, domain.ErrNotFound) {
		return err
	}
	return badRequest(err.Error())
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if strings.TrimSpace(token) == "" {
		return nil
	}
	return s.store.DeleteSession(ctx, token)
}

func (s *Service) ProxyOpenList(ctx context.Context, _ domain.Session, path string, body []byte) ([]byte, error) {
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	return s.openList.ProxyJSON(ctx, settings.OpenListBaseURL, path, settings.OpenListAdminKey, body)
}

func (s *Service) ListOpenListTasks(ctx context.Context) ([]domain.OpenListTask, error) {
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	tasks := make([]domain.OpenListTask, 0)
	for _, kind := range openListTaskKinds {
		items, err := s.openList.ListTasks(ctx, settings.OpenListBaseURL, settings.OpenListAdminKey, kind)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, items...)
	}
	return tasks, nil
}

func (s *Service) CancelOpenListTask(ctx context.Context, kind, taskID string) error {
	if !containsOpenListTaskKind(kind) {
		return badRequest("unsupported openlist task type")
	}
	if strings.TrimSpace(taskID) == "" {
		return badRequest("openlist task ID is required")
	}
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		return err
	}
	return s.openList.CancelTask(ctx, settings.OpenListBaseURL, settings.OpenListAdminKey, kind, taskID)
}

func containsOpenListTaskKind(kind string) bool {
	for _, candidate := range openListTaskKinds {
		if candidate == kind {
			return true
		}
	}
	return false
}

func (s *Service) TestMedia(ctx context.Context, submittedKey string) error {
	apiKey, err := s.resolveTMDBAPIKey(ctx, submittedKey)
	if err != nil {
		return err
	}
	return wrapMediaError(s.media.Test(ctx, apiKey), apiKey)
}

func (s *Service) SearchMedia(ctx context.Context, query, language string) ([]domain.MediaSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, badRequest("query is required")
	}
	apiKey, err := s.resolveTMDBAPIKey(ctx, "")
	if err != nil {
		return nil, err
	}
	name, year := metadata.ParseQuery(query)
	results, err := s.media.Search(ctx, apiKey, name, normalizeMediaLanguage(language))
	return metadata.FilterYear(results, year), wrapMediaError(err, apiKey)
}

func (s *Service) DetailMedia(ctx context.Context, mediaType string, id int64, season *int, language string) (domain.MediaDetail, error) {
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if mediaType != "movie" && mediaType != "tv" {
		return domain.MediaDetail{}, badRequest("media type must be movie or tv")
	}
	if id <= 0 {
		return domain.MediaDetail{}, badRequest("id must be a positive integer")
	}
	if season != nil {
		if *season < 0 {
			return domain.MediaDetail{}, badRequest("season must be a non-negative integer")
		}
		if mediaType != "tv" {
			return domain.MediaDetail{}, badRequest("season is only valid for tv results")
		}
	}
	apiKey, err := s.resolveTMDBAPIKey(ctx, "")
	if err != nil {
		return domain.MediaDetail{}, err
	}
	detail, err := s.media.Detail(ctx, apiKey, mediaType, id, season, normalizeMediaLanguage(language))
	return detail, wrapMediaError(err, apiKey)
}

func (s *Service) SearchMusic(ctx context.Context, query, source string) ([]domain.MusicTrack, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, badRequest("query is required")
	}
	source, err := s.normalizeRegisteredMusicSource(source)
	if err != nil {
		return nil, err
	}
	return s.music.Search(ctx, query, source, 20)
}

func (s *Service) MusicDetail(ctx context.Context, source, pictureID, lyricID string) (domain.MusicDetail, error) {
	source, err := s.normalizeRegisteredMusicSource(source)
	if err != nil {
		return domain.MusicDetail{}, err
	}
	pictureID = strings.TrimSpace(pictureID)
	lyricID = strings.TrimSpace(lyricID)
	if pictureID == "" && lyricID == "" {
		return domain.MusicDetail{}, badRequest("picture_id or lyric_id is required")
	}
	return s.music.Detail(ctx, source, pictureID, lyricID)
}

func (s *Service) normalizeMusicEmbedInput(input MusicEmbedInput) (MusicEmbedInput, error) {
	input.Path = normalizeOpenListFilePath(input.Path)
	if input.Path == "" {
		return MusicEmbedInput{}, badRequest("path must identify a file")
	}
	if !input.EmbedCover && !input.EmbedLyrics {
		return MusicEmbedInput{}, badRequest("select cover or lyrics to embed")
	}
	if s.audio == nil || !s.audio.Supports(input.Path) {
		return MusicEmbedInput{}, unsupportedMediaType(
			fmt.Sprintf("audio format %q is not supported", strings.ToLower(filepath.Ext(input.Path))),
		)
	}
	var err error
	input.Source, err = s.normalizeRegisteredMusicSource(input.Source)
	if err != nil {
		return MusicEmbedInput{}, err
	}
	input.PictureID = strings.TrimSpace(input.PictureID)
	input.LyricID = strings.TrimSpace(input.LyricID)
	if input.EmbedCover && input.PictureID == "" {
		return MusicEmbedInput{}, badRequest("picture_id is required")
	}
	if input.EmbedLyrics && input.LyricID == "" {
		return MusicEmbedInput{}, badRequest("lyric_id is required")
	}
	if !input.EmbedCover {
		input.PictureID = ""
	}
	if !input.EmbedLyrics {
		input.LyricID = ""
	}
	return input, nil
}

func (s *Service) embedMusicWithAccess(
	ctx context.Context,
	baseURL string,
	token string,
	input MusicEmbedInput,
	report MusicEmbedProgressReporter,
) (domain.MusicEmbedResult, error) {
	reportMusicEmbedProgress(report, MusicEmbedStagePreparing, 2)
	input, err := s.normalizeMusicEmbedInput(input)
	if err != nil {
		return domain.MusicEmbedResult{}, err
	}

	reportMusicEmbedProgress(report, MusicEmbedStageFetchingMetadata, 8)
	assets, err := s.music.Resolve(ctx, input.Source, input.PictureID, input.LyricID)
	if err != nil {
		return domain.MusicEmbedResult{}, err
	}
	metadata := domain.AudioMetadata{}
	if input.EmbedCover {
		if len(assets.CoverData) == 0 {
			return domain.MusicEmbedResult{}, unprocessable("cover image is unavailable")
		}
		metadata.Cover = assets.CoverData
		metadata.CoverMIME = assets.CoverMIME
	}
	if input.EmbedLyrics {
		metadata.Lyrics = strings.TrimSpace(assets.Detail.Lyrics)
		if metadata.Lyrics == "" {
			metadata.Lyrics = strings.TrimSpace(assets.Detail.TranslatedLyrics)
		}
		if metadata.Lyrics == "" {
			return domain.MusicEmbedResult{}, unprocessable("lyrics are unavailable")
		}
	}

	temporary, err := os.CreateTemp("", "openlist2emby-audio-*"+filepath.Ext(input.Path))
	if err != nil {
		return domain.MusicEmbedResult{}, fmt.Errorf("create temporary audio file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()

	reportMusicEmbedProgress(report, MusicEmbedStageDownloading, 20)
	if _, err := s.openList.DownloadFile(
		ctx,
		baseURL,
		input.Path,
		token,
		temporary,
		scaleMusicTransferProgress(report, MusicEmbedStageDownloading, 20, 60),
	); err != nil {
		return domain.MusicEmbedResult{}, err
	}
	if err := temporary.Close(); err != nil {
		return domain.MusicEmbedResult{}, fmt.Errorf("close temporary audio file: %w", err)
	}
	reportMusicEmbedProgress(report, MusicEmbedStageEmbedding, 65)
	if err := s.audio.Embed(temporaryPath, metadata); err != nil {
		if errors.Is(err, domain.ErrFragmentedAudio) {
			return domain.MusicEmbedResult{}, unsupportedMediaType(domain.ErrFragmentedAudio.Error())
		}
		return domain.MusicEmbedResult{}, unprocessableWithCause("unable to embed audio metadata", err)
	}

	file, err := os.Open(temporaryPath)
	if err != nil {
		return domain.MusicEmbedResult{}, fmt.Errorf("open tagged audio file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return domain.MusicEmbedResult{}, fmt.Errorf("stat tagged audio file: %w", err)
	}
	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(input.Path)))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	reportMusicEmbedProgress(report, MusicEmbedStageUploading, 75)
	if err := s.openList.UploadFile(
		ctx,
		baseURL,
		input.Path,
		token,
		file,
		info.Size(),
		contentType,
		scaleMusicTransferProgress(report, MusicEmbedStageUploading, 75, 99),
	); err != nil {
		return domain.MusicEmbedResult{}, err
	}
	reportMusicEmbedProgress(report, MusicEmbedStageCompleted, 100)
	return domain.MusicEmbedResult{
		EmbeddedCover:  input.EmbedCover,
		EmbeddedLyrics: input.EmbedLyrics,
	}, nil
}

func reportMusicEmbedProgress(report MusicEmbedProgressReporter, stage MusicEmbedStage, percent int) {
	if report == nil {
		return
	}
	percent = min(max(percent, 0), 100)
	report(MusicEmbedProgress{Stage: stage, Percent: percent})
}

func scaleMusicTransferProgress(
	report MusicEmbedProgressReporter,
	stage MusicEmbedStage,
	start int,
	end int,
) domain.TransferProgress {
	if report == nil {
		return nil
	}
	var mutex sync.Mutex
	lastPercent := -1
	return func(transferred, total int64) {
		percent := start
		if total > 0 {
			transferred = min(max(transferred, 0), total)
			percent += int(float64(end-start) * float64(transferred) / float64(total))
		}
		percent = min(max(percent, start), end)

		mutex.Lock()
		defer mutex.Unlock()
		if percent == lastPercent {
			return
		}
		lastPercent = percent
		reportMusicEmbedProgress(report, stage, percent)
	}
}

func normalizeMusicSource(source string) (string, error) {
	source = strings.ToLower(strings.TrimSpace(source))
	if source == "" {
		return "netease", nil
	}
	switch source {
	case "netease", "qmusic", "kuwo":
		return source, nil
	default:
		return "", badRequest("music source is not supported")
	}
}

func (s *Service) normalizeRegisteredMusicSource(source string) (string, error) {
	return normalizeMusicSource(source)
}

func normalizeOpenListFilePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "/" || strings.HasSuffix(value, "/") {
		return ""
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	return value
}

func normalizeMediaLanguage(language string) string {
	language = strings.ToLower(strings.TrimSpace(language))
	if language == "zh" || strings.HasPrefix(language, "zh-") {
		return "zh-CN"
	}
	return "en-US"
}

func (s *Service) resolveTMDBAPIKey(ctx context.Context, submitted string) (string, error) {
	if apiKey := strings.TrimSpace(submitted); apiKey != "" {
		return apiKey, nil
	}
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(settings.TMDBAPIKey) == "" {
		return "", domain.ErrTMDBKeyNotConfigured
	}
	return settings.TMDBAPIKey, nil
}

func maskSettings(settings domain.Settings) SettingsView {
	adminKey := ""
	if settings.OpenListAdminKey != "" {
		adminKey = "********"
	}
	tmdbKey := ""
	if settings.TMDBAPIKey != "" {
		tmdbKey = "********"
	}
	return SettingsView{
		TVDBAPIKeySet: settings.TVDBAPIKey != "", TVDBPINSet: settings.TVDBPIN != "", TVDBAPIKey: maskedSecret(settings.TVDBAPIKey), TVDBPIN: maskedSecret(settings.TVDBPIN), MetadataPriority: metadata.Priority(settings),
		OpenListBaseURL:     settings.OpenListBaseURL,
		OpenListAdminKeySet: settings.OpenListAdminKey != "",
		OpenListAdminKey:    adminKey,
		TMDBAPIKeySet:       settings.TMDBAPIKey != "",
		TMDBAPIKey:          tmdbKey,
		MediaLibraryEnabled: settings.MediaLibraryEnabled,
		EmbyPort:            settings.EmbyPort,
		MediaMetadataPath:   settings.MediaMetadataPath,
	}
}

func normalizeBaseURL(value string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(value), "/")
	if trimmed == "" {
		return "", badRequest("openlist address is required")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", badRequest(err.Error())
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", badRequest("openlist address must start with http:// or https://")
	}
	if parsed.Host == "" {
		return "", badRequest("openlist address must include a host")
	}
	return trimmed, nil
}

func ParsePositiveInt(value string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || id <= 0 {
		return 0, badRequest("id must be a positive integer")
	}
	return id, nil
}

func maskedSecret(value string) string {
	if value != "" {
		return "********"
	}
	return ""
}
func (s *Service) metadataRegistry() *metadata.Registry {
	if s.metadataSources != nil {
		return s.metadataSources
	}
	return newMetadataRegistry(s.media)
}
func newMetadataRegistry(media MediaService) *metadata.Registry {
	r := metadata.NewRegistry()
	if media != nil {
		_ = r.Register(metadata.TMDB, metadata.TMDBAdapter{Client: media})
	}
	_ = r.Register(metadata.TVDB, tvdb.NewService())
	return r
}
func (s *Service) TestMetadataSource(ctx context.Context, source, key, pin string) error {
	if source == "" || source == metadata.TMDB {
		return s.TestMedia(ctx, key)
	}
	if source != metadata.TVDB {
		return badRequest("invalid metadata source")
	}
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		return err
	}
	if key == "" || key == "********" {
		key = settings.TVDBAPIKey
	}
	if pin == "" || pin == "********" {
		pin = settings.TVDBPIN
	}
	return tvdb.NewService().Test(ctx, metadata.Credentials{Key: key, PIN: pin})
}
func (s *Service) SearchMetadataSource(ctx context.Context, source, query string) ([]domain.MediaSearchResult, error) {
	if strings.TrimSpace(query) == "" {
		return nil, badRequest("query is required")
	}
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	if source != "" && source != metadata.Auto {
		return s.metadataRegistry().Search(ctx, settings, source, query)
	}
	configured := false
	for _, candidate := range metadata.Priority(settings) {
		if !s.metadataRegistry().Configured(settings, candidate) {
			continue
		}
		configured = true
		results, err := s.metadataRegistry().Search(ctx, settings, candidate, query)
		if err != nil {
			return nil, err
		}
		if len(results) > 0 {
			return results, nil
		}
	}
	if !configured {
		return nil, metadata.ErrNotConfigured
	}
	return []domain.MediaSearchResult{}, nil
}
func (s *Service) DetailMetadataSource(ctx context.Context, source, kind string, id int64, season *int) (domain.MediaDetail, error) {
	if source == "" {
		source = metadata.TMDB
	}
	if id <= 0 || (kind != "movie" && kind != "tv") || season != nil && (*season < 0 || kind != "tv") {
		return domain.MediaDetail{}, badRequest("invalid metadata detail request")
	}
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		return domain.MediaDetail{}, err
	}
	return s.metadataRegistry().Detail(ctx, settings, source, kind, id, season)
}

func (s *Service) ImageSources(ctx context.Context) ([]string, error) {
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	result := []string{}
	for _, source := range metadata.Priority(settings) {
		if s.metadataRegistry().Configured(settings, source) {
			result = append(result, source)
		}
	}
	return result, nil
}

func (s *Service) MediaImages(ctx context.Context, q domain.MediaImageQuery) ([]domain.MediaImage, error) {
	if q.ID <= 0 || (q.Source != metadata.TMDB && q.Source != metadata.TVDB) ||
		(q.Type != "movie" && q.Type != "tv") || (q.ImageType != "primary" && q.ImageType != "backdrop") ||
		(q.Season != nil && (q.Type != "tv" || *q.Season < 0)) ||
		(q.Episode != nil && (q.Season == nil || *q.Episode < 0)) {
		return nil, badRequest("invalid image request")
	}
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	return s.metadataRegistry().Images(ctx, settings, q)
}
