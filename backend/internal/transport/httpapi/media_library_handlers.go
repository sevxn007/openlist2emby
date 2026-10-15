package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
	"github.com/sevxn007/openlist2emby/backend/internal/task"
)

const (
	defaultMediaScanCron     = "0 * * * *"
	defaultMediaScanTimezone = "UTC"
)

type mediaLibraryScanScheduleInput struct {
	CronExpression string `json:"cron_expression"`
	Timezone       string `json:"timezone"`
	Enabled        bool   `json:"enabled"`
}

func (s *Server) handleMediaLibraryItemImage(w http.ResponseWriter, r *http.Request, session domain.Session) {
	id, ok := parseMediaID(w, r.PathValue("id"))
	if !ok {
		return
	}
	imageType := medialibrary.ImageType(strings.ToLower(strings.TrimSpace(r.PathValue("type"))))
	content, err := s.mediaLibraries.GetItemImageForUser(r.Context(), mediaUserAccess(session), id, imageType, mediaImageVariant(r))
	if err != nil {
		writeMediaLibraryError(w, err)
		return
	}
	serveMediaAsset(w, r, content)
}

func (s *Server) handleUploadMediaLibraryItemImage(w http.ResponseWriter, r *http.Request, session domain.Session) {
	if !s.requireMediaLibraries(w) {
		return
	}
	id, ok := parseMediaID(w, r.PathValue("id"))
	if !ok {
		return
	}
	imageType := medialibrary.ImageType(strings.ToLower(strings.TrimSpace(r.PathValue("type"))))
	r.Body = http.MaxBytesReader(w, r.Body, medialibrary.MaxUploadedImageBytes+(1<<20))
	if err := r.ParseMultipartForm(medialibrary.MaxUploadedImageBytes); err != nil {
		writeError(w, http.StatusBadRequest, "invalid image upload")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "image file is required")
		return
	}
	defer file.Close()
	item, err := s.mediaLibraries.UploadItemImageForUser(r.Context(), mediaUserAccess(session), id, imageType, file)
	if err != nil {
		writeMediaLibraryError(w, err)
		return
	}
	writeOK(w, projectMediaItem(session, item))
}

func (s *Server) handleCaptureMediaLibraryItemImage(w http.ResponseWriter, r *http.Request, session domain.Session) {
	if !s.requireMediaLibraries(w) {
		return
	}
	id, ok := parseMediaID(w, r.PathValue("id"))
	if !ok {
		return
	}
	item, err := s.mediaLibraries.CaptureItemPrimaryImageForUser(r.Context(), mediaUserAccess(session), id)
	if err != nil {
		writeMediaLibraryError(w, err)
		return
	}
	writeOK(w, projectMediaItem(session, item))
}

func (s *Server) handleMediaLibraryPersonImage(w http.ResponseWriter, r *http.Request, session domain.Session) {
	id, ok := parseMediaID(w, r.PathValue("id"))
	if !ok {
		return
	}
	content, err := s.mediaLibraries.GetPersonImageForUser(r.Context(), mediaUserAccess(session), id, mediaImageVariant(r))
	if err != nil {
		writeMediaLibraryError(w, err)
		return
	}
	serveMediaAsset(w, r, content)
}

func (s *Server) handleCreateMediaStorageMigration(w http.ResponseWriter, r *http.Request, session domain.Session) {
	if !s.requireMediaLibraries(w) || !s.requireTaskManager(w) {
		return
	}
	var input medialibrary.StorageMigrationPayload
	if !readJSON(w, r, &input) {
		return
	}
	active, err := s.tasks.ListRuns(r.Context(), task.RunFilter{
		Statuses: []task.Status{task.StatusQueued, task.StatusRunning}, Type: medialibrary.StorageMigrationTaskType, Page: 1, PerPage: 1,
	})
	if err != nil {
		writeTaskError(w, err)
		return
	}
	if active.Total > 0 {
		writeError(w, http.StatusConflict, "media metadata migration is already running")
		return
	}
	payload, err := json.Marshal(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media metadata path")
		return
	}
	run, err := s.tasks.CreateManualRun(r.Context(), session.Username, task.ManualRunInput{Type: medialibrary.StorageMigrationTaskType, Payload: payload})
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeData(w, http.StatusAccepted, run)
}

