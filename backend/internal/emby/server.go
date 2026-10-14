package emby

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/app"
	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
)

const (
	productName = "openlist2emby"
	embyVersion = "4.9.5.0"
)

type Server struct {
	media         *medialibrary.Service
	tokens        TokenStore
	accounts      LocalAccountStore
	credentials   CredentialVerifier
	serverID      string
	vidHubMu      sync.Mutex
	vidHubSeasons map[vidHubSeasonKey]vidHubSeasonSelection
}

type viewer struct {
	user domain.LocalUser
}

func (v viewer) id() int64 { return v.user.ID }

func (v viewer) idString() string { return strconv.FormatInt(v.user.ID, 10) }

func (v viewer) isAdmin() bool { return v.user.Role.IsAdmin() }

func (v viewer) mediaAccess() medialibrary.UserAccess {
	return medialibrary.UserAccess{UserID: v.id(), IsAdmin: v.isAdmin(), FilterVisibleLibraries: true}
}

func NewServer(media *medialibrary.Service, tokens TokenStore, accounts LocalAccountStore, credentials CredentialVerifier) (*Server, error) {
	proposed, err := newServerID()
	if err != nil {
		return nil, err
	}
	serverID, err := tokens.GetOrCreateEmbyServerID(context.Background(), proposed)
	if err != nil {
		return nil, err
	}
	return &Server{media: media, tokens: tokens, accounts: accounts, credentials: credentials, serverID: serverID}, nil
}

func handlesPath(requestPath string) bool {
	segments := splitPath(requestPath)
	if len(segments) == 0 {
		return false
	}
	if segments[0] == "emby" || segments[0] == "mediabrowser" {
		return true
	}
	switch segments[0] {
	case "system", "users", "items", "artists", "persons", "videos", "audio", "shows", "sessions", "search", "branding", "mediasegments":
		return true
	default:
		return false
	}
}

// Wrap routes Emby-compatible paths to the server and delegates all other
// requests to next.
func (s *Server) Wrap(next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s != nil && handlesPath(r.URL.Path) {
			s.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if s == nil || s.media == nil || s.tokens == nil || s.accounts == nil || s.credentials == nil {
		writeEmbyError(w, http.StatusServiceUnavailable, "ServerUnavailable", "media server is unavailable")
		return
	}
	enabled, err := s.media.Enabled(r.Context())
	if err != nil {
		writeEmbyError(w, http.StatusInternalServerError, "ServerError", "could not read media server settings")
		return
	}
	if !enabled {
		http.NotFound(w, r)
		return
	}
	segments := splitPath(r.URL.Path)
	if len(segments) > 0 && (segments[0] == "emby" || segments[0] == "mediabrowser") {
		segments = segments[1:]
	}
	if len(segments) == 0 {
		http.NotFound(w, r)
		return
	}
	if s.servePublic(w, r, segments) {
		return
	}
	if isPublicImageRequest(segments, r) {
		if s.handlePublicImage(w, r, segments) {
			return
		}
		if parseClientAuthorization(r).Token == "" {
			writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "image not found")
			return
		}
	}
	auth := parseClientAuthorization(r)
	viewer, valid, err := s.authenticatedViewer(r.Context(), auth.Token)
	if err != nil {
		status := app.StatusCode(err, http.StatusInternalServerError)
		if errors.Is(err, domain.ErrNotFound) {
			status = http.StatusUnauthorized
		}
		writeEmbyError(w, status, "AuthenticationError", "could not validate OpenList identity")
		return
	}
	if auth.Token == "" || !valid {
		writeEmbyError(w, http.StatusUnauthorized, "Unauthorized", "access token is required")
		return
	}
	if !s.serveAuthenticated(w, r, segments, auth, viewer) {
		http.NotFound(w, r)
	}
}

func isPublicImageRequest(path []string, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if len(path) != 4 && len(path) != 5 || path[2] != "images" || path[0] != "items" && path[0] != "persons" {
		return false
	}
	return true
}

