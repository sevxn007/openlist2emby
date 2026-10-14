package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	glebarezsqlite "github.com/glebarez/sqlite"
	"github.com/sevxn007/openlist2emby/backend/internal/app"
	"github.com/sevxn007/openlist2emby/backend/internal/audio/tagger"
	musicclient "github.com/sevxn007/openlist2emby/backend/internal/client/music"
	"github.com/sevxn007/openlist2emby/backend/internal/client/openlist"
	tmdbclient "github.com/sevxn007/openlist2emby/backend/internal/client/tmdb"
	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
	"github.com/sevxn007/openlist2emby/backend/internal/repository/sqlite"
	"github.com/sevxn007/openlist2emby/backend/internal/task"
	"github.com/sevxn007/openlist2emby/backend/internal/testidentity"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTaskHandlerTestServer(t *testing.T) (*Server, *sqlite.Store) {
	t.Helper()

	db, err := gorm.Open(glebarezsqlite.Open(filepath.Join(t.TempDir(), "task-handler-test.db")), &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Silent),
		TranslateError: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 22, 8, 0, 0, 0, time.UTC)
	store := sqlite.NewStoreWithClock(db, func() time.Time { return now })
	if err := store.InitializeSchema(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	application := app.NewService(
		store,
		openlist.NewClient(nil),
		tmdbclient.NewService(),
		musicclient.NewClient(nil),
		tagger.New(),
	)
	application.SetIdentityClient(&testidentity.Client{Store: store})
	registry, err := task.NewRegistry(
		app.NewMusicEmbedTaskHandler(application),
		medialibrary.NewTaskHandler(store, nil, nil),
		medialibrary.NewStorageMigrationHandler(store, nil),
	)
	if err != nil {
		t.Fatal(err)
	}
	options := task.DefaultOptions()
	options.Now = func() time.Time { return now }
	manager, err := task.NewManager(store, registry, options)
	if err != nil {
		t.Fatal(err)
	}

	// Keep the manager stopped so queued-run cancellation and retry are deterministic.
	return NewServer(application, "", manager), store
}

