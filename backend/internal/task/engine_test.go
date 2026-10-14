package task

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestEngineWorkerCompletesRunAndPersistsProgress(t *testing.T) {
	now := time.Date(2026, time.July, 22, 8, 0, 0, 0, time.UTC)
	run := engineTestRun(1, "fixture.success", now)
	repository := newEngineRepository(run)
	handler := newEngineHandler(run.Type, func(_ context.Context, _ json.RawMessage, execution Execution) (json.RawMessage, error) {
		execution.Report(Progress{Stage: "download", Percent: 20})
		execution.Report(Progress{Stage: "embed", Percent: 70})
		execution.Report(Progress{Stage: "upload", Percent: 100})
		return json.RawMessage(`{"embedded":true}`), nil
	})
	manager := newEngineTestManager(t, repository, handler, func() time.Time { return now })
	startEngineTestManager(t, manager)

	waitForEngineTestSignal(t, repository.completed, "worker completion")
	shutdownEngineTestManager(t, manager)

	state := repository.snapshot(run.ID)
	if state.run.Status != StatusSucceeded || state.run.Attempt != 1 {
		t.Fatalf("completed run status/attempt = %q/%d, want %q/1", state.run.Status, state.run.Attempt, StatusSucceeded)
	}
	if string(state.run.Result) != `{"embedded":true}` {
		t.Fatalf("completed result = %s", state.run.Result)
	}
	wantProgress := []Progress{
		{Stage: "download", Percent: 20},
		{Stage: "embed", Percent: 70},
		{Stage: "upload", Percent: 100},
	}
	if len(state.progress) != len(wantProgress) {
		t.Fatalf("progress updates = %#v, want %#v", state.progress, wantProgress)
	}
	for index := range wantProgress {
		if state.progress[index] != wantProgress[index] {
			t.Fatalf("progress update %d = %#v, want %#v", index, state.progress[index], wantProgress[index])
		}
	}
	if state.completeCalls != 1 || state.retryCalls != 0 || state.failCalls != 0 || state.releaseCalls != 0 {
		t.Fatalf("terminal calls = complete:%d retry:%d fail:%d release:%d", state.completeCalls, state.retryCalls, state.failCalls, state.releaseCalls)
	}
}

func TestProgressReporterPersistsChangedScanCountsAtSamePercent(t *testing.T) {
	now := time.Date(2026, time.July, 22, 8, 0, 0, 0, time.UTC)
	run := engineTestRun(2, "fixture.scan", now)
	run.Status = StatusRunning
	run.LeaseOwner = "worker-scan"
	repository := newEngineRepository(run)
	manager := newEngineTestManager(t, repository, newEngineHandler(run.Type, nil), func() time.Time {
		return now
	})
	attachEngineTestContext(t, manager)

	report := manager.progressReporter(run.ID, run.LeaseOwner)
	report(Progress{Stage: "scanning", Percent: 0})
	now = now.Add(2 * time.Hour)
	report(Progress{Stage: "scanning", Percent: 0, ScannedDirectories: 8, ScannedFiles: 120})

	state := repository.snapshot(run.ID)
	want := []Progress{
		{Stage: "scanning", Percent: 0},
		{Stage: "scanning", Percent: 0, ScannedDirectories: 8, ScannedFiles: 120},
	}
	if !reflect.DeepEqual(state.progress, want) {
		t.Fatalf("scan progress updates = %#v, want %#v", state.progress, want)
	}
}