func mediaImageVariant(r *http.Request) medialibrary.ImageVariant {
	value := func(names ...string) string {
		for _, name := range names {
			if found := r.URL.Query().Get(name); found != "" {
				return found
			}
		}
		return ""
	}
	integer := func(names ...string) int {
		parsed, _ := strconv.Atoi(strings.TrimSpace(value(names...)))
		return parsed
	}
	return medialibrary.ImageVariant{
		MaxWidth: integer("MaxWidth", "max_width"), MaxHeight: integer("MaxHeight", "max_height"),
		Width: integer("Width", "width"), Height: integer("Height", "height"), Quality: integer("Quality", "quality"),
		Format: value("Format", "format"),
	}
}

func serveMediaAsset(w http.ResponseWriter, r *http.Request, content medialibrary.AssetContent) {
	etag := `"` + content.ETag + `"`
	w.Header().Set("Content-Type", content.MIMEType)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	if strings.TrimSpace(r.Header.Get("If-None-Match")) == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	file, err := os.Open(content.Path)
	if err != nil {
		writeError(w, http.StatusNotFound, "media image not found")
		return
	}
	defer file.Close()
	http.ServeContent(w, r, "", content.LastModified, file)
}

func (s *Server) handleListMediaLibraries(w http.ResponseWriter, r *http.Request, session domain.Session) {
	if !s.requireMediaLibraries(w) {
		return
	}
	includeHidden := parseBool(r.URL.Query().Get("include_hidden"))
	if includeHidden && !session.IsAdmin() {
		writeError(w, http.StatusForbidden, "administrator access is required")
		return
	}
	var libraries []medialibrary.Library
	var err error
	if includeHidden {
		libraries, err = s.mediaLibraries.ListLibraries(r.Context())
	} else {
		libraries, err = s.mediaLibraries.ListLibrariesForUser(r.Context(), mediaUserAccess(session))
	}
	if err != nil {
		writeMediaLibraryError(w, err)
		return
	}
	response := make([]medialibrary.Library, 0, len(libraries))
	for _, library := range libraries {
		response = append(response, projectMediaLibrary(session, library))
	}
	writeOK(w, map[string]any{"items": response, "total": len(response)})
}

func (s *Server) handleCreateMediaLibrary(w http.ResponseWriter, r *http.Request, session domain.Session) {
	if !s.requireMediaLibraries(w) || !s.requireTaskManager(w) {
		return
	}
	var input medialibrary.LibraryInput
	if !readJSON(w, r, &input) {
		return
	}
	library, err := s.mediaLibraries.CreateLibrary(r.Context(), input)
	if err != nil {
		writeMediaLibraryError(w, err)
		return
	}
	scheduleInput, err := newMediaScanTaskScheduleInput(library.ID, library.Name, defaultMediaScanCron, defaultMediaScanTimezone, true)
	if err == nil {
		_, err = s.tasks.CreateSchedule(r.Context(), session.Username, scheduleInput)
	}
	if err != nil {
		rollbackErr := s.mediaLibraries.DeleteLibrary(r.Context(), library.ID)
		if rollbackErr != nil {
			err = errors.Join(err, rollbackErr)
		}
		writeTaskError(w, err)
		return
	}
	writeData(w, http.StatusCreated, library)
}

func (s *Server) handleGetMediaLibrary(w http.ResponseWriter, r *http.Request, session domain.Session) {
	if !s.requireMediaLibraries(w) {
		return
	}
	id, ok := parseMediaID(w, r.PathValue("id"))
	if !ok {
		return
	}
	library, err := s.mediaLibraries.GetLibraryForUser(r.Context(), mediaUserAccess(session), id)
	if err != nil {
		writeMediaLibraryError(w, err)
		return
	}
	writeOK(w, projectMediaLibrary(session, library))
}