func (s *Server) handlePublicImage(w http.ResponseWriter, r *http.Request, path []string) bool {
	if len(path) == 5 && path[4] != "0" {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "image not found")
		return true
	}
	if path[0] == "persons" || strings.HasPrefix(queryValueFold(r, "tag"), "person:") {
		personValue := path[1]
		if tag := queryValueFold(r, "tag"); strings.HasPrefix(tag, "person:") {
			personValue = strings.TrimPrefix(tag, "person:")
		}
		personID, err := parsePositiveID(personValue)
		if err != nil || !strings.EqualFold(path[3], "Primary") {
			writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "person image not found")
			return true
		}
		content, err := s.media.GetPublicPersonImage(r.Context(), personID, embyImageVariant(r))
		if err != nil {
			return false
		}
		serveEmbyAsset(w, r, content)
		return true
	}
	if !strings.EqualFold(path[3], "Primary") && !strings.EqualFold(path[3], "Backdrop") {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "media image not found")
		return true
	}
	id, err := parsePositiveID(path[1])
	if err != nil {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "media image not found")
		return true
	}
	typeValue := medialibrary.ImagePrimary
	if strings.EqualFold(path[3], "Backdrop") {
		typeValue = medialibrary.ImageBackdrop
	}
	if libraryID, ok := parseLibraryViewID(path[1]); ok {
		content, coverErr := s.media.GetPublicLibraryCover(r.Context(), libraryID, queryValueFold(r, "tag"), embyImageVariant(r))
		if coverErr != nil {
			writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "media image not found")
			return true
		}
		serveEmbyAsset(w, r, content)
		return true
	}
	content, err := s.media.GetPublicItemImage(r.Context(), id, typeValue, queryValueFold(r, "tag"), embyImageVariant(r))
	if err != nil {
		if typeValue == medialibrary.ImagePrimary {
			if person, personErr := s.media.GetPublicPersonImageWithTag(r.Context(), id, queryValueFold(r, "tag"), embyImageVariant(r)); personErr == nil {
				serveEmbyAsset(w, r, person)
				return true
			}
		}
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "media image not found")
		return true
	}
	serveEmbyAsset(w, r, content)
	return true
}

func (s *Server) authenticatedViewer(ctx context.Context, token string) (viewer, bool, error) {
	if strings.TrimSpace(token) == "" {
		return viewer{}, false, nil
	}
	identity, valid, err := s.tokens.GetEmbyTokenIdentity(ctx, tokenHash(token))
	if err != nil || !valid {
		return viewer{}, valid, err
	}
	session, err := s.credentials.AuthenticateEmby(ctx, identity.IdentitySession)
	if err != nil {
		return viewer{}, false, err
	}
	if session.UserID != identity.UserID {
		return viewer{}, false, nil
	}
	user, err := s.accounts.GetLocalUserByID(ctx, identity.UserID)
	if errors.Is(err, domain.ErrNotFound) {
		return viewer{}, false, nil
	}
	if err != nil {
		return viewer{}, false, err
	}
	if !user.Enabled {
		return viewer{}, false, nil
	}
	return viewer{user: user}, true, nil
}

func (s *Server) servePublic(w http.ResponseWriter, r *http.Request, path []string) bool {
	if pathEquals(path, "system", "info", "public") && r.Method == http.MethodGet {
		writeEmbyJSON(w, http.StatusOK, s.publicInfo(r))
		return true
	}
	if pathEquals(path, "system", "ping") && (r.Method == http.MethodGet || r.Method == http.MethodPost || r.Method == http.MethodHead) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write([]byte("openlist2emby"))
		}
		return true
	}
	if pathEquals(path, "users", "public") && r.Method == http.MethodGet {
		users, err := s.accounts.ListLocalUsers(r.Context())
		if err != nil {
			writeEmbyError(w, http.StatusInternalServerError, "ServerError", "could not list authorized users")
			return true
		}
		result := make([]userDTO, 0, len(users))
		for _, user := range users {
			if user.Enabled {
				result = append(result, s.userDTO(user))
			}
		}
		writeEmbyJSON(w, http.StatusOK, result)
		return true
	}
	if pathEquals(path, "users", "authenticatebyname") && r.Method == http.MethodPost {
		s.handleAuthenticate(w, r)
		return true
	}
	return false
}