func taskRequestWithSession(
	t *testing.T,
	store *sqlite.Store,
	method string,
	path string,
	body any,
) *http.Request {
	t.Helper()
	session, err := createTestSession(t, store, t.Context(), "local-admin", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	request := requestJSON(method, path, body)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
	return request
}

func decodeTaskData[T any](t *testing.T, recorder *httptest.ResponseRecorder) T {
	t.Helper()
	var response struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    T      `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode task response: %v\nbody: %s", err, recorder.Body.String())
	}
	return response.Data
}

func TestTaskEndpointsRequireLocalSession(t *testing.T) {
	server, _ := newTaskHandlerTestServer(t)
	tests := []struct {
		method string
		path   string
		body   any
	}{
		{method: http.MethodGet, path: "/api/manager/tasks/types"},
		{method: http.MethodGet, path: "/api/manager/tasks/schedules"},
		{method: http.MethodPost, path: "/api/manager/tasks/schedules", body: map[string]any{}},
		{method: http.MethodGet, path: "/api/manager/tasks/schedules/1"},
		{method: http.MethodPut, path: "/api/manager/tasks/schedules/1", body: map[string]any{}},
		{method: http.MethodDelete, path: "/api/manager/tasks/schedules/1"},
		{method: http.MethodPost, path: "/api/manager/tasks/schedules/1/run", body: map[string]any{}},
		{method: http.MethodGet, path: "/api/manager/tasks/runs"},
		{method: http.MethodPost, path: "/api/manager/tasks/runs", body: map[string]any{}},
		{method: http.MethodGet, path: "/api/manager/tasks/runs/1"},
		{method: http.MethodDelete, path: "/api/manager/tasks/runs/1"},
		{method: http.MethodDelete, path: "/api/manager/tasks/runs/completed"},
		{method: http.MethodGet, path: "/api/manager/tasks/runs/1/items"},
		{method: http.MethodPost, path: "/api/manager/tasks/runs/1/cancel", body: map[string]any{}},
		{method: http.MethodPost, path: "/api/manager/tasks/runs/1/retry", body: map[string]any{}},
	}

	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, requestJSON(test.method, test.path, test.body))

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestLocalSessionCanListTaskTypesAndRuns(t *testing.T) {
	server, store := newTaskHandlerTestServer(t)

	typesRecorder := httptest.NewRecorder()
	server.ServeHTTP(typesRecorder, taskRequestWithSession(
		t,
		store,
		http.MethodGet,
		"/api/manager/tasks/types",
		nil,
	))
	if typesRecorder.Code != http.StatusOK {
		t.Fatalf("types status = %d body = %s", typesRecorder.Code, typesRecorder.Body.String())
	}
	typesData := decodeTaskData[struct {
		Items []task.Descriptor `json:"items"`
	}](t, typesRecorder)
	wantTypes := map[string]task.Descriptor{
		app.MusicEmbedTaskType: {
			Type: app.MusicEmbedTaskType, PayloadVersion: 1, Manual: true, Cancelable: true,
		},
		medialibrary.ScanTaskType: {
			Type: medialibrary.ScanTaskType, PayloadVersion: 1, Manual: true, Schedulable: true, Cancelable: true,
		},
		medialibrary.StorageMigrationTaskType: {
			Type: medialibrary.StorageMigrationTaskType, PayloadVersion: 1, Manual: true, Cancelable: true,
		},
	}
	if len(typesData.Items) != len(wantTypes) {
		t.Fatalf("task types = %#v", typesData.Items)
	}
	for _, descriptor := range typesData.Items {
		if want, ok := wantTypes[descriptor.Type]; !ok || descriptor != want {
			t.Fatalf("unexpected task descriptor = %#v", descriptor)
		}
	}

	runsRecorder := httptest.NewRecorder()
	server.ServeHTTP(runsRecorder, taskRequestWithSession(
		t,
		store,
		http.MethodGet,
		"/api/manager/tasks/runs",
		nil,
	))
	if runsRecorder.Code != http.StatusOK {
		t.Fatalf("runs status = %d body = %s", runsRecorder.Code, runsRecorder.Body.String())
	}
	runs := decodeTaskData[task.PublicRunPage](t, runsRecorder)
	if runs.Total != 0 || len(runs.Items) != 0 || runs.Page != 1 {
		t.Fatalf("runs = %#v", runs)
	}
}

func TestMusicTaskRunCreateListDetailCancelAndRetry(t *testing.T) {
	server, store := newTaskHandlerTestServer(t)
	const (
		openListBaseURL = "http://synthetic.openlist.local"
		adminKey        = "synthetic-openlist-admin-key"
	)
	if err := store.SaveInitialSettings(t.Context(), openListBaseURL, adminKey); err != nil {
		t.Fatal(err)
	}

	payload := json.RawMessage(`{
		"path":"/Music/synthetic-track.mp3",
		"source":"netease",
		"picture_id":"synthetic-picture",
		"lyric_id":"synthetic-lyric",
		"embed_cover":true,
		"embed_lyrics":true
	}`)
	createRecorder := httptest.NewRecorder()
	server.ServeHTTP(createRecorder, taskRequestWithSession(
		t,
		store,
		http.MethodPost,
		"/api/manager/tasks/runs",
		task.ManualRunInput{Type: app.MusicEmbedTaskType, Payload: payload},
	))
	if createRecorder.Code != http.StatusAccepted {
		t.Fatalf("create status = %d body = %s", createRecorder.Code, createRecorder.Body.String())
	}
	if bytes.Contains(createRecorder.Body.Bytes(), []byte(openListBaseURL)) ||
		bytes.Contains(createRecorder.Body.Bytes(), []byte(adminKey)) ||
		bytes.Contains(createRecorder.Body.Bytes(), []byte("openlist_base_url")) {
		t.Fatalf("create response exposed task credentials: %s", createRecorder.Body.String())
	}
	created := decodeTaskData[task.PublicRun](t, createRecorder)
	if created.ID <= 0 || created.Type != app.MusicEmbedTaskType || created.Status != task.StatusQueued {
		t.Fatalf("created run = %#v", created)
	}
	assertMusicTaskSubject(t, created.Subject)

	listRecorder := httptest.NewRecorder()
	server.ServeHTTP(listRecorder, taskRequestWithSession(
		t,
		store,
		http.MethodGet,
		"/api/manager/tasks/runs?status=queued&type=music.embed_metadata&page=1&per_page=10",
		nil,
	))
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d body = %s", listRecorder.Code, listRecorder.Body.String())
	}
	page := decodeTaskData[task.PublicRunPage](t, listRecorder)
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != created.ID {
		t.Fatalf("run page = %#v", page)
	}
	assertMusicTaskSubject(t, page.Items[0].Subject)

	detailRecorder := httptest.NewRecorder()
	server.ServeHTTP(detailRecorder, taskRequestWithSession(
		t,
		store,
		http.MethodGet,
		"/api/manager/tasks/runs/"+jsonNumber(created.ID),
		nil,
	))
	if detailRecorder.Code != http.StatusOK {
		t.Fatalf("detail status = %d body = %s", detailRecorder.Code, detailRecorder.Body.String())
	}
	detail := decodeTaskData[task.PublicRun](t, detailRecorder)
	if detail.ID != created.ID || detail.Type != app.MusicEmbedTaskType {
		t.Fatalf("run detail = %#v", detail)
	}
	assertMusicTaskSubject(t, detail.Subject)

	cancelRecorder := httptest.NewRecorder()
	server.ServeHTTP(cancelRecorder, taskRequestWithSession(
		t,
		store,
		http.MethodPost,
		"/api/manager/tasks/runs/"+jsonNumber(created.ID)+"/cancel",
		map[string]any{},
	))
	if cancelRecorder.Code != http.StatusOK {
		t.Fatalf("cancel status = %d body = %s", cancelRecorder.Code, cancelRecorder.Body.String())
	}
	canceled := decodeTaskData[task.PublicRun](t, cancelRecorder)
	if canceled.Status != task.StatusCanceled {
		t.Fatalf("canceled run = %#v", canceled)
	}

	retryRecorder := httptest.NewRecorder()
	server.ServeHTTP(retryRecorder, taskRequestWithSession(
		t,
		store,
		http.MethodPost,
		"/api/manager/tasks/runs/"+jsonNumber(created.ID)+"/retry",
		map[string]any{},
	))
	if retryRecorder.Code != http.StatusAccepted {
		t.Fatalf("retry status = %d body = %s", retryRecorder.Code, retryRecorder.Body.String())
	}
	retried := decodeTaskData[task.PublicRun](t, retryRecorder)
	if retried.ID == created.ID || retried.Status != task.StatusQueued || retried.Trigger != task.TriggerRetry {
		t.Fatalf("retried run = %#v", retried)
	}
	if retried.RetryOfRunID == nil || *retried.RetryOfRunID != created.ID {
		t.Fatalf("retry_of_run_id = %#v", retried.RetryOfRunID)
	}
	assertMusicTaskSubject(t, retried.Subject)

	queuedDeleteRecorder := httptest.NewRecorder()
	server.ServeHTTP(queuedDeleteRecorder, taskRequestWithSession(
		t,
		store,
		http.MethodDelete,
		"/api/manager/tasks/runs/"+jsonNumber(retried.ID),
		nil,
	))
	if queuedDeleteRecorder.Code != http.StatusOK {
		t.Fatalf("queued delete status = %d body = %s", queuedDeleteRecorder.Code, queuedDeleteRecorder.Body.String())
	}
	if _, err := store.GetRun(t.Context(), retried.ID); !errors.Is(err, task.ErrNotFound) {
		t.Fatalf("GetRun(deleted queued) error = %v, want ErrNotFound", err)
	}

	now := time.Date(2026, 7, 22, 8, 0, 0, 0, time.UTC)
	running, err := store.CreateRun(t.Context(), task.NewRun{
		Type:           "fixture.running",
		PayloadVersion: 1,
		Payload:        json.RawMessage(`{}`),
		Trigger:        task.TriggerManual,
		AvailableAt:    now,
		CreatedBy:      "local-admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimRun(t.Context(), "worker-delete", now, now.Add(time.Minute))
	if err != nil || claimed == nil || claimed.ID != running.ID {
		t.Fatalf("ClaimRun() = %#v, %v", claimed, err)
	}
	runningDeleteRecorder := httptest.NewRecorder()
	server.ServeHTTP(runningDeleteRecorder, taskRequestWithSession(
		t,
		store,
		http.MethodDelete,
		"/api/manager/tasks/runs/"+jsonNumber(running.ID),
		nil,
	))
	if runningDeleteRecorder.Code != http.StatusConflict {
		t.Fatalf("running delete status = %d body = %s", runningDeleteRecorder.Code, runningDeleteRecorder.Body.String())
	}

	deleteRecorder := httptest.NewRecorder()
	server.ServeHTTP(deleteRecorder, taskRequestWithSession(
		t,
		store,
		http.MethodDelete,
		"/api/manager/tasks/runs/"+jsonNumber(created.ID),
		nil,
	))
	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("delete status = %d body = %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
	deleted := decodeTaskData[map[string]bool](t, deleteRecorder)
	if !deleted["deleted"] {
		t.Fatalf("delete response = %#v", deleted)
	}
	if _, err := store.GetRun(t.Context(), created.ID); !errors.Is(err, task.ErrNotFound) {
		t.Fatalf("GetRun(deleted) error = %v, want ErrNotFound", err)
	}
}

func TestListTaskRunItemsFiltersPaginatesAndSummarizes(t *testing.T) {
	server, store := newTaskHandlerTestServer(t)
	now := time.Date(2026, 7, 22, 8, 0, 0, 0, time.UTC)
	run, err := store.CreateRun(t.Context(), task.NewRun{
		Type:           "fixture.items",
		PayloadVersion: 1,
		Payload:        json.RawMessage(`{}`),
		Trigger:        task.TriggerManual,
		AvailableAt:    now,
		CreatedBy:      "local-admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimRun(t.Context(), "worker-items", now, now.Add(time.Minute))
	if err != nil || claimed == nil || claimed.ID != run.ID {
		t.Fatalf("ClaimRun() = %#v, %v", claimed, err)
	}
	_, err = store.CreateRunItems(t.Context(), run.ID, "worker-items", []task.NewRunItem{
		{
			Position:        1,
			RelativePath:    "present.mkv",
			ObjectKind:      task.RunItemObjectFile,
			Action:          task.RunItemActionNone,
			SourcePath:      "/A/present.mkv",
			DestinationPath: "/B/present.mkv",
			Size:            200,
			Status:          task.RunItemStatusSkipped,
		},
		{
			Position:        0,
			RelativePath:    "missing.mkv",
			ObjectKind:      task.RunItemObjectFile,
			Action:          task.RunItemActionCopy,
			SourcePath:      "/A/missing.mkv",
			DestinationPath: "/B/missing.mkv",
			Size:            100,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, taskRequestWithSession(
		t,
		store,
		http.MethodGet,
		"/api/manager/tasks/runs/"+jsonNumber(run.ID)+"/items?status=pending&action=copy&page=1&per_page=1",
		nil,
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("items status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	page := decodeTaskData[taskRunItemPageResponse](t, recorder)
	if page.Total != 1 || page.Page != 1 || page.PerPage != 1 || len(page.Items) != 1 {
		t.Fatalf("item page = %#v", page)
	}
	item := page.Items[0]
	if item.Sequence != 0 ||
		item.RelativePath != "missing.mkv" ||
		item.ObjectType != task.RunItemObjectFile ||
		item.Action != task.RunItemActionCopy ||
		item.Percent != 0 ||
		item.TargetPath != "/B/missing.mkv" {
		t.Fatalf("item = %#v", item)
	}
	if page.Summary.Statuses[task.RunItemStatusPending] != 1 ||
		page.Summary.Statuses[task.RunItemStatusSkipped] != 1 ||
		page.Summary.Actions[task.RunItemActionCopy] != 1 ||
		page.Summary.Actions[task.RunItemActionNone] != 1 {
		t.Fatalf("summary = %#v", page.Summary)
	}
}

func TestListTaskRunItemsValidatesFiltersAndRun(t *testing.T) {
	server, store := newTaskHandlerTestServer(t)
	tests := []struct {
		name   string
		path   string
		status int
	}{
		{name: "invalid status", path: "/api/manager/tasks/runs/1/items?status=unknown", status: http.StatusBadRequest},
		{name: "invalid action", path: "/api/manager/tasks/runs/1/items?action=unknown", status: http.StatusBadRequest},
		{name: "invalid page", path: "/api/manager/tasks/runs/1/items?page=0", status: http.StatusBadRequest},
		{name: "missing run", path: "/api/manager/tasks/runs/999/items", status: http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, taskRequestWithSession(t, store, http.MethodGet, test.path, nil))
			if recorder.Code != test.status {
				t.Fatalf("status = %d body = %s, want %d", recorder.Code, recorder.Body.String(), test.status)
			}
		})
	}
}

func TestCreateScheduleRejectsNonSchedulableTaskType(t *testing.T) {
	server, store := newTaskHandlerTestServer(t)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, taskRequestWithSession(
		t,
		store,
		http.MethodPost,
		"/api/manager/tasks/schedules",
		task.ScheduleInput{
			Name:           "Synthetic music schedule",
			Type:           app.MusicEmbedTaskType,
			CronExpression: "0 * * * *",
			Timezone:       "UTC",
			Enabled:        true,
			Payload:        json.RawMessage(`{"path":"/Music/synthetic-track.mp3"}`),
		},
	))

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("schedule status = %d body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestManualTaskDefinitionCanBeSavedAndRunNow(t *testing.T) {
	server, store := newTaskHandlerTestServer(t)
	const (
		openListBaseURL = "http://synthetic.openlist.local"
		adminKey        = "synthetic-openlist-admin-key"
	)
	if err := store.SaveInitialSettings(t.Context(), openListBaseURL, adminKey); err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{
		"path":"/Music/synthetic-track.mp3",
		"source":"netease",
		"picture_id":"synthetic-picture",
		"lyric_id":"synthetic-lyric",
		"embed_cover":true,
		"embed_lyrics":true
	}`)
	createRecorder := httptest.NewRecorder()
	server.ServeHTTP(createRecorder, taskRequestWithSession(
		t,
		store,
		http.MethodPost,
		"/api/manager/tasks/schedules",
		task.ScheduleInput{
			Name:    "On-demand music task",
			Type:    app.MusicEmbedTaskType,
			Enabled: true,
			Payload: payload,
		},
	))
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d body = %s", createRecorder.Code, createRecorder.Body.String())
	}
	if bytes.Contains(createRecorder.Body.Bytes(), []byte(openListBaseURL)) ||
		bytes.Contains(createRecorder.Body.Bytes(), []byte(adminKey)) {
		t.Fatalf("create response exposed task credentials: %s", createRecorder.Body.String())
	}
	definition := decodeTaskData[task.PublicSchedule](t, createRecorder)
	if definition.CronExpression != "" || definition.Timezone != "" || definition.Enabled || definition.NextRunAt != nil {
		t.Fatalf("manual definition = %#v", definition)
	}

	runRecorder := httptest.NewRecorder()
	server.ServeHTTP(runRecorder, taskRequestWithSession(
		t,
		store,
		http.MethodPost,
		"/api/manager/tasks/schedules/"+jsonNumber(definition.ID)+"/run",
		map[string]any{},
	))
	if runRecorder.Code != http.StatusAccepted {
		t.Fatalf("run status = %d body = %s", runRecorder.Code, runRecorder.Body.String())
	}
	run := decodeTaskData[task.PublicRun](t, runRecorder)
	if run.ScheduleID == nil || *run.ScheduleID != definition.ID || run.Trigger != task.TriggerRunNow {
		t.Fatalf("run = %#v", run)
	}
}

