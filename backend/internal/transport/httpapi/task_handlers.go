package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/task"
)

func (s *Server) handleTaskTypes(w http.ResponseWriter, _ *http.Request, _ domain.Session) {
	if !s.requireTaskManager(w) {
		return
	}
	writeOK(w, map[string]any{"items": s.tasks.Types()})
}

func (s *Server) handleListTaskSchedules(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if !s.requireTaskManager(w) {
		return
	}
	page, err := s.tasks.ListSchedules(r.Context(), r.URL.Query().Get("type"))
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeOK(w, page)
}

func (s *Server) handleCreateTaskSchedule(w http.ResponseWriter, r *http.Request, session domain.Session) {
	if !s.requireTaskManager(w) {
		return
	}
	var input task.ScheduleInput
	if !readJSON(w, r, &input) {
		return
	}
	schedule, err := s.tasks.CreateSchedule(r.Context(), session.Username, input)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeData(w, http.StatusCreated, schedule)
}

func (s *Server) handleGetTaskSchedule(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if !s.requireTaskManager(w) {
		return
	}
	id, ok := readTaskID(w, r)
	if !ok {
		return
	}
	schedule, err := s.tasks.GetSchedule(r.Context(), id)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeOK(w, schedule)
}

func (s *Server) handleUpdateTaskSchedule(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if !s.requireTaskManager(w) {
		return
	}
	id, ok := readTaskID(w, r)
	if !ok {
		return
	}
	var input task.ScheduleInput
	if !readJSON(w, r, &input) {
		return
	}
	schedule, err := s.tasks.UpdateSchedule(r.Context(), id, input)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeOK(w, schedule)
}

func (s *Server) handleDeleteTaskSchedule(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if !s.requireTaskManager(w) {
		return
	}
	id, ok := readTaskID(w, r)
	if !ok {
		return
	}
	if err := s.tasks.DeleteSchedule(r.Context(), id); err != nil {
		writeTaskError(w, err)
		return
	}
	writeOK(w, map[string]bool{"deleted": true})
}

func (s *Server) handleRunTaskSchedule(w http.ResponseWriter, r *http.Request, session domain.Session) {
	if !s.requireTaskManager(w) {
		return
	}
	id, ok := readTaskID(w, r)
	if !ok {
		return
	}
	run, err := s.tasks.RunScheduleNow(r.Context(), id, session.Username)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeData(w, http.StatusAccepted, run)
}

func (s *Server) handleListTaskRuns(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if !s.requireTaskManager(w) {
		return
	}
	statuses, err := task.ParseStatuses(r.URL.Query().Get("status"))
	if err != nil {
		writeTaskError(w, err)
		return
	}
	page, err := optionalPositiveInt(r.URL.Query().Get("page"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "page must be a positive integer")
		return
	}
	perPage, err := optionalPositiveInt(r.URL.Query().Get("per_page"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "per_page must be a positive integer")
		return
	}
	result, err := s.tasks.ListRuns(r.Context(), task.RunFilter{
		Statuses: statuses,
		Type:     strings.TrimSpace(r.URL.Query().Get("type")),
		Page:     page,
		PerPage:  perPage,
	})
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeOK(w, result)
}

func (s *Server) handleCreateTaskRun(w http.ResponseWriter, r *http.Request, session domain.Session) {
	if !s.requireTaskManager(w) {
		return
	}
	var input task.ManualRunInput
	if !readJSON(w, r, &input) {
		return
	}
	run, err := s.tasks.CreateManualRun(r.Context(), session.Username, input)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeData(w, http.StatusAccepted, run)
}

func (s *Server) handleGetTaskRun(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if !s.requireTaskManager(w) {
		return
	}
	id, ok := readTaskID(w, r)
	if !ok {
		return
	}
	run, err := s.tasks.GetRun(r.Context(), id)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeOK(w, run)
}

func (s *Server) handleDeleteTaskRun(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if !s.requireTaskManager(w) {
		return
	}
	id, ok := readTaskID(w, r)
	if !ok {
		return
	}
	if err := s.tasks.DeleteRun(r.Context(), id); err != nil {
		writeTaskError(w, err)
		return
	}
	writeOK(w, map[string]bool{"deleted": true})
}

func (s *Server) handleDeleteCompletedTaskRuns(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if !s.requireTaskManager(w) {
		return
	}
	deleted, err := s.tasks.DeleteCompletedRuns(r.Context())
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeOK(w, map[string]int{"deleted": deleted})
}

func (s *Server) handleListTaskRunItems(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if !s.requireTaskManager(w) {
		return
	}
	id, ok := readTaskID(w, r)
	if !ok {
		return
	}
	statuses, err := task.ParseRunItemStatuses(r.URL.Query().Get("status"))
	if err != nil {
		writeTaskError(w, err)
		return
	}
	actions, err := task.ParseRunItemActions(r.URL.Query().Get("action"))
	if err != nil {
		writeTaskError(w, err)
		return
	}
	page, err := optionalPositiveInt(r.URL.Query().Get("page"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "page must be a positive integer")
		return
	}
	perPage, err := optionalPositiveInt(r.URL.Query().Get("per_page"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "per_page must be a positive integer")
		return
	}
	result, err := s.tasks.ListRunItems(r.Context(), id, task.RunItemFilter{
		Statuses: statuses,
		Actions:  actions,
		Page:     page,
		PerPage:  perPage,
	})
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeOK(w, publicTaskRunItemPage(result))
}

func (s *Server) handleCancelTaskRun(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if !s.requireTaskManager(w) {
		return
	}
	id, ok := readTaskID(w, r)
	if !ok {
		return
	}
	run, err := s.tasks.CancelRun(r.Context(), id)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeOK(w, run)
}

func (s *Server) handleRetryTaskRun(w http.ResponseWriter, r *http.Request, session domain.Session) {
	if !s.requireTaskManager(w) {
		return
	}
	id, ok := readTaskID(w, r)
	if !ok {
		return
	}
	run, err := s.tasks.RetryRun(r.Context(), id, session.Username)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeData(w, http.StatusAccepted, run)
}

func (s *Server) requireTaskManager(w http.ResponseWriter) bool {
	if s.tasks != nil {
		return true
	}
	writeError(w, http.StatusServiceUnavailable, "task manager is unavailable")
	return false
}

func readTaskID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "task id must be a positive integer")
		return 0, false
	}
	return id, true
}

func optionalPositiveInt(value string) (int, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, errors.New("not a positive integer")
	}
	return parsed, nil
}

func writeTaskError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, task.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, task.ErrConflict):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, task.ErrRunActive):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, task.ErrTypeUnavailable),
		errors.Is(err, task.ErrNotManual),
		errors.Is(err, task.ErrNotSchedulable),
		errors.Is(err, task.ErrNotCancelable):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		writeServiceError(w, err, http.StatusInternalServerError)
	}
}