func TestEngineRetryableFailureUsesAttemptBackoff(t *testing.T) {
	now := time.Date(2026, time.July, 22, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		attempt int
		backoff time.Duration
	}{
		{name: "first failure waits thirty seconds", attempt: 1, backoff: 30 * time.Second},
		{name: "second failure waits two minutes", attempt: 2, backoff: 2 * time.Minute},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			run := engineTestRun(10+int64(test.attempt), "fixture.retryable", now)
			run.Status = StatusRunning
			run.Attempt = test.attempt
			run.LeaseOwner = "worker-retry"
			repository := newEngineRepository(run)
			handler := newEngineHandler(run.Type, func(context.Context, json.RawMessage, Execution) (json.RawMessage, error) {
				return nil, NewPublicError(
					"upstream_unavailable",
					"upstream service is temporarily unavailable",
					503,
					true,
					errors.New("synthetic upstream failure"),
				)
			})
			manager := newEngineTestManager(t, repository, handler, func() time.Time { return now })
			attachEngineTestContext(t, manager)

			manager.executeRun(run.LeaseOwner, run)

			state := repository.snapshot(run.ID)
			wantAvailableAt := now.Add(test.backoff)
			if state.run.Status != StatusQueued || !state.run.AvailableAt.Equal(wantAvailableAt) {
				t.Fatalf("retried run status/available_at = %q/%v, want %q/%v", state.run.Status, state.run.AvailableAt, StatusQueued, wantAvailableAt)
			}
			if state.run.Attempt != test.attempt {
				t.Fatalf("retried run attempt = %d, want %d", state.run.Attempt, test.attempt)
			}
			if state.run.ErrorCode != "upstream_unavailable" || state.run.ErrorMessage != "upstream service is temporarily unavailable" {
				t.Fatalf("retried run error = %q/%q", state.run.ErrorCode, state.run.ErrorMessage)
			}
			if state.retryCalls != 1 || state.failCalls != 0 || state.completeCalls != 0 || state.releaseCalls != 0 {
				t.Fatalf("transition calls = retry:%d fail:%d complete:%d release:%d", state.retryCalls, state.failCalls, state.completeCalls, state.releaseCalls)
			}
		})
	}
}

func TestEngineCooperativelyCancelsRunningHandler(t *testing.T) {
	now := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	run := engineTestRun(20, "fixture.cancel", now)
	run.Status = StatusRunning
	run.Attempt = 1
	run.LeaseOwner = "worker-cancel"
	repository := newEngineRepository(run)
	handlerStarted := make(chan struct{})
	handlerCause := make(chan error, 1)
	handler := newEngineHandler(run.Type, func(ctx context.Context, _ json.RawMessage, _ Execution) (json.RawMessage, error) {
		close(handlerStarted)
		<-ctx.Done()
		handlerCause <- context.Cause(ctx)
		return nil, ctx.Err()
	})
	manager := newEngineTestManager(t, repository, handler, func() time.Time { return now })
	attachEngineTestContext(t, manager)
	executionDone := make(chan struct{})
	go func() {
		manager.executeRun(run.LeaseOwner, run)
		close(executionDone)
	}()

	waitForEngineTestSignal(t, handlerStarted, "handler start")
	canceling, err := manager.CancelRun(t.Context(), run.ID)
	if err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}
	if canceling.Status != StatusRunning || !canceling.CancelRequested {
		t.Fatalf("canceling run = %#v", canceling)
	}
	select {
	case cause := <-handlerCause:
		if !errors.Is(cause, ErrRunCanceled) {
			t.Fatalf("handler cancellation cause = %v, want ErrRunCanceled", cause)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for handler cancellation")
	}
	waitForEngineTestSignal(t, repository.canceled, "canceled run persistence")
	waitForEngineTestSignal(t, executionDone, "canceled execution exit")

	state := repository.snapshot(run.ID)
	if state.run.Status != StatusCanceled || state.cancelCalls != 1 || state.markCanceledCalls != 1 {
		t.Fatalf("canceled run status/calls = %q/%d/%d", state.run.Status, state.cancelCalls, state.markCanceledCalls)
	}
	if state.retryCalls != 0 || state.failCalls != 0 || state.completeCalls != 0 || state.releaseCalls != 0 {
		t.Fatalf("unexpected terminal calls = retry:%d fail:%d complete:%d release:%d", state.retryCalls, state.failCalls, state.completeCalls, state.releaseCalls)
	}
}

