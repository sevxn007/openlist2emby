package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/app"
	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/task"
)

const musicEventHeartbeatInterval = 15 * time.Second

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.application.Status(r.Context())
	if err != nil {
		writeServiceError(w, err, http.StatusInternalServerError)
		return
	}
	writeOK(w, statusResponse{
		SetupRequired:       status.SetupRequired,
		OpenListBaseURL:     status.OpenListBaseURL,
		AdminKeyConfigured:  status.AdminKeyConfigured,
		MediaLibraryEnabled: status.MediaLibraryEnabled,
		EmbyPort:            status.EmbyPort,
	})
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	var req setupRequest
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.application.Setup(r.Context(), app.SetupInput{
		OpenListBaseURL:  req.OpenListBaseURL,
		OpenListAdminKey: req.OpenListAdminKey,
		Username:         req.Username,
		Password:         req.Password,
		OTPCode:          req.OTPCode,
	}); err != nil {
		writeServiceError(w, err, http.StatusInternalServerError)
		return
	}
	writeOK(w, statusResponse{SetupRequired: false})
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	settings, err := s.application.Settings(r.Context())
	if err != nil {
		writeServiceError(w, err, http.StatusInternalServerError)
		return
	}
	writeOK(w, settings)
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var req settingsRequest
	if !readJSON(w, r, &req) {
		return
	}
	settings, err := s.application.UpdateSettings(r.Context(), app.SettingsInput{
		OpenListBaseURL:  req.OpenListBaseURL,
		OpenListAdminKey: req.OpenListAdminKey,
		TMDBAPIKey:       req.TMDBAPIKey,
		TVDBAPIKey:       req.TVDBAPIKey, TVDBPIN: req.TVDBPIN, ClearTVDBAPIKey: req.ClearTVDBAPIKey, ClearTVDBPIN: req.ClearTVDBPIN, MetadataPriority: req.MetadataPriority,
		MediaLibraryEnabled: req.MediaLibraryEnabled,
		EmbyPort:            req.EmbyPort,
	})
	if err != nil {
		writeServiceError(w, err, http.StatusInternalServerError)
		return
	}
	writeOK(w, settings)
}

func (s *Server) handleTestSettings(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var req settingsRequest
	if !readJSON(w, r, &req) {
		return
	}
	result, err := s.application.TestSettings(r.Context(), app.SettingsInput{
		OpenListBaseURL:  req.OpenListBaseURL,
		OpenListAdminKey: req.OpenListAdminKey,
	})
	if err != nil {
		writeServiceError(w, err, http.StatusBadGateway)
		return
	}
	writeOK(w, result)
}

func (s *Server) handleTestMedia(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var req testMediaRequest
	if !readJSON(w, r, &req) {
		return
	}
	key := req.TMDBAPIKey
	if req.Source == "tvdb" {
		key = req.TVDBAPIKey
	}
	if err := s.application.TestMetadataSource(r.Context(), req.Source, key, req.TVDBPIN); err != nil {
		writeServiceError(w, err, http.StatusBadGateway)
		return
	}
	writeOK(w, map[string]bool{"connected": true})
}

func (s *Server) handleSearchMedia(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	results, err := s.application.SearchMetadataSource(r.Context(), r.URL.Query().Get("source"), r.URL.Query().Get("query"))
	if err != nil {
		writeServiceError(w, err, http.StatusBadGateway)
		return
	}
	writeOK(w, mediaSearchResponse{Results: results})
}

func (s *Server) handleMediaDetail(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	id, err := app.ParsePositiveInt(r.URL.Query().Get("id"))
	if err != nil {
		writeServiceError(w, err, http.StatusBadRequest)
		return
	}
	var season *int
	if value := strings.TrimSpace(r.URL.Query().Get("season")); value != "" {
		seasonNumber, err := strconv.Atoi(value)
		if err != nil || seasonNumber < 0 {
			writeError(w, http.StatusBadRequest, "season must be a non-negative integer")
			return
		}
		season = &seasonNumber
	}
	detail, err := s.application.DetailMetadataSource(r.Context(), r.URL.Query().Get("source"), r.URL.Query().Get("type"), id, season)
	if err != nil {
		writeServiceError(w, err, http.StatusBadGateway)
		return
	}
	writeOK(w, detail)
}

