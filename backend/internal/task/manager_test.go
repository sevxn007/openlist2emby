package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type fakeHandler struct {
	descriptor  Descriptor
	prepared    json.RawMessage
	prepareErr  error
	prepareArgs []json.RawMessage
	summary     func(json.RawMessage) any
}

func (h *fakeHandler) Descriptor() Descriptor {
	return h.descriptor
}

func (h *fakeHandler) Prepare(_ context.Context, payload json.RawMessage) (json.RawMessage, error) {
	h.prepareArgs = append(h.prepareArgs, append(json.RawMessage(nil), payload...))
	if h.prepareErr != nil {
		return nil, h.prepareErr
	}
	if h.prepared != nil {
		return append(json.RawMessage(nil), h.prepared...), nil
	}
	return append(json.RawMessage(nil), payload...), nil
}

func (h *fakeHandler) Run(context.Context, json.RawMessage, Execution) (json.RawMessage, error) {
	return json.RawMessage(`{"ok":true}`), nil
}

func (h *fakeHandler) PublicSummary(payload json.RawMessage) any {
	if h.summary == nil {
		return nil
	}
	return h.summary(payload)
}

type identityFakeHandler struct {
	*fakeHandler
}

func (h *identityFakeHandler) ScheduleIdentity(payload json.RawMessage) (string, bool) {
	var value struct {
		Target string `json:"target"`
	}
	if json.Unmarshal(payload, &value) != nil || value.Target == "" {
		return "", false
	}
	return value.Target, true
}

type fakeRepository struct {
	Repository
	now              time.Time
	nextRunID        int64
	nextScheduleID   int64
	runs             map[int64]Run
	createdRuns      []NewRun
	createdSchedules []Schedule
	schedules        map[int64]Schedule
	cancelCalls      int
	deleteCalls      int
}

func newFakeRepository(now time.Time) *fakeRepository {
	return &fakeRepository{
		now:            now,
		nextRunID:      40,
		nextScheduleID: 10,
		runs:           make(map[int64]Run),
		schedules:      make(map[int64]Schedule),
	}
}

func (r *fakeRepository) CreateRun(_ context.Context, input NewRun) (Run, error) {
	r.createdRuns = append(r.createdRuns, input)
	r.nextRunID++
	run := Run{
		ID:             r.nextRunID,
		ScheduleID:     input.ScheduleID,
		Type:           input.Type,
		PayloadVersion: input.PayloadVersion,
		Payload:        append(json.RawMessage(nil), input.Payload...),
		Trigger:        input.Trigger,
		Status:         StatusQueued,
		MaxAttempts:    input.MaxAttempts,
		AvailableAt:    input.AvailableAt,
		ScheduledFor:   input.ScheduledFor,
		RetryOfRunID:   input.RetryOfRunID,
		CreatedBy:      input.CreatedBy,
		CreatedAt:      r.now,
		UpdatedAt:      r.now,
	}
	r.runs[run.ID] = run
	return run, nil
}

func (r *fakeRepository) GetRun(_ context.Context, id int64) (Run, error) {
	run, ok := r.runs[id]
	if !ok {
		return Run{}, ErrNotFound
	}
	return run, nil
}

func (r *fakeRepository) CancelRun(_ context.Context, id int64, now time.Time) (Run, error) {
	r.cancelCalls++
	run, ok := r.runs[id]
	if !ok {
		return Run{}, ErrNotFound
	}
	switch run.Status {
	case StatusQueued:
		run.Status = StatusCanceled
		run.FinishedAt = timePointer(now)
	case StatusRunning:
		run.CancelRequestedAt = timePointer(now)
	}
	run.UpdatedAt = now
	r.runs[id] = run
	return run, nil
}

func (r *fakeRepository) DeleteRun(_ context.Context, id int64) error {
	r.deleteCalls++
	run, ok := r.runs[id]
	if !ok {
		return ErrNotFound
	}
	if run.Status == StatusRunning {
		return ErrRunActive
	}
	delete(r.runs, id)
	return nil
}