func (s *Server) serveAuthenticated(w http.ResponseWriter, r *http.Request, path []string, auth clientAuthorization, viewer viewer) bool {
	switch {
	case pathEquals(path, "system", "info") && r.Method == http.MethodGet:
		writeEmbyJSON(w, http.StatusOK, systemInfo{publicSystemInfo: s.publicInfo(r), WebSocketPortNumber: 0, CompletedInstallations: []string{}})
		return true
	case pathEquals(path, "system", "endpoint") && r.Method == http.MethodGet:
		writeEmbyJSON(w, http.StatusOK, map[string]any{"IsLocal": false, "IsInNetwork": false})
		return true
	case len(path) == 2 && path[0] == "users" && r.Method == http.MethodGet:
		if path[1] != viewer.idString() {
			writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "user not found")
			return true
		}
		writeEmbyJSON(w, http.StatusOK, s.userDTO(viewer.user))
		return true
	case len(path) >= 3 && path[0] == "users" && path[2] == "views" && r.Method == http.MethodGet:
		s.handleViews(w, r, path, viewer)
		return true
	case len(path) >= 3 && path[0] == "users" && path[2] == "playeditems":
		s.handlePlayedItems(w, r, path, auth.Token, viewer)
		return true
	case len(path) >= 3 && path[0] == "users" && path[2] == "playingitems":
		s.handlePlayingItems(w, r, path, viewer)
		return true
	case len(path) >= 3 && path[0] == "users" && path[2] == "favoriteitems":
		s.handleUserFavoriteItems(w, r, path, viewer)
		return true
	case len(path) == 5 && path[0] == "users" && path[2] == "items" && path[4] == "hidefromresume":
		s.handleHideFromResume(w, r, path, viewer)
		return true
	case len(path) == 5 && path[0] == "users" && path[2] == "items" && path[4] == "userdata":
		s.handleUserItemData(w, r, path, viewer)
		return true
	case len(path) >= 3 && path[0] == "users" && path[2] == "items":
		s.handleUserItems(w, r, path, auth.Token, viewer)
		return true
	case path[0] == "items":
		s.handleItems(w, r, path, auth.Token, viewer)
		return true
	case path[0] == "artists" && len(path) == 1 && r.Method == http.MethodGet:
		s.handleArtists(w, r, auth.Token, viewer)
		return true
	case pathEquals(path, "artists", "albumartists") && r.Method == http.MethodGet:
		s.handleArtists(w, r, auth.Token, viewer)
		return true
	case path[0] == "persons":
		s.handlePersonImages(w, r, path, viewer)
		return true
	case path[0] == "shows":
		s.handleShows(w, r, path, auth.Token, viewer)
		return true
	case path[0] == "audio" && len(path) == 3 && strings.EqualFold(path[2], "lyrics") && r.Method == http.MethodGet:
		s.handleAudioLyrics(w, r, path, viewer)
		return true
	case (path[0] == "videos" || path[0] == "audio") && len(path) >= 3:
		s.handleStream(w, r, path, viewer)
		return true
	case path[0] == "sessions":
		s.handleSessions(w, r, path, auth, viewer)
		return true
	case pathEquals(path, "branding", "configuration") && r.Method == http.MethodGet:
		writeEmbyJSON(w, http.StatusOK, map[string]any{"LoginDisclaimer": "", "CustomCss": "", "SplashscreenEnabled": false})
		return true
	case pathEquals(path, "search", "hints") && r.Method == http.MethodGet:
		s.handleSearchHints(w, r, viewer)
		return true
	case len(path) >= 2 && path[0] == "mediasegments" && r.Method == http.MethodGet:
		writeEmbyJSON(w, http.StatusOK, []any{})
		return true
	default:
		return false
	}
}