func (s *Server) handleMediaImageSources(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	sources, err := s.application.ImageSources(r.Context())
	if err != nil {
		writeServiceError(w, err, http.StatusInternalServerError)
		return
	}
	writeOK(w, struct {
		Sources []string `json:"sources"`
	}{sources})
}

func (s *Server) handleMediaImages(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	params := r.URL.Query()
	id, err := app.ParsePositiveInt(params.Get("id"))
	if err != nil {
		writeServiceError(w, err, http.StatusBadRequest)
		return
	}
	query := domain.MediaImageQuery{Source: params.Get("source"), Type: params.Get("type"), ID: id, ImageType: params.Get("image_type")}
	if value := params.Get("season"); value != "" {
		number, err := strconv.Atoi(value)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid season")
			return
		}
		query.Season = &number
	}
	if value := params.Get("episode"); value != "" {
		number, err := strconv.Atoi(value)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid episode")
			return
		}
		query.Episode = &number
	}
	images, err := s.application.MediaImages(r.Context(), query)
	if err != nil {
		writeServiceError(w, err, http.StatusBadGateway)
		return
	}
	writeOK(w, struct {
		Images []domain.MediaImage `json:"images"`
	}{images})
}

func (s *Server) handleSearchMusic(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	results, err := s.application.SearchMusic(
		r.Context(),
		r.URL.Query().Get("query"),
		r.URL.Query().Get("source"),
	)
	if err != nil {
		writeServiceError(w, err, http.StatusBadGateway)
		return
	}
	writeOK(w, musicSearchResponse{Results: results})
}

func (s *Server) handleMusicDetail(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	assets, err := s.application.MusicDetail(
		r.Context(),
		r.URL.Query().Get("source"),
		r.URL.Query().Get("picture_id"),
		r.URL.Query().Get("lyric_id"),
	)
	if err != nil {
		writeServiceError(w, err, http.StatusBadGateway)
		return
	}
	writeOK(w, assets)
}

func (s *Server) handleEmbedMusic(w http.ResponseWriter, r *http.Request, session domain.Session) {
	if !s.requireTaskManager(w) {
		return
	}
	var req musicEmbedRequest
	if !readJSON(w, r, &req) {
		return
	}
	input := app.MusicEmbedInput{
		Path:        req.Path,
		Source:      req.Source,
		PictureID:   req.PictureID,
		LyricID:     req.LyricID,
		EmbedCover:  req.EmbedCover,
		EmbedLyrics: req.EmbedLyrics,
	}
	payload, err := json.Marshal(input)
	if err != nil {
		writeServiceError(w, err, http.StatusInternalServerError)
		return
	}
	run, err := s.tasks.CreateManualRun(r.Context(), session.Username, task.ManualRunInput{
		Type:    app.MusicEmbedTaskType,
		Payload: payload,
	})
	if err != nil {
		writeTaskError(w, err)
		return
	}
	if strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream") {
		s.handleEmbedMusicStream(w, r, run.ID)
		return
	}
	completed, err := s.tasks.WaitRun(r.Context(), run.ID, nil)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	var result domain.MusicEmbedResult
	if err := json.Unmarshal(completed.Result, &result); err != nil {
		writeError(w, http.StatusBadGateway, "invalid music task result")
		return
	}
	writeOK(w, result)
}

func (s *Server) handleEmbedMusicStream(
	w http.ResponseWriter,
	r *http.Request,
	runID int64,
) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	stream := &musicEventStream{writer: w, flusher: flusher}
	if err := stream.comment("connected"); err != nil {
		return
	}
	type waitResult struct {
		run task.Run
		err error
	}
	waitDone := make(chan waitResult, 1)
	go func() {
		completed, err := s.tasks.WaitRun(
			r.Context(),
			runID,
			func(progress task.Progress) {
				if progress.Stage == "" {
					return
				}
				_ = stream.send("progress", app.MusicEmbedProgress{
					Stage:   app.MusicEmbedStage(progress.Stage),
					Percent: progress.Percent,
				})
			},
		)
		waitDone <- waitResult{run: completed, err: err}
	}()

	heartbeat := time.NewTicker(musicEventHeartbeatInterval)
	defer heartbeat.Stop()
	var completed task.Run
	var err error