func (r *fakeRepository) CreateSchedule(_ context.Context, schedule Schedule) (Schedule, error) {
	r.nextScheduleID++
	schedule.ID = r.nextScheduleID
	r.createdSchedules = append(r.createdSchedules, schedule)
	r.schedules[schedule.ID] = schedule
	return schedule, nil
}

func (r *fakeRepository) UpdateSchedule(_ context.Context, schedule Schedule) (Schedule, error) {
	if _, ok := r.schedules[schedule.ID]; !ok {
		return Schedule{}, ErrNotFound
	}
	r.schedules[schedule.ID] = schedule
	return schedule, nil
}

func (r *fakeRepository) GetSchedule(_ context.Context, id int64) (Schedule, error) {
	schedule, ok := r.schedules[id]
	if !ok {
		return Schedule{}, ErrNotFound
	}
	return schedule, nil
}

func (r *fakeRepository) ListSchedules(context.Context) ([]Schedule, error) {
	items := make([]Schedule, 0, len(r.schedules))
	for _, schedule := range r.schedules {
		items = append(items, schedule)
	}
	return items, nil
}

func (r *fakeRepository) LastRunForSchedule(context.Context, int64) (*Run, error) {
	return nil, nil
}

func TestNewRegistryRejectsDuplicateTaskTypes(t *testing.T) {
	first := &fakeHandler{descriptor: testDescriptor("fixture.task")}
	duplicate := &fakeHandler{descriptor: testDescriptor("fixture.task")}

	_, err := NewRegistry(first, duplicate)
	if err == nil || !strings.Contains(err.Error(), "duplicate type") {
		t.Fatalf("NewRegistry() error = %v, want duplicate type error", err)
	}
}

func TestCreateManualRunPreparesAndEnqueuesPayload(t *testing.T) {
	now := time.Date(2026, time.July, 22, 3, 0, 0, 0, time.UTC)
	repository := newFakeRepository(now)
	handler := &fakeHandler{
		descriptor: testDescriptor("fixture.manual"),
		prepared:   json.RawMessage(`{"normalized":"value","secret":"synthetic-secret"}`),
		summary: func(json.RawMessage) any {
			return map[string]string{"name": "safe subject"}
		},
	}
	manager := newTestManager(t, repository, now, handler)

	result, err := manager.CreateManualRun(t.Context(), "  local-admin  ", ManualRunInput{
		Type:    "fixture.manual",
		Payload: json.RawMessage(`{"input":"value"}`),
	})
	if err != nil {
		t.Fatalf("CreateManualRun() error = %v", err)
	}
	if len(handler.prepareArgs) != 1 || string(handler.prepareArgs[0]) != `{"input":"value"}` {
		t.Fatalf("Prepare() args = %q, want original input", handler.prepareArgs)
	}
	if len(repository.createdRuns) != 1 {
		t.Fatalf("CreateRun() calls = %d, want 1", len(repository.createdRuns))
	}
	created := repository.createdRuns[0]
	if created.Type != "fixture.manual" || created.PayloadVersion != 1 {
		t.Fatalf("created run type/version = %q/%d", created.Type, created.PayloadVersion)
	}
	if created.Trigger != TriggerManual || created.MaxAttempts != DefaultMaxAttempts {
		t.Fatalf("created run trigger/attempts = %q/%d", created.Trigger, created.MaxAttempts)
	}
	if created.CreatedBy != "local-admin" || !created.AvailableAt.Equal(now) {
		t.Fatalf("created run identity/time = %q/%v", created.CreatedBy, created.AvailableAt)
	}
	if string(created.Payload) != string(handler.prepared) {
		t.Fatalf("created payload = %s, want prepared payload %s", created.Payload, handler.prepared)
	}
	if result.Status != StatusQueued || result.Subject == nil {
		t.Fatalf("public run = %#v", result)
	}
}

