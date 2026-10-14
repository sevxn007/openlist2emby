package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/task"
)

func TestRemovedFeatureEndpointsReturnJSONNotFound(t *testing.T) {
	server, store := newTaskHandlerTestServer(t)
	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/manager/sync/previews"},
		{http.MethodGet, "/api/manager/sync/previews/old-preview"},
		{http.MethodGet, "/api/manager/sync/previews/old-preview/items"},
		{http.MethodGet, "/api/manager/music/catalog/search?query=synthetic"},
		{http.MethodGet, "/api/manager/music/catalog/detail?source=netease"},
		{http.MethodGet, "/api/manager/music/catalog/stream?source=netease&id=synthetic"},
		{http.MethodPost, "/api/manager/music/catalog/upload"},
	}
	for _, endpoint := range endpoints {
		for _, authenticated := range []bool{false, true} {
			name := endpoint.method + " " + endpoint.path
			if authenticated {
				name += " authenticated"
			}
			t.Run(name, func(t *testing.T) {
				request := requestJSON(endpoint.method, endpoint.path, map[string]any{})
				if authenticated {
					request = taskRequestWithSession(t, store, endpoint.method, endpoint.path, map[string]any{})
				}
				recorder := httptest.NewRecorder()
				server.ServeHTTP(recorder, request)
				assertTaskErrorEnvelope(t, recorder, http.StatusNotFound, "not found")
			})
		}
	}
}

func TestRemovedTaskTypesCannotBeCreatedOrScheduled(t *testing.T) {
	for _, taskType := range []string{"files.sync", "music.catalog_upload"} {
		t.Run(taskType, func(t *testing.T) {
			server, store := newTaskHandlerTestServer(t)
			input := task.ScheduleInput{
				Name: "Removed task", Type: taskType, Enabled: true,
				CronExpression: "0 3 * * *", Timezone: "UTC", Payload: json.RawMessage(`{}`),
			}
			for _, path := range []string{"/api/manager/tasks/runs", "/api/manager/tasks/schedules"} {
				recorder := httptest.NewRecorder()
				server.ServeHTTP(recorder, taskRequestWithSession(t, store, http.MethodPost, path, input))
				assertTaskErrorEnvelope(t, recorder, http.StatusUnprocessableEntity, task.ErrTypeUnavailable.Error())
			}

			now := time.Date(2026, 7, 22, 8, 0, 0, 0, time.UTC)
			schedule, err := store.CreateSchedule(t.Context(), task.Schedule{
				Name: input.Name, Type: taskType, PayloadVersion: 1, Payload: input.Payload,
				CronExpression: input.CronExpression, Timezone: input.Timezone,
				CreatedBy: "local-admin", CreatedAt: now, UpdatedAt: now,
			})
			if err != nil {
				t.Fatal(err)
			}
			path := "/api/manager/tasks/schedules/" + jsonNumber(schedule.ID)
			for _, request := range []*http.Request{
				taskRequestWithSession(t, store, http.MethodPut, path, input),
				taskRequestWithSession(t, store, http.MethodPost, path+"/run", nil),
			} {
				recorder := httptest.NewRecorder()
				server.ServeHTTP(recorder, request)
				assertTaskErrorEnvelope(t, recorder, http.StatusUnprocessableEntity, task.ErrTypeUnavailable.Error())
			}
			page, err := store.ListRuns(t.Context(), task.RunFilter{Page: 1, PerPage: 20})
			if err != nil || page.Total != 0 {
				t.Fatalf("removed-type requests created runs: %#v, %v", page, err)
			}
			deleteRecorder := httptest.NewRecorder()
			server.ServeHTTP(deleteRecorder, taskRequestWithSession(t, store, http.MethodDelete, path, nil))
			if deleteRecorder.Code != http.StatusOK {
				t.Fatalf("delete old schedule: status = %d, body = %s", deleteRecorder.Code, deleteRecorder.Body.String())
			}
			if _, err := store.GetSchedule(t.Context(), schedule.ID); !errors.Is(err, task.ErrNotFound) {
				t.Fatalf("GetSchedule(deleted) error = %v", err)
			}
		})
	}
}