func TestEngineShutdownReleasesRunningRunWithoutConsumingAttempt(t *testing.T) {
	now := time.Date(2026, time.July, 22, 11, 0, 0, 0, time.UTC)
	run := engineTestRun(30, "fixture.shutdown", now)
	repository := newEngineRepository(run)
	handlerStarted := make(chan struct{})
	handlerCause := make(chan error, 1)
	handler := newEngineHandler(run.Type, func(ctx context.Context, _ json.RawMessage, _ Execution) (json.RawMessage, error) {
		close(handlerStarted)
		<-ctx.Done()
		handlerCause <- context.Cause(ctx)
		return nil, ctx.Err()
	})
	manager := newEngineTestManager(t, repository, handler, func() time.Time { return now })
	startEngineTestManager(t, manager)

	waitForEngineTestSignal(t, handlerStarted, "handler start")
	running := repository.snapshot(run.ID).run
	if running.Status != StatusRunning || running.Attempt != 1 {
		t.Fatalf("claimed run status/attempt = %q/%d, want %q/1", running.Status, running.Attempt, StatusRunning)
	}
	shutdownEngineTestManager(t, manager)

	select {
	case cause := <-handlerCause:
		if !errors.Is(cause, ErrManagerStopping) {
			t.Fatalf("handler shutdown cause = %v, want ErrManagerStopping", cause)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for handler shutdown")
	}
	waitForEngineTestSignal(t, repository.released, "run release")
	state := repository.snapshot(run.ID)
	if state.run.Status != StatusQueued || state.run.Attempt != 0 {
		t.Fatalf("released run status/attempt = %q/%d, want %q/0", state.run.Status, state.run.Attempt, StatusQueued)
	}
	if !state.run.AvailableAt.Equal(now) || state.run.LeaseOwner != "" || state.run.LeaseExpiresAt != nil {
		t.Fatalf("released run queue state = %#v", state.run)
	}
	if state.releaseCalls != 1 || state.retryCalls != 0 || state.failCalls != 0 || state.completeCalls != 0 || state.markCanceledCalls != 0 {
		t.Fatalf("shutdown transition calls = release:%d retry:%d fail:%d complete:%d canceled:%d", state.releaseCalls, state.retryCalls, state.failCalls, state.completeCalls, state.markCanceledCalls)
	}
}

type engineHandler struct {
	descriptor Descriptor
	run        func(context.Context, json.RawMessage, Execution) (json.RawMessage, error)
}

func newEngineHandler(taskType string, run func(context.Context, json.RawMessage, Execution) (json.RawMessage, error)) *engineHandler {
	return &engineHandler{
		descriptor: Descriptor{
			Type:           taskType,
			PayloadVersion: 1,
			Manual:         true,
			Cancelable:     true,
		},
		run: run,
	}
}

func (h *engineHandler) Descriptor() Descriptor {
	return h.descriptor
}

func (h *engineHandler) Prepare(_ context.Context, payload json.RawMessage) (json.RawMessage, error) {
	return append(json.RawMessage(nil), payload...), nil
}

func (h *engineHandler) Run(ctx context.Context, payload json.RawMessage, execution Execution) (json.RawMessage, error) {
	return h.run(ctx, payload, execution)
}

func (h *engineHandler) PublicSummary(json.RawMessage) any {
	return nil
}

type engineRepository struct {
	Repository

	mutex sync.Mutex
	runs  map[int64]Run

	progress          []Progress
	completeCalls     int
	retryCalls        int
	failCalls         int
	releaseCalls      int
	cancelCalls       int
	markCanceledCalls int

	completed chan struct{}
	canceled  chan struct{}
	released  chan struct{}
}

type engineRepositorySnapshot struct {
	run               Run
	progress          []Progress
	completeCalls     int
	retryCalls        int
	failCalls         int
	releaseCalls      int
	cancelCalls       int
	markCanceledCalls int
}

func newEngineRepository(runs ...Run) *engineRepository {
	repository := &engineRepository{
		runs:      make(map[int64]Run, len(runs)),
		completed: make(chan struct{}, 1),
		canceled:  make(chan struct{}, 1),
		released:  make(chan struct{}, 1),
	}
	for _, run := range runs {
		repository.runs[run.ID] = run
	}
	return repository
}

func (r *engineRepository) GetRun(_ context.Context, id int64) (Run, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	run, ok := r.runs[id]
	if !ok {
		return Run{}, ErrNotFound
	}
	return run, nil
}

func (r *engineRepository) ClaimRun(_ context.Context, owner string, now, leaseUntil time.Time) (*Run, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	for id, run := range r.runs {
		if run.Status != StatusQueued || run.AvailableAt.After(now) {
			continue
		}
		run.Status = StatusRunning
		run.Attempt++
		if run.StartedAt == nil {
			run.StartedAt = engineTimePointer(now)
		}
		run.LeaseOwner = owner
		run.LeaseExpiresAt = engineTimePointer(leaseUntil)
		run.UpdatedAt = now
		r.runs[id] = run
		claimed := run
		return &claimed, nil
	}
	return nil, nil
}

func (r *engineRepository) HeartbeatRun(_ context.Context, id int64, owner string, leaseUntil, now time.Time) (bool, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	run, ok := r.runs[id]
	if !ok || run.Status != StatusRunning || run.LeaseOwner != owner {
		return false, ErrConflict
	}
	run.LeaseExpiresAt = engineTimePointer(leaseUntil)
	run.UpdatedAt = now
	r.runs[id] = run
	return run.CancelRequestedAt != nil, nil
}

func (r *engineRepository) UpdateRunProgress(_ context.Context, id int64, owner string, progress Progress, now time.Time) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	run, ok := r.runs[id]
	if !ok || run.Status != StatusRunning || run.LeaseOwner != owner {
		return ErrConflict
	}
	run.ProgressStage = progress.Stage
	run.ProgressPercent = progress.Percent
	run.UpdatedAt = now
	r.runs[id] = run
	r.progress = append(r.progress, progress)
	return nil
}