func TestCreateScheduleValidatesFiveFieldCronAndTimezone(t *testing.T) {
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	handler := &fakeHandler{
		descriptor: Descriptor{
			Type:           "fixture.scheduled",
			PayloadVersion: 1,
			Manual:         true,
			Schedulable:    true,
			Cancelable:     true,
		},
	}

	t.Run("accepts five fields in the selected timezone", func(t *testing.T) {
		repository := newFakeRepository(now)
		manager := newTestManager(t, repository, now, handler)
		created, err := manager.CreateSchedule(t.Context(), "local-admin", ScheduleInput{
			Name:           "Morning task",
			Type:           "fixture.scheduled",
			CronExpression: "0 9 * * *",
			Timezone:       "Asia/Shanghai",
			Enabled:        true,
			Payload:        json.RawMessage(`{"path":"/Music"}`),
		})
		if err != nil {
			t.Fatalf("CreateSchedule() error = %v", err)
		}
		expected := time.Date(2026, time.January, 1, 1, 0, 0, 0, time.UTC)
		if created.NextRunAt == nil || !created.NextRunAt.Equal(expected) {
			t.Fatalf("next run = %v, want %v", created.NextRunAt, expected)
		}
		if len(repository.createdSchedules) != 1 {
			t.Fatalf("CreateSchedule() calls = %d, want 1", len(repository.createdSchedules))
		}
	})

	t.Run("stores a manual definition without a schedule", func(t *testing.T) {
		repository := newFakeRepository(now)
		manager := newTestManager(t, repository, now, handler)
		created, err := manager.CreateSchedule(t.Context(), "local-admin", ScheduleInput{
			Name:    "On-demand task",
			Type:    "fixture.scheduled",
			Enabled: true,
			Payload: json.RawMessage(`{"path":"/Music"}`),
		})
		if err != nil {
			t.Fatalf("CreateSchedule() error = %v", err)
		}
		if created.CronExpression != "" || created.Timezone != "" || created.Enabled || created.NextRunAt != nil {
			t.Fatalf("manual definition = %#v", created)
		}
	})

	tests := []struct {
		name       string
		expression string
		timezone   string
		code       string
	}{
		{name: "rejects cron seconds field", expression: "0 0 9 * * *", timezone: "UTC", code: "invalid_cron"},
		{name: "rejects unknown timezone", expression: "0 9 * * *", timezone: "Mars/Olympus", code: "invalid_timezone"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := newFakeRepository(now)
			manager := newTestManager(t, repository, now, handler)
			_, err := manager.CreateSchedule(t.Context(), "local-admin", ScheduleInput{
				Name:           "Invalid task",
				Type:           "fixture.scheduled",
				CronExpression: test.expression,
				Timezone:       test.timezone,
				Enabled:        true,
				Payload:        json.RawMessage(`{}`),
			})
			assertPublicErrorCode(t, err, test.code)
			if len(repository.createdSchedules) != 0 {
				t.Fatal("invalid schedule reached repository")
			}
		})
	}
}

func TestCreateScheduleRejectsDuplicateHandlerIdentity(t *testing.T) {
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	repository := newFakeRepository(now)
	handler := &identityFakeHandler{fakeHandler: &fakeHandler{descriptor: Descriptor{
		Type: "fixture.unique", PayloadVersion: 1, Manual: true, Schedulable: true,
	}}}
	manager := newTestManager(t, repository, now, handler)
	input := ScheduleInput{
		Name: "Unique target", Type: "fixture.unique", CronExpression: "0 * * * *",
		Timezone: "UTC", Enabled: true, Payload: json.RawMessage(`{"target":"library-7"}`),
	}
	if _, err := manager.CreateSchedule(t.Context(), "admin", input); err != nil {
		t.Fatal(err)
	}
	_, err := manager.CreateSchedule(t.Context(), "admin", input)
	assertPublicErrorCode(t, err, "duplicate_schedule")
	if len(repository.createdSchedules) != 1 {
		t.Fatalf("created schedules = %d, want 1", len(repository.createdSchedules))
	}
}