func (s *Server) handleUpdateMediaLibrary(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if !s.requireMediaLibraries(w) || !s.requireTaskManager(w) {
		return
	}
	id, ok := parseMediaID(w, r.PathValue("id"))
	if !ok {
		return
	}
	var input medialibrary.LibraryInput
	if !readJSON(w, r, &input) {
		return
	}
	library, err := s.mediaLibraries.UpdateLibrary(r.Context(), id, input)
	if err != nil {
		writeMediaLibraryError(w, err)
		return
	}
	schedule, err := s.findMediaLibraryScanSchedule(r.Context(), id)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	scheduleInput, err := newMediaScanTaskScheduleInput(library.ID, library.Name, schedule.CronExpression, schedule.Timezone, schedule.Enabled)
	if err == nil {
		_, err = s.tasks.UpdateSchedule(r.Context(), schedule.ID, scheduleInput)
	}
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeOK(w, library)
}

func (s *Server) handleGetMediaLibraryScanSchedule(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if !s.requireMediaLibraries(w) || !s.requireTaskManager(w) {
		return
	}
	id, ok := parseMediaID(w, r.PathValue("id"))
	if !ok {
		return
	}
	if _, err := s.mediaLibraries.GetLibrary(r.Context(), id); err != nil {
		writeMediaLibraryError(w, err)
		return
	}
	schedule, err := s.findMediaLibraryScanSchedule(r.Context(), id)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeOK(w, schedule)
}

func (s *Server) handleUpdateMediaLibraryScanSchedule(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if !s.requireMediaLibraries(w) || !s.requireTaskManager(w) {
		return
	}
	id, ok := parseMediaID(w, r.PathValue("id"))
	if !ok {
		return
	}
	if _, err := s.mediaLibraries.GetLibrary(r.Context(), id); err != nil {
		writeMediaLibraryError(w, err)
		return
	}
	var input mediaLibraryScanScheduleInput
	if !readJSON(w, r, &input) {
		return
	}
	schedule, err := s.findMediaLibraryScanSchedule(r.Context(), id)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	cronExpression := strings.TrimSpace(input.CronExpression)
	timezone := strings.TrimSpace(input.Timezone)
	if !input.Enabled && cronExpression == "" {
		cronExpression = schedule.CronExpression
		timezone = schedule.Timezone
	}
	if input.Enabled && (cronExpression == "" || timezone == "") {
		writeError(w, http.StatusBadRequest, "cron expression and timezone are required when scan scheduling is enabled")
		return
	}
	scheduleInput, err := newMediaScanTaskScheduleInput(id, schedule.Name, cronExpression, timezone, input.Enabled)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not prepare media scan schedule")
		return
	}
	updated, err := s.tasks.UpdateSchedule(r.Context(), schedule.ID, scheduleInput)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeOK(w, updated)
}

func (s *Server) findMediaLibraryScanSchedule(ctx context.Context, libraryID int64) (task.PublicSchedule, error) {
	page, err := s.tasks.ListSchedules(ctx, medialibrary.ScanTaskType)
	if err != nil {
		return task.PublicSchedule{}, err
	}
	var match task.PublicSchedule
	found := false
	for _, schedule := range page.Items {
		payload, marshalErr := json.Marshal(schedule.Parameters)
		if marshalErr != nil {
			return task.PublicSchedule{}, marshalErr
		}
		var scan medialibrary.ScanPayload
		if json.Unmarshal(payload, &scan) != nil || scan.LibraryID != libraryID {
			continue
		}
		if found {
			return task.PublicSchedule{}, task.ErrConflict
		}
		match, found = schedule, true
	}
	if !found {
		return task.PublicSchedule{}, task.ErrNotFound
	}
	return match, nil
}