func (r *engineRepository) CompleteRun(_ context.Context, id int64, owner string, result []byte, now time.Time) error {
	r.mutex.Lock()
	run, ok := r.runs[id]
	if !ok || run.Status != StatusRunning || run.LeaseOwner != owner {
		r.mutex.Unlock()
		return ErrConflict
	}
	run.Status = StatusSucceeded
	run.Result = append(json.RawMessage(nil), result...)
	run.ProgressPercent = 100
	run.FinishedAt = engineTimePointer(now)
	run.LeaseOwner = ""
	run.LeaseExpiresAt = nil
	run.UpdatedAt = now
	r.runs[id] = run
	r.completeCalls++
	r.mutex.Unlock()
	engineTestSignal(r.completed)
	return nil
}

func (r *engineRepository) RetryRun(_ context.Context, id int64, owner string, availableAt time.Time, code, message string, status int, now time.Time) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	run, ok := r.runs[id]
	if !ok || run.Status != StatusRunning || run.LeaseOwner != owner {
		return ErrConflict
	}
	run.Status = StatusQueued
	run.AvailableAt = availableAt
	run.ErrorCode = code
	run.ErrorMessage = message
	run.ErrorStatus = status
	run.LeaseOwner = ""
	run.LeaseExpiresAt = nil
	run.UpdatedAt = now
	r.runs[id] = run
	r.retryCalls++
	return nil
}

func (r *engineRepository) FailRun(_ context.Context, id int64, owner, code, message string, status int, now time.Time) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	run, ok := r.runs[id]
	if !ok || run.Status != StatusRunning || run.LeaseOwner != owner {
		return ErrConflict
	}
	run.Status = StatusFailed
	run.ErrorCode = code
	run.ErrorMessage = message
	run.ErrorStatus = status
	run.FinishedAt = engineTimePointer(now)
	run.LeaseOwner = ""
	run.LeaseExpiresAt = nil
	run.UpdatedAt = now
	r.runs[id] = run
	r.failCalls++
	return nil
}

func (r *engineRepository) ReleaseRun(_ context.Context, id int64, owner string, now time.Time) error {
	r.mutex.Lock()
	run, ok := r.runs[id]
	if !ok || run.Status != StatusRunning || run.LeaseOwner != owner {
		r.mutex.Unlock()
		return ErrConflict
	}
	run.Status = StatusQueued
	run.AvailableAt = now
	if run.Attempt > 0 {
		run.Attempt--
	}
	run.LeaseOwner = ""
	run.LeaseExpiresAt = nil
	run.UpdatedAt = now
	r.runs[id] = run
	r.releaseCalls++
	r.mutex.Unlock()
	engineTestSignal(r.released)
	return nil
}