func (s *Server) handleAuthenticate(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Username string `json:"Username"`
		Pw       string `json:"Pw"`
		Password string `json:"Password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&request); err != nil {
		writeEmbyError(w, http.StatusBadRequest, "InvalidRequest", "invalid authentication request")
		return
	}
	password := request.Pw
	if password == "" {
		password = request.Password
	}
	result, err := s.credentials.LoginEmby(r.Context(), request.Username, password)
	if err != nil {
		writeEmbyError(w, app.StatusCode(err, http.StatusUnauthorized), "Unauthorized", app.PublicMessage(err))
		return
	}
	user := result.User
	persisted := false
	defer func() {
		if !persisted {
			_ = s.credentials.Logout(r.Context(), result.Session.Token)
		}
	}()
	auth := parseClientAuthorization(r)
	if strings.TrimSpace(auth.DeviceID) == "" {
		auth.DeviceID = "unknown"
	}
	token, err := newAccessToken()
	if err != nil {
		writeEmbyError(w, http.StatusInternalServerError, "ServerError", "could not create access token")
		return
	}
	if err := s.tokens.CreateEmbyToken(r.Context(), tokenHash(token), TokenInput{
		IdentitySession: result.Session.Token, UserID: user.ID, DeviceID: auth.DeviceID, DeviceName: auth.Device, ClientName: auth.Client, ClientVersion: auth.Version,
	}); err != nil {
		writeEmbyError(w, http.StatusInternalServerError, "ServerError", "could not create access token")
		return
	}
	persisted = true
	now := embyTime(time.Now())
	dto := s.userDTO(user)
	writeEmbyJSON(w, http.StatusOK, authenticationResult{
		User: dto, AccessToken: token, ServerId: s.serverID,
		SessionInfo: sessionInfo{
			PlayState: sessionPlayState{CanSeek: true, RepeatMode: "RepeatNone"}, AdditionalUsers: []string{},
			Capabilities:   sessionCapabilities{PlayableMediaTypes: []string{"Audio", "Video"}, SupportedCommands: []string{}, SupportsMediaControl: true, SupportsPersistentIdentifier: true},
			RemoteEndPoint: r.RemoteAddr, PlayableMediaTypes: []string{"Audio", "Video"}, Id: tokenHash(token)[:32],
			UserId: strconv.FormatInt(user.ID, 10), UserName: user.Username, Client: auth.Client,
			LastActivityDate: now, LastPlaybackCheckIn: now, DeviceName: auth.Device, DeviceId: auth.DeviceID,
			ApplicationVersion: auth.Version, IsActive: true, SupportsMediaControl: true,
		},
	})
}

func (s *Server) userDTO(user domain.LocalUser) userDTO {
	return userDTO{
		Name: user.Username, ServerId: s.serverID, Id: strconv.FormatInt(user.ID, 10), HasPassword: true, HasConfiguredPassword: true,
		Configuration: userConfiguration{
			PlayDefaultAudioTrack: true, GroupedFolders: []string{}, SubtitleMode: "Default",
			OrderedViews: []string{}, LatestItemsExcludes: []string{}, MyMediaExcludes: []string{},
			RememberAudioSelections: true, RememberSubtitleSelections: true,
		},
		Policy: userPolicy{
			IsAdministrator: user.Role.IsAdmin(), IsDisabled: !user.Enabled, EnableUserPreferenceAccess: true, EnableSharedDeviceControl: true,
			EnableRemoteAccess: true, EnableMediaPlayback: true, EnableContentDownloading: true,
			EnableAllDevices: true, EnableAllChannels: true, EnableAllFolders: true,
			BlockedTags: []string{}, AllowedTags: []string{}, EnabledDevices: []string{}, EnabledChannels: []string{}, EnabledFolders: []string{},
			AuthenticationProviderId: "OpenList2Emby.OpenList", PasswordResetProviderId: "OpenList2Emby.Disabled",
		},
	}
}

func (s *Server) publicInfo(r *http.Request) publicSystemInfo {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); forwarded == "http" || forwarded == "https" {
		scheme = forwarded
	}
	return publicSystemInfo{
		LocalAddress: scheme + "://" + r.Host, ServerName: productName, Version: embyVersion,
		ProductName: productName, OperatingSystem: runtime.GOOS, Id: s.serverID, StartupWizardCompleted: true,
	}
}

func splitPath(value string) []string {
	parts := strings.Split(strings.Trim(value, "/"), "/")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			result = append(result, strings.ToLower(part))
		}
	}
	return result
}

func pathEquals(actual []string, expected ...string) bool {
	if len(actual) != len(expected) {
		return false
	}
	for index := range actual {
		if actual[index] != expected[index] {
			return false
		}
	}
	return true
}

func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Emby-Authorization, X-Emby-Token, X-MediaBrowser-Token")
	w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, POST, DELETE, OPTIONS")
}

func writeEmbyJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if value != nil {
		_ = json.NewEncoder(w).Encode(value)
	}
}

func writeEmbyError(w http.ResponseWriter, status int, code, message string) {
	writeEmbyJSON(w, status, errorResponse{ResponseStatus: responseStatus{ErrorCode: code, Message: message, StackTrace: "", Errors: []string{}}})
}

func parsePositiveID(value string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid item ID")
	}
	return id, nil
}