func TestUpdateScheduleRejectsDuplicateHandlerIdentity(t *testing.T) {
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	repository := newFakeRepository(now)
	handler := &identityFakeHandler{fakeHandler: &fakeHandler{descriptor: Descriptor{
		Type: "fixture.unique", PayloadVersion: 1, Manual: true, Schedulable: true,
	}}}
	manager := newTestManager(t, repository, now, handler)
	input := func(target string) ScheduleInput {
		return ScheduleInput{
			Name: "Unique target", Type: "fixture.unique", CronExpression: "0 * * * *",
			Timezone: "UTC", Enabled: true, Payload: json.RawMessage(`{"target":"` + target + `"}`),
		}
	}
	if _, err := manager.CreateSchedule(t.Context(), "admin", input("library-7")); err != nil {
		t.Fatal(err)
	}
	second, err := manager.CreateSchedule(t.Context(), "admin", input("library-8"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.UpdateSchedule(t.Context(), second.ID, input("library-7"))
	assertPublicErrorCode(t, err, "duplicate_schedule")
	if got := string(repository.schedules[second.ID].Payload); got != `{"target":"library-8"}` {
		t.Fatalf("stored payload = %s, want original identity", got)
	}
}

func TestRunScheduleNowAcceptsManualOnlyDefinition(t *testing.T) {
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	repository := newFakeRepository(now)
	handler := &fakeHandler{descriptor: Descriptor{
		Type:           "fixture.manual-only",
		PayloadVersion: 1,
		Manual:         true,
	}}
	manager := newTestManager(t, repository, now, handler)
	definition, err := manager.CreateSchedule(t.Context(), "local-admin", ScheduleInput{
		Name:    "Manual definition",
		Type:    "fixture.manual-only",
		Payload: json.RawMessage(`{"path":"/synthetic"}`),
	})
	if err != nil {
		t.Fatalf("CreateSchedule() error = %v", err)
	}
	run, err := manager.RunScheduleNow(t.Context(), definition.ID, "local-admin")
	if err != nil {
		t.Fatalf("RunScheduleNow() error = %v", err)
	}
	if run.ScheduleID == nil || *run.ScheduleID != definition.ID || run.Trigger != TriggerRunNow {
		t.Fatalf("run = %#v", run)
	}
}

func TestPublicRunUsesHandlerSummaryWithoutExposingPayload(t *testing.T) {
	now := time.Date(2026, time.July, 22, 4, 0, 0, 0, time.UTC)
	repository := newFakeRepository(now)
	handler := &fakeHandler{
		descriptor: testDescriptor("fixture.private"),
		prepared:   json.RawMessage(`{"path":"/Music/song.flac","token":"raw-synthetic-token"}`),
		summary: func(json.RawMessage) any {
			return map[string]string{"path": "/Music/song.flac"}
		},
	}
	manager := newTestManager(t, repository, now, handler)

	run, err := manager.CreateManualRun(t.Context(), "local-admin", ManualRunInput{
		Type:    "fixture.private",
		Payload: json.RawMessage(`{"token":"request-token"}`),
	})
	if err != nil {
		t.Fatalf("CreateManualRun() error = %v", err)
	}
	encoded, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"raw-synthetic-token", "request-token"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("public run leaks raw payload value %q: %s", secret, encoded)
		}
	}
	if !strings.Contains(string(encoded), "/Music/song.flac") {
		t.Fatalf("public run omits safe summary: %s", encoded)
	}
}