func (r *engineRepository) CancelRun(_ context.Context, id int64, now time.Time) (Run, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	run, ok := r.runs[id]
	if !ok {
		return Run{}, ErrNotFound
	}
	r.cancelCalls++
	switch run.Status {
	case StatusQueued:
		run.Status = StatusCanceled
		run.FinishedAt = engineTimePointer(now)
	case StatusRunning:
		run.CancelRequestedAt = engineTimePointer(now)
	}
	run.UpdatedAt = now
	r.runs[id] = run
	return run, nil
}

func (r *engineRepository) MarkRunCanceled(_ context.Context, id int64, owner string, now time.Time) error {
	r.mutex.Lock()
	run, ok := r.runs[id]
	if !ok || run.Status != StatusRunning || run.LeaseOwner != owner {
		r.mutex.Unlock()
		return ErrConflict
	}
	run.Status = StatusCanceled
	run.FinishedAt = engineTimePointer(now)
	run.LeaseOwner = ""
	run.LeaseExpiresAt = nil
	run.UpdatedAt = now
	r.runs[id] = run
	r.markCanceledCalls++
	r.mutex.Unlock()
	engineTestSignal(r.canceled)
	return nil
}

func (r *engineRepository) RecoverExpiredRuns(context.Context, time.Time, time.Duration) (int, error) {
	return 0, nil
}

func (r *engineRepository) ListDueSchedules(context.Context, time.Time, int) ([]Schedule, error) {
	return nil, nil
}

func (r *engineRepository) DeleteTerminalRunsBefore(context.Context, time.Time) (int, error) {
	return 0, nil
}

func (r *engineRepository) DeleteRun(context.Context, int64) error {
	return nil
}

func (r *engineRepository) snapshot(id int64) engineRepositorySnapshot {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return engineRepositorySnapshot{
		run:               r.runs[id],
		progress:          append([]Progress(nil), r.progress...),
		completeCalls:     r.completeCalls,
		retryCalls:        r.retryCalls,
		failCalls:         r.failCalls,
		releaseCalls:      r.releaseCalls,
		cancelCalls:       r.cancelCalls,
		markCanceledCalls: r.markCanceledCalls,
	}
}

func engineTestRun(id int64, taskType string, now time.Time) Run {
	return Run{
		ID:             id,
		Type:           taskType,
		PayloadVersion: 1,
		Payload:        json.RawMessage(`{"subject":"synthetic"}`),
		Trigger:        TriggerManual,
		Status:         StatusQueued,
		MaxAttempts:    DefaultMaxAttempts,
		AvailableAt:    now,
		CreatedBy:      "local-admin",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func newEngineTestManager(t *testing.T, repository Repository, handler Handler, now func() time.Time) *Manager {
	t.Helper()
	registry, err := NewRegistry(handler)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	options := DefaultOptions()
	options.Workers = 1
	options.Retention = time.Hour
	options.PollInterval = time.Hour
	options.ScheduleInterval = time.Hour
	options.LeaseDuration = time.Hour
	options.Heartbeat = time.Minute
	options.ProgressInterval = time.Hour
	options.Now = now
	manager, err := NewManager(repository, registry, options)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	return manager
}

func attachEngineTestContext(t *testing.T, manager *Manager) {
	t.Helper()
	manager.ctx, manager.cancel = context.WithCancelCause(context.Background())
	t.Cleanup(func() {
		manager.cancel(context.Canceled)
	})
}

func startEngineTestManager(t *testing.T, manager *Manager) {
	t.Helper()
	if err := manager.Start(t.Context()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = manager.Shutdown(ctx)
	})
}

func shutdownEngineTestManager(t *testing.T, manager *Manager) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := manager.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func waitForEngineTestSignal(t *testing.T, signal <-chan struct{}, event string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", event)
	}
}

func engineTestSignal(signal chan<- struct{}) {
	select {
	case signal <- struct{}{}:
	default:
	}
}

func engineTimePointer(value time.Time) *time.Time {
	return &value
}
