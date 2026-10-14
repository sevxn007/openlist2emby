package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/sevxn007/openlist2emby/backend/internal/app"
	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
	"github.com/sevxn007/openlist2emby/backend/internal/task"
)

type Server struct {
	application    *app.Service
	tasks          *task.Manager
	mediaLibraries *medialibrary.Service
	staticDir      string
	api            http.Handler
}

type authenticatedHandler func(http.ResponseWriter, *http.Request, domain.Session)

func NewServer(application *app.Service, staticDir string, taskManagers ...*task.Manager) *Server {
	var taskManager *task.Manager
	if len(taskManagers) > 0 {
		taskManager = taskManagers[0]
	}
	return NewServerWithMedia(application, staticDir, taskManager, nil)
}

func NewServerWithMedia(
	application *app.Service,
	staticDir string,
	taskManager *task.Manager,
	mediaLibraries *medialibrary.Service,
) *Server {
	server := &Server{
		application:    application,
		tasks:          taskManager,
		mediaLibraries: mediaLibraries,
		staticDir:      staticDir,
	}
	server.api = server.routes()
	return server
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/manager" || strings.HasPrefix(r.URL.Path, "/api/manager/") {
		s.api.ServeHTTP(w, r)
		return
	}
	s.serveStatic(w, r)
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/manager/status", s.handleStatus)
	mux.HandleFunc("POST /api/manager/setup", s.handleSetup)
	mux.Handle("GET /api/manager/settings", s.withAdmin(s.handleGetSettings))
	mux.Handle("PUT /api/manager/settings", s.withAdmin(s.handleUpdateSettings))
	mux.Handle("POST /api/manager/settings/test", s.withAdmin(s.handleTestSettings))
	mux.Handle("POST /api/manager/media/test", s.withAdmin(s.handleTestMedia))
	mux.Handle("GET /api/manager/media/search", s.withAuth(s.handleSearchMedia))
	mux.Handle("GET /api/manager/media/detail", s.withAuth(s.handleMediaDetail))
	mux.Handle("GET /api/manager/media/images", s.withAdmin(s.handleMediaImages))
	mux.Handle("GET /api/manager/media/image-sources", s.withAdmin(s.handleMediaImageSources))
	mux.Handle("GET /api/manager/music/search", s.withAuth(s.handleSearchMusic))
	mux.Handle("GET /api/manager/music/detail", s.withAuth(s.handleMusicDetail))
	mux.Handle("POST /api/manager/music/embed", s.withAdmin(s.handleEmbedMusic))
	mux.Handle("GET /api/manager/media-libraries", s.withAuth(s.handleListMediaLibraries))
	mux.Handle("POST /api/manager/media-libraries", s.withAdmin(s.handleCreateMediaLibrary))
	mux.Handle("GET /api/manager/media-libraries/{id}", s.withAuth(s.handleGetMediaLibrary))
	mux.Handle("PUT /api/manager/media-libraries/{id}", s.withAdmin(s.handleUpdateMediaLibrary))
	mux.Handle("GET /api/manager/media-libraries/{id}/scan-schedule", s.withAdmin(s.handleGetMediaLibraryScanSchedule))
	mux.Handle("PUT /api/manager/media-libraries/{id}/scan-schedule", s.withAdmin(s.handleUpdateMediaLibraryScanSchedule))
	mux.Handle("PUT /api/manager/media-libraries/order", s.withAdmin(s.handleReorderMediaLibraries))
	mux.Handle("DELETE /api/manager/media-libraries/{id}", s.withAdmin(s.handleDeleteMediaLibrary))
	mux.Handle("POST /api/manager/media-libraries/{id}/scan", s.withAdmin(s.handleScanMediaLibrary))
	mux.Handle("GET /api/manager/media-library/continue-watching", s.withAuth(s.handleContinueWatching))
	mux.Handle("GET /api/manager/media-library/items", s.withAuth(s.handleListMediaLibraryItems))
	mux.Handle("GET /api/manager/media-library/items/{id}", s.withAuth(s.handleGetMediaLibraryItem))
	mux.Handle("GET /api/manager/media-library/items/{id}/next-episode", s.withAuth(s.handleNextMediaLibraryEpisode))
	mux.Handle("GET /api/manager/media-library/items/{id}/people", s.withAuth(s.handleListMediaLibraryItemPeople))
	mux.Handle("GET /api/manager/media-library/items/{id}/images/{type}", s.withAuth(s.handleMediaLibraryItemImage))
	mux.Handle("HEAD /api/manager/media-library/items/{id}/images/{type}", s.withAuth(s.handleMediaLibraryItemImage))
	mux.Handle("PUT /api/manager/media-library/items/{id}/images/{type}", s.withAuth(s.handleUploadMediaLibraryItemImage))
	mux.Handle("POST /api/manager/media-library/items/{id}/images/{type}/select", s.withAdmin(s.handleSelectMediaImage))
	mux.Handle("POST /api/manager/media-library/items/{id}/images/primary/capture", s.withAuth(s.handleCaptureMediaLibraryItemImage))
	mux.Handle("GET /api/manager/media-library/people/{id}/images/primary", s.withAuth(s.handleMediaLibraryPersonImage))
	mux.Handle("HEAD /api/manager/media-library/people/{id}/images/primary", s.withAuth(s.handleMediaLibraryPersonImage))
	mux.Handle("POST /api/manager/media-library/storage/migrations", s.withAdmin(s.handleCreateMediaStorageMigration))
	mux.Handle("PUT /api/manager/media-library/items/{id}/metadata", s.withAuth(s.handleUpdateMediaLibraryItemMetadata))
	mux.Handle("POST /api/manager/media-library/items/{id}/metadata/refresh", s.withAuth(s.handleRefreshMediaLibraryItemMetadata))
	mux.Handle("GET /api/manager/media-library/items/{id}/stream", s.withAuth(s.handleStreamMediaLibraryItem))
	mux.Handle("PUT /api/manager/media-library/items/{id}/playback", s.withAuth(s.handleUpdateMediaLibraryPlayback))
	mux.Handle("PUT /api/manager/media-library/items/{id}/played", s.withAuth(s.handleSetMediaLibraryPlayed))
	mux.Handle("PUT /api/manager/media-library/items/{id}/resume-visibility", s.withAuth(s.handleSetMediaLibraryResumeVisibility))
	mux.Handle("DELETE /api/manager/media-library/items/{id}/resume", s.withAuth(s.handleClearMediaLibraryResume))
	mux.HandleFunc("POST /api/manager/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/manager/auth/logout", s.handleLogout)
	mux.Handle("GET /api/manager/auth/me", s.withAuth(s.handleMe))
	mux.Handle("GET /api/manager/users", s.withAdmin(s.handleListUsers))
	mux.Handle("GET /api/manager/openlist/users", s.withAdmin(s.handleOpenListUsers))
	mux.Handle("POST /api/manager/user-authorizations", s.withAdmin(s.handleGrantIdentity))
	mux.Handle("PUT /api/manager/users/{id}", s.withAdmin(s.handleUpdateUser))
	mux.Handle("GET /api/manager/tasks/types", s.withAdmin(s.handleTaskTypes))
	mux.Handle("GET /api/manager/tasks/schedules", s.withAdmin(s.handleListTaskSchedules))
	mux.Handle("POST /api/manager/tasks/schedules", s.withAdmin(s.handleCreateTaskSchedule))
	mux.Handle("GET /api/manager/tasks/schedules/{id}", s.withAdmin(s.handleGetTaskSchedule))
	mux.Handle("PUT /api/manager/tasks/schedules/{id}", s.withAdmin(s.handleUpdateTaskSchedule))
	mux.Handle("DELETE /api/manager/tasks/schedules/{id}", s.withAdmin(s.handleDeleteTaskSchedule))
	mux.Handle("POST /api/manager/tasks/schedules/{id}/run", s.withAdmin(s.handleRunTaskSchedule))
	mux.Handle("GET /api/manager/tasks/runs", s.withAdmin(s.handleListTaskRuns))
	mux.Handle("POST /api/manager/tasks/runs", s.withAdmin(s.handleCreateTaskRun))
	mux.Handle("GET /api/manager/tasks/runs/{id}", s.withAdmin(s.handleGetTaskRun))
	mux.Handle("DELETE /api/manager/tasks/runs/completed", s.withAdmin(s.handleDeleteCompletedTaskRuns))
	mux.Handle("DELETE /api/manager/tasks/runs/{id}", s.withAdmin(s.handleDeleteTaskRun))
	mux.Handle("GET /api/manager/tasks/runs/{id}/items", s.withAdmin(s.handleListTaskRunItems))
	mux.Handle("POST /api/manager/tasks/runs/{id}/cancel", s.withAdmin(s.handleCancelTaskRun))
	mux.Handle("POST /api/manager/tasks/runs/{id}/retry", s.withAdmin(s.handleRetryTaskRun))
	mux.Handle("GET /api/manager/openlist/tasks", s.withAdmin(s.handleListOpenListTasks))
	mux.Handle("POST /api/manager/openlist/tasks/{type}/{id}/cancel", s.withAdmin(s.handleCancelOpenListTask))
	for path, upstream := range map[string]string{
		"/api/manager/openlist/fs/list":         "/api/fs/list",
		"/api/manager/openlist/fs/mkdir":        "/api/fs/mkdir",
		"/api/manager/openlist/fs/rename":       "/api/fs/rename",
		"/api/manager/openlist/fs/move":         "/api/fs/move",
		"/api/manager/openlist/fs/copy":         "/api/fs/copy",
		"/api/manager/openlist/fs/remove":       "/api/fs/remove",
		"/api/manager/openlist/fs/batch_rename": "/api/fs/batch_rename",
	} {
		mux.Handle("POST "+path, s.withAdmin(s.handleOpenListProxy(upstream)))
	}
	notFound := func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	}
	mux.HandleFunc("/api/manager", notFound)
	mux.HandleFunc("/api/manager/", notFound)
	return mux
}

func (s *Server) withAdmin(next authenticatedHandler) http.Handler {
	return s.withAuth(func(w http.ResponseWriter, r *http.Request, session domain.Session) {
		if !session.IsAdmin() {
			writeError(w, http.StatusForbidden, "administrator access required")
			return
		}
		next(w, r, session)
	})
}

func (s *Server) withAuth(next authenticatedHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "login required")
			return
		}
		session, err := s.application.Authenticate(r.Context(), cookie.Value)
		if errors.Is(err, domain.ErrNotFound) {
			clearSessionCookie(w, r)
			writeError(w, http.StatusUnauthorized, "login required")
			return
		}
		if err != nil {
			if app.StatusCode(err, http.StatusInternalServerError) == http.StatusUnauthorized {
				clearSessionCookie(w, r)
			}
			writeServiceError(w, err, http.StatusInternalServerError)
			return
		}
		setSessionCookie(w, r, session)
		next(w, r, session)
	})
}