func newMediaScanTaskScheduleInput(libraryID int64, name, cronExpression, timezone string, enabled bool) (task.ScheduleInput, error) {
	payload, err := json.Marshal(medialibrary.ScanPayload{LibraryID: libraryID})
	if err != nil {
		return task.ScheduleInput{}, err
	}
	return task.ScheduleInput{
		Name: name, Type: medialibrary.ScanTaskType, CronExpression: cronExpression,
		Timezone: timezone, Enabled: enabled, Payload: payload,
	}, nil
}

type mediaLibraryOrderRequest struct {
	LibraryIDs []int64 `json:"library_ids"`
}

func (s *Server) handleReorderMediaLibraries(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if !s.requireMediaLibraries(w) {
		return
	}
	var input mediaLibraryOrderRequest
	if !readJSON(w, r, &input) {
		return
	}
	if err := s.mediaLibraries.ReorderLibraries(r.Context(), input.LibraryIDs); err != nil {
		writeMediaLibraryError(w, err)
		return
	}
	writeOK(w, map[string]bool{"reordered": true})
}

func (s *Server) handleDeleteMediaLibrary(w http.ResponseWriter, r *http.Request, session domain.Session) {
	if !s.requireMediaLibraries(w) || !s.requireTaskManager(w) {
		return
	}
	id, ok := parseMediaID(w, r.PathValue("id"))
	if !ok {
		return
	}
	schedule, err := s.findMediaLibraryScanSchedule(r.Context(), id)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	if err := s.tasks.DeleteSchedule(r.Context(), schedule.ID); err != nil {
		writeTaskError(w, err)
		return
	}
	if err := s.mediaLibraries.DeleteLibrary(r.Context(), id); err != nil {
		restoreInput, inputErr := newMediaScanTaskScheduleInput(id, schedule.Name, schedule.CronExpression, schedule.Timezone, schedule.Enabled)
		var restoreErr error
		if inputErr == nil {
			_, restoreErr = s.tasks.CreateSchedule(r.Context(), session.Username, restoreInput)
		} else {
			restoreErr = inputErr
		}
		if restoreErr != nil {
			err = errors.Join(err, restoreErr)
		}
		writeMediaLibraryError(w, err)
		return
	}
	writeOK(w, map[string]bool{"deleted": true})
}

func (s *Server) handleScanMediaLibrary(w http.ResponseWriter, r *http.Request, session domain.Session) {
	s.queueMediaLibraryScan(w, r, session, nil)
}