func TestCancelRunHonorsHandlerCapabilityAndRunState(t *testing.T) {
	now := time.Date(2026, time.July, 22, 5, 0, 0, 0, time.UTC)

	t.Run("rejects a non-cancelable task type", func(t *testing.T) {
		repository := newFakeRepository(now)
		repository.runs[1] = Run{ID: 1, Type: "fixture.fixed", PayloadVersion: 1, Status: StatusQueued}
		handler := &fakeHandler{descriptor: Descriptor{
			Type:           "fixture.fixed",
			PayloadVersion: 1,
			Manual:         true,
			Cancelable:     false,
		}}
		manager := newTestManager(t, repository, now, handler)

		_, err := manager.CancelRun(t.Context(), 1)
		if !errors.Is(err, ErrNotCancelable) {
			t.Fatalf("CancelRun() error = %v, want ErrNotCancelable", err)
		}
		if repository.cancelCalls != 0 {
			t.Fatal("non-cancelable task reached repository cancellation")
		}
	})

	t.Run("cancels a queued task", func(t *testing.T) {
		repository := newFakeRepository(now)
		repository.runs[2] = Run{ID: 2, Type: "fixture.cancelable", PayloadVersion: 1, Status: StatusQueued}
		handler := &fakeHandler{descriptor: testDescriptor("fixture.cancelable")}
		manager := newTestManager(t, repository, now, handler)

		canceled, err := manager.CancelRun(t.Context(), 2)
		if err != nil {
			t.Fatalf("CancelRun() error = %v", err)
		}
		if canceled.Status != StatusCanceled || canceled.FinishedAt == nil {
			t.Fatalf("canceled run = %#v", canceled)
		}
	})

	t.Run("requests cooperative cancellation for a running task", func(t *testing.T) {
		repository := newFakeRepository(now)
		repository.runs[3] = Run{ID: 3, Type: "fixture.cancelable", PayloadVersion: 1, Status: StatusRunning}
		handler := &fakeHandler{descriptor: testDescriptor("fixture.cancelable")}
		manager := newTestManager(t, repository, now, handler)

		canceling, err := manager.CancelRun(t.Context(), 3)
		if err != nil {
			t.Fatalf("CancelRun() error = %v", err)
		}
		if canceling.Status != StatusRunning || !canceling.CancelRequested {
			t.Fatalf("canceling run = %#v", canceling)
		}
	})

	t.Run("rejects a terminal task", func(t *testing.T) {
		repository := newFakeRepository(now)
		repository.runs[4] = Run{ID: 4, Type: "fixture.cancelable", PayloadVersion: 1, Status: StatusSucceeded}
		handler := &fakeHandler{descriptor: testDescriptor("fixture.cancelable")}
		manager := newTestManager(t, repository, now, handler)

		_, err := manager.CancelRun(t.Context(), 4)
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("CancelRun() error = %v, want ErrConflict", err)
		}
		if repository.cancelCalls != 0 {
			t.Fatal("terminal task reached repository cancellation")
		}
	})
}

func TestRetryRunOnlyAcceptsFailedOrCanceledRuns(t *testing.T) {
	now := time.Date(2026, time.July, 22, 6, 0, 0, 0, time.UTC)
	handler := &fakeHandler{descriptor: testDescriptor("fixture.retry")}

	for _, status := range []Status{StatusQueued, StatusRunning, StatusSucceeded} {
		t.Run("rejects "+string(status), func(t *testing.T) {
			repository := newFakeRepository(now)
			repository.runs[1] = Run{ID: 1, Type: "fixture.retry", PayloadVersion: 1, Status: status}
			manager := newTestManager(t, repository, now, handler)

			_, err := manager.RetryRun(t.Context(), 1, "local-admin")
			if !errors.Is(err, ErrConflict) {
				t.Fatalf("RetryRun() error = %v, want ErrConflict", err)
			}
			if len(repository.createdRuns) != 0 {
				t.Fatal("invalid retry created a run")
			}
		})
	}

	for _, status := range []Status{StatusFailed, StatusCanceled} {
		t.Run("retries "+string(status), func(t *testing.T) {
			repository := newFakeRepository(now)
			repository.runs[9] = Run{
				ID:             9,
				Type:           "fixture.retry",
				PayloadVersion: 1,
				Payload:        json.RawMessage(`{"path":"/synthetic"}`),
				Status:         status,
			}
			manager := newTestManager(t, repository, now, handler)

			retry, err := manager.RetryRun(t.Context(), 9, "  local-admin  ")
			if err != nil {
				t.Fatalf("RetryRun() error = %v", err)
			}
			if retry.Trigger != TriggerRetry || retry.RetryOfRunID == nil || *retry.RetryOfRunID != 9 {
				t.Fatalf("retry run = %#v", retry)
			}
			created := repository.createdRuns[0]
			if created.CreatedBy != "local-admin" || string(created.Payload) != `{"path":"/synthetic"}` {
				t.Fatalf("retry input = %#v", created)
			}
		})
	}
}