waitLoop:
	for {
		select {
		case result := <-waitDone:
			completed, err = result.run, result.err
			break waitLoop
		case <-heartbeat.C:
			if stream.comment("keepalive") != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}

	if err != nil {
		_ = stream.send("error", musicEmbedErrorEvent{
			Code:    app.StatusCode(err, http.StatusInternalServerError),
			Message: app.PublicMessage(err),
		})
		return
	}
	var result domain.MusicEmbedResult
	if err := json.Unmarshal(completed.Result, &result); err != nil {
		_ = stream.send("error", musicEmbedErrorEvent{
			Code:    http.StatusBadGateway,
			Message: "invalid music task result",
		})
		return
	}
	_ = stream.send("complete", result)
}

type musicEventStream struct {
	writer  io.Writer
	flusher http.Flusher
	mutex   sync.Mutex
	failed  bool
}

func (s *musicEventStream) comment(value string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.failed {
		return io.ErrClosedPipe
	}
	if _, err := fmt.Fprintf(s.writer, ": %s\n\n", value); err != nil {
		s.failed = true
		return err
	}
	s.flusher.Flush()
	return nil
}

func (s *musicEventStream) send(event string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.failed {
		return io.ErrClosedPipe
	}
	if _, err := fmt.Fprintf(s.writer, "event: %s\ndata: %s\n\n", event, data); err != nil {
		s.failed = true
		return err
	}
	s.flusher.Flush()
	return nil
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !readJSON(w, r, &req) {
		return
	}
	result, err := s.application.Login(r.Context(), req.Username, req.Password, req.OTPCode)
	if err != nil {
		writeServiceError(w, err, http.StatusUnauthorized)
		return
	}
	setSessionCookie(w, r, result.Session)
	writeOK(w, currentUserResponseFromDomain(result.Session, result.User))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		_ = s.application.Logout(r.Context(), cookie.Value)
	}
	clearSessionCookie(w, r)
	writeOK(w, nil)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request, session domain.Session) {
	user, err := s.application.GetUser(r.Context(), session.UserID)
	if err != nil {
		writeServiceError(w, err, http.StatusInternalServerError)
		return
	}
	writeOK(w, currentUserResponseFromDomain(session, user))
}

func currentUserResponseFromDomain(session domain.Session, user domain.LocalUser) currentUserResponse {
	editableLibraryIDs := make([]int64, 0, len(user.LibraryGrants))

	return currentUserResponse{
		ID: session.UserID, Username: session.Username, Role: string(session.Role),
		ExpiresAt: session.ExpiresAt, IsAdmin: session.IsAdmin(), EditableLibraryIDs: editableLibraryIDs,
	}
}

func (s *Server) handleOpenListProxy(upstreamPath string) authenticatedHandler {
	return func(w http.ResponseWriter, r *http.Request, session domain.Session) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, "request body is too large")
			return
		}
		responseBody, err := s.application.ProxyOpenList(r.Context(), session, upstreamPath, body)
		if err != nil {
			writeServiceError(w, err, http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(responseBody)
	}
}

func (s *Server) handleListOpenListTasks(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	tasks, err := s.application.ListOpenListTasks(r.Context())
	if err != nil {
		writeServiceError(w, err, http.StatusBadGateway)
		return
	}
	writeOK(w, map[string]any{"items": tasks})
}

func (s *Server) handleCancelOpenListTask(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if err := s.application.CancelOpenListTask(r.Context(), r.PathValue("type"), r.PathValue("id")); err != nil {
		writeServiceError(w, err, http.StatusBadGateway)
		return
	}
	writeOK(w, map[string]bool{"canceled": true})
}