func (s *Server) handleImportMediaLibrary(w http.ResponseWriter, r *http.Request, session domain.Session) {
	var input struct {
		Paths []string `json:"paths"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if len(input.Paths) == 0 {
		writeError(w, http.StatusBadRequest, "at least one import path is required")
		return
	}
	s.queueMediaLibraryScan(w, r, session, input.Paths)
}

func (s *Server) queueMediaLibraryScan(w http.ResponseWriter, r *http.Request, session domain.Session, paths []string) {
	if !s.requireMediaLibraries(w) || !s.requireTaskManager(w) {
		return
	}
	id, ok := parseMediaID(w, r.PathValue("id"))
	if !ok {
		return
	}
	if _, err := s.mediaLibraries.GetLibrary(r.Context(), id); err != nil {
		writeMediaLibraryError(w, err)
		return
	}
	payload, err := json.Marshal(medialibrary.ScanPayload{LibraryID: id, Paths: paths})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create media scan")
		return
	}
	run, err := s.tasks.CreateManualRun(r.Context(), session.Username, task.ManualRunInput{Type: medialibrary.ScanTaskType, Payload: payload})
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeData(w, http.StatusAccepted, run)
}

func (s *Server) handleListMediaLibraryItems(w http.ResponseWriter, r *http.Request, session domain.Session) {
	if !s.requireMediaLibraries(w) {
		return
	}
	query := medialibrary.ItemQuery{Search: r.URL.Query().Get("search")}
	if value := strings.TrimSpace(r.URL.Query().Get("library_id")); value != "" {
		id, ok := parseMediaID(w, value)
		if !ok {
			return
		}
		query.LibraryID = &id
	}
	if value, exists := r.URL.Query()["parent_id"]; exists {
		query.ParentSet = true
		if len(value) > 0 && value[0] != "" && value[0] != "root" {
			id, ok := parseMediaID(w, value[0])
			if !ok {
				return
			}
			query.ParentID = &id
		}
	}
	query.Recursive = parseBool(r.URL.Query().Get("recursive"))
	query.Latest = parseBool(r.URL.Query().Get("latest"))
	query.Resume = parseBool(r.URL.Query().Get("resume"))
	query.SortBy = strings.TrimSpace(r.URL.Query().Get("sort_by"))
	query.SortDescending = strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("sort_order")), "descending")
	query.Start = parseNonNegative(r.URL.Query().Get("start"), 0)
	query.Limit = parseNonNegative(r.URL.Query().Get("limit"), 100)
	if value := strings.TrimSpace(r.URL.Query().Get("person_id")); value != "" {
		id, ok := parseMediaID(w, value)
		if !ok {
			return
		}
		query.PersonIDs = []int64{id}
	}
	for _, value := range strings.Split(r.URL.Query().Get("kinds"), ",") {
		if value = strings.TrimSpace(value); value != "" {
			query.Kinds = append(query.Kinds, medialibrary.ItemKind(value))
		}
	}
	page, err := s.mediaLibraries.ListItemsForUser(r.Context(), mediaUserAccess(session), query)
	if err != nil {
		writeMediaLibraryError(w, err)
		return
	}
	for index := range page.Items {
		page.Items[index] = projectMediaItem(session, page.Items[index])
	}
	writeOK(w, page)
}

func (s *Server) handleContinueWatching(w http.ResponseWriter, r *http.Request, session domain.Session) {
	if !s.requireMediaLibraries(w) {
		return
	}
	page, err := s.mediaLibraries.ListContinueWatchingForUser(
		r.Context(), mediaUserAccess(session),
		parseNonNegative(r.URL.Query().Get("start"), 0),
		parseNonNegative(r.URL.Query().Get("limit"), 24),
	)
	if err != nil {
		writeMediaLibraryError(w, err)
		return
	}
	for index := range page.Items {
		page.Items[index].Item = projectMediaItem(session, page.Items[index].Item)
	}
	writeOK(w, page)
}

func (s *Server) handleGetMediaLibraryItem(w http.ResponseWriter, r *http.Request, session domain.Session) {
	if !s.requireMediaLibraries(w) {
		return
	}
	id, ok := parseMediaID(w, r.PathValue("id"))
	if !ok {
		return
	}
	item, err := s.mediaLibraries.GetItemForUser(r.Context(), mediaUserAccess(session), id)
	if err != nil {
		writeMediaLibraryError(w, err)
		return
	}
	writeOK(w, projectMediaItem(session, item))
}

func (s *Server) handleNextMediaLibraryEpisode(w http.ResponseWriter, r *http.Request, session domain.Session) {
	if !s.requireMediaLibraries(w) {
		return
	}
	id, ok := parseMediaID(w, r.PathValue("id"))
	if !ok {
		return
	}
	item, err := s.mediaLibraries.NextEpisodeForUser(r.Context(), mediaUserAccess(session), id)
	if err != nil {
		writeMediaLibraryError(w, err)
		return
	}
	if item.ID == 0 {
		writeOK(w, map[string]any{"item": nil})
		return
	}
	writeOK(w, map[string]any{"item": projectMediaItem(session, item)})
}

func (s *Server) handleListMediaLibraryItemPeople(w http.ResponseWriter, r *http.Request, session domain.Session) {
	if !s.requireMediaLibraries(w) {
		return
	}
	id, ok := parseMediaID(w, r.PathValue("id"))
	if !ok {
		return
	}
	people, err := s.mediaLibraries.ListItemPeopleForUser(
		r.Context(), mediaUserAccess(session), id, r.URL.Query().Get("language"),
	)
	if err != nil {
		writeMediaLibraryError(w, err)
		return
	}
	writeOK(w, map[string]any{"items": people, "total": len(people)})
}

func (s *Server) handleUpdateMediaLibraryItemMetadata(w http.ResponseWriter, r *http.Request, session domain.Session) {
	if !s.requireMediaLibraries(w) {
		return
	}
	id, ok := parseMediaID(w, r.PathValue("id"))
	if !ok {
		return
	}
	var raw json.RawMessage
	if !readJSON(w, r, &raw) {
		return
	}
	var input medialibrary.MetadataInput
	if err := json.Unmarshal(raw, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if _, exists := fields["metadata_binding"]; exists {
		writeError(w, http.StatusBadRequest, "episode movie bindings are no longer supported")
		return
	}

	item, err := s.mediaLibraries.UpdateItemMetadataForUser(r.Context(), mediaUserAccess(session), id, input)
	if err != nil {
		writeMediaLibraryError(w, err)
		return
	}
	writeOK(w, projectMediaItem(session, item))
}

type metadataRefreshRequest struct {
	TMDBID            *int64 `json:"tmdb_id,omitempty"`
	TVDBID            *int64 `json:"tvdb_id,omitempty"`
	MetadataSource    string `json:"metadata_source,omitempty"`
	ClassicShortOrder string `json:"classic_short_order,omitempty"`
}

func (s *Server) handleRefreshMediaLibraryItemMetadata(w http.ResponseWriter, r *http.Request, session domain.Session) {
	if !s.requireMediaLibraries(w) {
		return
	}
	id, ok := parseMediaID(w, r.PathValue("id"))
	if !ok {
		return
	}
	var request metadataRefreshRequest
	if !readJSON(w, r, &request) {
		return
	}
	if request.ClassicShortOrder != "" {
		writeError(w, http.StatusBadRequest, "classic short ordering is no longer supported")
		return
	}
	item, err := s.mediaLibraries.RefreshMetadataForUser(r.Context(), mediaUserAccess(session), id, medialibrary.RefreshMetadataInput{TMDBID: request.TMDBID, TVDBID: request.TVDBID, MetadataSource: request.MetadataSource})

	if err != nil {
		writeMediaLibraryError(w, err)
		return
	}
	writeOK(w, projectMediaItem(session, item))
}

func (s *Server) handleStreamMediaLibraryItem(w http.ResponseWriter, r *http.Request, session domain.Session) {
	if !s.requireMediaLibraries(w) {
		return
	}
	id, ok := parseMediaID(w, r.PathValue("id"))
	if !ok {
		return
	}
	target, err := s.mediaLibraries.ResolveDownloadURLForUser(r.Context(), mediaUserAccess(session), id, r.UserAgent())
	if err != nil {
		writeMediaLibraryError(w, err)
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

type playbackUpdateRequest struct {
	PositionTicks int64 `json:"position_ticks"`
	Stopped       bool  `json:"stopped"`
}

func (s *Server) handleUpdateMediaLibraryPlayback(w http.ResponseWriter, r *http.Request, session domain.Session) {
	if !s.requireMediaLibraries(w) {
		return
	}
	id, ok := parseMediaID(w, r.PathValue("id"))
	if !ok {
		return
	}
	var request playbackUpdateRequest
	if !readJSON(w, r, &request) {
		return
	}
	data, err := s.mediaLibraries.UpdatePlaybackForUser(r.Context(), mediaUserAccess(session), id, medialibrary.PlaybackUpdate{
		PositionTicks: request.PositionTicks,
		Stopped:       request.Stopped,
	})
	if err != nil {
		writeMediaLibraryError(w, err)
		return
	}
	writeOK(w, data)
}

type playedUpdateRequest struct {
	Played bool `json:"played"`
}

func (s *Server) handleSetMediaLibraryPlayed(w http.ResponseWriter, r *http.Request, session domain.Session) {
	if !s.requireMediaLibraries(w) {
		return
	}
	id, ok := parseMediaID(w, r.PathValue("id"))
	if !ok {
		return
	}
	var request playedUpdateRequest
	if !readJSON(w, r, &request) {
		return
	}
	data, err := s.mediaLibraries.SetPlayedForUser(r.Context(), mediaUserAccess(session), id, request.Played)
	if err != nil {
		writeMediaLibraryError(w, err)
		return
	}
	writeOK(w, data)
}

func (s *Server) handleClearMediaLibraryResume(w http.ResponseWriter, r *http.Request, session domain.Session) {
	if !s.requireMediaLibraries(w) {
		return
	}
	id, ok := parseMediaID(w, r.PathValue("id"))
	if !ok {
		return
	}
	data, err := s.mediaLibraries.ClearResumeForUser(r.Context(), mediaUserAccess(session), id)
	if err != nil {
		writeMediaLibraryError(w, err)
		return
	}
	writeOK(w, data)
}

func mediaUserAccess(session domain.Session) medialibrary.UserAccess {
	return medialibrary.UserAccess{
		UserID: session.UserID, IsAdmin: session.IsAdmin(), FilterVisibleLibraries: true,
	}
}

func projectMediaLibrary(session domain.Session, library medialibrary.Library) medialibrary.Library {
	if session.IsAdmin() {
		return library
	}
	library.RootPath = ""
	library.RootPaths = nil
	return library
}

func projectMediaItem(session domain.Session, item medialibrary.Item) medialibrary.Item {
	if session.IsAdmin() {
		return item
	}
	item.Path = ""
	item.MediaPath = ""
	item.ImageSourceURL = ""
	item.BackdropSourceURL = ""
	return item
}

func (s *Server) requireMediaLibraries(w http.ResponseWriter) bool {
	if s.mediaLibraries == nil {
		writeError(w, http.StatusNotFound, "not found")
		return false
	}
	return true
}

func writeMediaLibraryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "media library item not found")
	case errors.Is(err, medialibrary.ErrDisabled):
		writeError(w, http.StatusNotFound, "media library is disabled")
	case errors.Is(err, medialibrary.ErrInvalidLibrary):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, medialibrary.ErrLibraryEditForbidden):
		writeError(w, http.StatusForbidden, medialibrary.ErrLibraryEditForbidden.Error())
	case errors.Is(err, medialibrary.ErrVideoScreenshotUnavailable):
		writeError(w, http.StatusServiceUnavailable, medialibrary.ErrVideoScreenshotUnavailable.Error())
	case errors.Is(err, medialibrary.ErrVideoScreenshotFailed):
		writeError(w, http.StatusBadGateway, medialibrary.ErrVideoScreenshotFailed.Error())
	default:
		writeServiceError(w, err, http.StatusInternalServerError)
	}
}

func parseMediaID(w http.ResponseWriter, value string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "id must be a positive integer")
		return 0, false
	}
	return id, true
}

func parseBool(value string) bool {
	parsed, _ := strconv.ParseBool(value)
	return parsed
}

func parseNonNegative(value string, fallback int) int {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

func (s *Server) handleSetMediaLibraryResumeVisibility(w http.ResponseWriter, r *http.Request, session domain.Session) {
	if !s.requireMediaLibraries(w) {
		return
	}
	id, ok := parseMediaID(w, r.PathValue("id"))
	if !ok {
		return
	}
	var request struct {
		Hidden *bool `json:"hidden"`
	}
	if !readJSON(w, r, &request) {
		return
	}
	if request.Hidden == nil {
		writeError(w, http.StatusBadRequest, "hidden is required")
		return
	}
	data, err := s.mediaLibraries.SetHideFromResumeForUser(r.Context(), mediaUserAccess(session), id, *request.Hidden)
	if err != nil {
		writeMediaLibraryError(w, err)
		return
	}
	writeOK(w, data)
}