func TestRemovedTaskHistoryRemainsReadableButCannotBeRetried(t *testing.T) {
	for _, taskType := range []string{"files.sync", "music.catalog_upload"} {
		for _, status := range []task.Status{task.StatusSucceeded, task.StatusFailed, task.StatusCanceled} {
			t.Run(taskType+"/"+string(status), func(t *testing.T) {
				server, store := newTaskHandlerTestServer(t)
				now := time.Date(2026, 7, 22, 8, 0, 0, 0, time.UTC)
				run, err := store.CreateRun(t.Context(), task.NewRun{
					Type: taskType, PayloadVersion: 1, Payload: json.RawMessage(`{}`),
					Trigger: task.TriggerManual, AvailableAt: now, CreatedBy: "local-admin",
				})
				if err != nil {
					t.Fatal(err)
				}
				if status == task.StatusCanceled {
					_, err = store.CancelRun(t.Context(), run.ID, now)
				} else {
					claimed, claimErr := store.ClaimRun(t.Context(), "history-fixture", now, now.Add(time.Minute))
					if claimErr != nil || claimed == nil || claimed.ID != run.ID {
						t.Fatalf("ClaimRun() = %#v, %v", claimed, claimErr)
					}
					if status == task.StatusSucceeded {
						err = store.CompleteRun(t.Context(), run.ID, "history-fixture", []byte(`{"completed":true}`), now)
					} else {
						err = store.FailRun(t.Context(), run.ID, "history-fixture", "synthetic_failure", "Synthetic failure", http.StatusBadGateway, now)
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				path := "/api/manager/tasks/runs/" + jsonNumber(run.ID)
				detailRecorder := httptest.NewRecorder()
				server.ServeHTTP(detailRecorder, taskRequestWithSession(t, store, http.MethodGet, path, nil))
				if detailRecorder.Code != http.StatusOK {
					t.Fatalf("history detail: status = %d, body = %s", detailRecorder.Code, detailRecorder.Body.String())
				}
				history := decodeTaskData[task.PublicRun](t, detailRecorder)
				if history.ID != run.ID || history.Type != taskType || history.Status != status || history.Subject != nil {
					t.Fatalf("history detail = %#v", history)
				}
				if status == task.StatusSucceeded && history.Result == nil {
					t.Fatal("completed historical result is missing")
				}
				retryRecorder := httptest.NewRecorder()
				server.ServeHTTP(retryRecorder, taskRequestWithSession(t, store, http.MethodPost, path+"/retry", nil))
				if status == task.StatusSucceeded {
					assertTaskErrorEnvelope(t, retryRecorder, http.StatusConflict, task.ErrConflict.Error())
				} else {
					assertTaskErrorEnvelope(t, retryRecorder, http.StatusUnprocessableEntity, task.ErrTypeUnavailable.Error())
				}
				listRecorder := httptest.NewRecorder()
				server.ServeHTTP(listRecorder, taskRequestWithSession(t, store, http.MethodGet, "/api/manager/tasks/runs?type="+taskType, nil))
				if listRecorder.Code != http.StatusOK {
					t.Fatalf("history list: status = %d, body = %s", listRecorder.Code, listRecorder.Body.String())
				}
				page := decodeTaskData[task.PublicRunPage](t, listRecorder)
				if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != run.ID || page.Items[0].Status != status {
					t.Fatalf("history was changed or retried: %#v", page)
				}
			})
		}
	}
}

func assertTaskErrorEnvelope(t *testing.T, recorder *httptest.ResponseRecorder, status int, message string) {
	t.Helper()
	if recorder.Code != status || recorder.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("response status = %d, content type = %q, body = %s", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
	}
	var response envelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code != status || response.Message != message || response.Data != nil {
		t.Fatalf("error envelope = %#v", response)
	}
}