func TestDeleteRunAcceptsQueuedAndTerminalRuns(t *testing.T) {
	now := time.Date(2026, time.July, 22, 6, 15, 0, 0, time.UTC)
	repository := newFakeRepository(now)
	repository.runs[1] = Run{ID: 1, Status: StatusRunning}
	repository.runs[2] = Run{ID: 2, Status: StatusQueued}
	repository.runs[3] = Run{ID: 3, Status: StatusSucceeded}
	manager := newTestManager(t, repository, now)

	if err := manager.DeleteRun(t.Context(), 1); !errors.Is(err, ErrRunActive) {
		t.Fatalf("DeleteRun(active) error = %v, want ErrRunActive", err)
	}
	if err := manager.DeleteRun(t.Context(), 2); err != nil {
		t.Fatalf("DeleteRun(queued) error = %v", err)
	}
	if _, ok := repository.runs[2]; ok {
		t.Fatal("queued run was not deleted")
	}
	if err := manager.DeleteRun(t.Context(), 3); err != nil {
		t.Fatalf("DeleteRun(terminal) error = %v", err)
	}
	if _, ok := repository.runs[3]; ok {
		t.Fatal("terminal run was not deleted")
	}
}

func TestWaitRunPreservesPersistedPublicErrorStatus(t *testing.T) {
	now := time.Date(2026, time.July, 22, 6, 30, 0, 0, time.UTC)
	repository := newFakeRepository(now)
	repository.runs[15] = Run{
		ID:           15,
		Type:         "fixture.failed",
		Status:       StatusFailed,
		ErrorCode:    "upstream_422",
		ErrorMessage: "cover image is unavailable",
		ErrorStatus:  422,
	}
	manager := newTestManager(t, repository, now)

	_, err := manager.WaitRun(t.Context(), 15, nil)
	var publicErr *PublicError
	if !errors.As(err, &publicErr) {
		t.Fatalf("WaitRun() error = %v, want PublicError", err)
	}
	if publicErr.Status != 422 || publicErr.Code != "upstream_422" || publicErr.Message != "cover image is unavailable" {
		t.Fatalf("WaitRun() error = %#v", publicErr)
	}
}

func newTestManager(t *testing.T, repository Repository, now time.Time, handlers ...Handler) *Manager {
	t.Helper()
	registry, err := NewRegistry(handlers...)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	options := DefaultOptions()
	options.Now = func() time.Time { return now }
	manager, err := NewManager(repository, registry, options)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	return manager
}

func testDescriptor(taskType string) Descriptor {
	return Descriptor{
		Type:           taskType,
		PayloadVersion: 1,
		Manual:         true,
		Cancelable:     true,
	}
}

func assertPublicErrorCode(t *testing.T, err error, code string) {
	t.Helper()
	var publicErr *PublicError
	if !errors.As(err, &publicErr) {
		t.Fatalf("error = %v, want PublicError %q", err, code)
	}
	if publicErr.Code != code {
		t.Fatalf("error code = %q, want %q", publicErr.Code, code)
	}
}

func timePointer(value time.Time) *time.Time {
	return &value
}

type completedRunsRepository struct {
	Repository
	ids []int64
	err error
	ctx context.Context
}

func (r *completedRunsRepository) DeleteCompletedRuns(ctx context.Context) ([]int64, error) {
	r.ctx = ctx
	return r.ids, r.err
}

func TestDeleteCompletedRunsClearsOnlyDeletedProgressHistory(t *testing.T) {
	for _, fails := range []bool{false, true} {
		t.Run(fmt.Sprintf("fails=%t", fails), func(t *testing.T) {
			repository := &completedRunsRepository{ids: []int64{1, 2}}
			if fails {
				repository.err = errors.New("synthetic store failure")
			}
			manager := newTestManager(t, repository, time.Now())
			for _, id := range []int64{1, 2, 3} {
				manager.progressHistory[id] = []Progress{{Percent: 50}}
			}
			ctx := t.Context()
			deleted, err := manager.DeleteCompletedRuns(ctx)
			if repository.ctx != ctx {
				t.Fatal("request context was not propagated")
			}
			if fails {
				if deleted != 0 || !errors.Is(err, repository.err) || len(manager.progressHistory) != 3 {
					t.Fatalf("failed cleanup = %d, %v, history=%v", deleted, err, manager.progressHistory)
				}
			} else if deleted != 2 || err != nil || len(manager.progressHistory) != 1 || len(manager.progressHistory[3]) != 1 {
				t.Fatalf("cleanup = %d, %v, history=%v", deleted, err, manager.progressHistory)
			}
		})
	}
}