func assertMusicTaskSubject(t *testing.T, subject any) {
	t.Helper()
	value, ok := subject.(map[string]any)
	if !ok {
		t.Fatalf("music task subject = %#v", subject)
	}
	if value["path"] != "/Music/synthetic-track.mp3" || value["embed_cover"] != true || value["embed_lyrics"] != true {
		t.Fatalf("music task subject = %#v", value)
	}
	if _, exists := value["openlist_base_url"]; exists {
		t.Fatalf("music task subject exposed OpenList URL: %#v", value)
	}
	if _, exists := value["openlist_admin_key"]; exists {
		t.Fatalf("music task subject exposed OpenList key: %#v", value)
	}
}

func jsonNumber(value int64) string {
	return strconv.FormatInt(value, 10)
}

func TestDeleteCompletedTaskRunsRequiresAdmin(t *testing.T) {
	server, store := newTaskHandlerTestServer(t)
	request := taskRequestWithSession(t, store, http.MethodDelete, "/api/manager/tasks/runs/completed", nil)
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Movies", ContentType: medialibrary.ContentMovies, RootPath: "/Movies",
	})
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.CreateUser(t.Context(), domain.LocalUser{
		OpenListUserID: 102, InstanceID: "test-instance", Username: "media-viewer",
		Role: domain.LocalUserRoleMedia, Enabled: true,
		LibraryGrants: []domain.LibraryGrant{{LibraryID: library.ID, Access: domain.LibraryAccessView}},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := testidentity.Session(t.Context(), store, user.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	request = requestJSON(http.MethodDelete, "/api/manager/tasks/runs/completed", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestDeleteCompletedTaskRunsClearsAllPagesAndTypes(t *testing.T) {
	server, store := newTaskHandlerTestServer(t)
	now := time.Date(2026, 7, 22, 8, 0, 0, 0, time.UTC)
	for i := range 25 {
		run, err := store.CreateRun(t.Context(), task.NewRun{
			Type: "fixture.type." + strconv.Itoa(i%2), PayloadVersion: 1, Payload: json.RawMessage(`{}`),
			Trigger: task.TriggerManual, AvailableAt: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		if i%2 == 0 {
			claimed, err := store.ClaimRun(t.Context(), "worker-clear", now, now.Add(time.Minute))
			if err != nil || claimed == nil || claimed.ID != run.ID {
				t.Fatalf("ClaimRun() = %#v, %v", claimed, err)
			}
			if err := store.CompleteRun(t.Context(), run.ID, "worker-clear", []byte(`{}`), now); err != nil {
				t.Fatal(err)
			}
		} else if _, err := store.CancelRun(t.Context(), run.ID, now); err != nil {
			t.Fatal(err)
		}
	}
	queued, err := store.CreateRun(t.Context(), task.NewRun{
		Type: "fixture.retained", PayloadVersion: 1, Payload: json.RawMessage(`{}`), Trigger: task.TriggerManual, AvailableAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []int{25, 0} {
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, taskRequestWithSession(t, store, http.MethodDelete,
			"/api/manager/tasks/runs/completed?page=2&type=fixture.type.0&status=running", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
		}
		result := decodeTaskData[struct {
			Deleted int `json:"deleted"`
		}](t, recorder)
		if result.Deleted != want {
			t.Fatalf("deleted = %d, want %d", result.Deleted, want)
		}
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, taskRequestWithSession(t, store, http.MethodGet, "/api/manager/tasks/runs", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("list status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	page := decodeTaskData[task.PublicRunPage](t, recorder)
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != queued.ID || page.Items[0].Status != task.StatusQueued {
		t.Fatalf("remaining page = %#v", page)
	}
}
