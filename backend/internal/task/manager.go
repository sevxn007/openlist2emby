package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

type Options struct {
	Workers          int
	Retention        time.Duration
	PollInterval     time.Duration
	ScheduleInterval time.Duration
	LeaseDuration    time.Duration
	Heartbeat        time.Duration
	ProgressInterval time.Duration
	Now              func() time.Time
}

func DefaultOptions() Options {
	return Options{
		Workers:          2,
		Retention:        30 * 24 * time.Hour,
		PollInterval:     time.Second,
		ScheduleInterval: time.Second,
		LeaseDuration:    time.Minute,
		Heartbeat:        15 * time.Second,
		ProgressInterval: 500 * time.Millisecond,
		Now:              time.Now,
	}
}

type Manager struct {
	repository Repository
	registry   *Registry
	options    Options
	parser     cron.Parser
	wake       chan struct{}

	lifecycleMu sync.Mutex
	started     bool
	ctx         context.Context
	cancel      context.CancelCauseFunc
	waitGroup   sync.WaitGroup

	activeMu sync.Mutex
	active   map[int64]context.CancelCauseFunc

	progressMu      sync.Mutex
	progressHistory map[int64][]Progress
}

func NewManager(repository Repository, registry *Registry, options Options) (*Manager, error) {
	if repository == nil {
		return nil, fmt.Errorf("create task manager: repository is required")
	}
	if registry == nil {
		return nil, fmt.Errorf("create task manager: registry is required")
	}
	options = normalizeOptions(options)
	return &Manager{
		repository: repository,
		registry:   registry,
		options:    options,
		parser: cron.NewParser(
			cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow,
		),
		wake:            make(chan struct{}, 1),
		active:          make(map[int64]context.CancelCauseFunc),
		progressHistory: make(map[int64][]Progress),
	}, nil
}

func normalizeOptions(options Options) Options {
	defaults := DefaultOptions()
	if options.Workers <= 0 {
		options.Workers = defaults.Workers
	}
	if options.Retention <= 0 {
		options.Retention = defaults.Retention
	}
	if options.PollInterval <= 0 {
		options.PollInterval = defaults.PollInterval
	}
	if options.ScheduleInterval <= 0 {
		options.ScheduleInterval = defaults.ScheduleInterval
	}
	if options.LeaseDuration <= 0 {
		options.LeaseDuration = defaults.LeaseDuration
	}
	if options.Heartbeat <= 0 || options.Heartbeat >= options.LeaseDuration {
		options.Heartbeat = defaults.Heartbeat
	}
	if options.ProgressInterval <= 0 {
		options.ProgressInterval = defaults.ProgressInterval
	}
	if options.Now == nil {
		options.Now = defaults.Now
	}
	return options
}

func (m *Manager) Types() []Descriptor {
	return m.registry.Descriptors()
}

func (m *Manager) CreateManualRun(ctx context.Context, username string, input ManualRunInput) (PublicRun, error) {
	handler, descriptor, err := m.manualHandler(input.Type)
	if err != nil {
		return PublicRun{}, err
	}
	payload, err := handler.Prepare(ctx, normalizeRawJSON(input.Payload))
	if err != nil {
		return PublicRun{}, err
	}
	run, err := m.repository.CreateRun(ctx, NewRun{
		Type:           descriptor.Type,
		PayloadVersion: descriptor.PayloadVersion,
		Payload:        payload,
		Trigger:        TriggerManual,
		MaxAttempts:    DefaultMaxAttempts,
		AvailableAt:    m.now(),
		CreatedBy:      strings.TrimSpace(username),
	})
	if err != nil {
		return PublicRun{}, err
	}
	m.notifyWorkers()
	return m.publicRun(run), nil
}

func (m *Manager) CreateSchedule(ctx context.Context, username string, input ScheduleInput) (PublicSchedule, error) {
	handler, descriptor, nextRunAt, err := m.prepareSchedule(input)
	if err != nil {
		return PublicSchedule{}, err
	}
	payload, err := handler.Prepare(ctx, normalizeRawJSON(input.Payload))
	if err != nil {
		return PublicSchedule{}, err
	}
	if err := m.requireUniqueScheduleIdentity(ctx, handler, descriptor.Type, payload, 0); err != nil {
		return PublicSchedule{}, err
	}
	now := m.now()
	cronExpression := strings.TrimSpace(input.CronExpression)
	timezone := strings.TrimSpace(input.Timezone)
	enabled := input.Enabled
	if cronExpression == "" {
		timezone = ""
		enabled = false
	}
	schedule, err := m.repository.CreateSchedule(ctx, Schedule{
		Name:           strings.TrimSpace(input.Name),
		Type:           descriptor.Type,
		PayloadVersion: descriptor.PayloadVersion,
		Payload:        payload,
		CronExpression: cronExpression,
		Timezone:       timezone,
		Enabled:        enabled,
		NextRunAt:      nextRunAt,
		CreatedBy:      strings.TrimSpace(username),
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	if err != nil {
		return PublicSchedule{}, err
	}
	return m.publicSchedule(schedule, nil), nil
}

func (m *Manager) UpdateSchedule(ctx context.Context, id int64, input ScheduleInput) (PublicSchedule, error) {
	current, err := m.repository.GetSchedule(ctx, id)
	if err != nil {
		return PublicSchedule{}, err
	}
	handler, descriptor, nextRunAt, err := m.prepareSchedule(input)
	if err != nil {
		return PublicSchedule{}, err
	}
	payload, err := handler.Prepare(ctx, normalizeRawJSON(input.Payload))
	if err != nil {
		return PublicSchedule{}, err
	}
	if err := m.requireUniqueScheduleIdentity(ctx, handler, descriptor.Type, payload, id); err != nil {
		return PublicSchedule{}, err
	}
	cronExpression := strings.TrimSpace(input.CronExpression)
	timezone := strings.TrimSpace(input.Timezone)
	enabled := input.Enabled
	if cronExpression == "" {
		timezone = ""
		enabled = false
	}
	current.Name = strings.TrimSpace(input.Name)
	current.Type = descriptor.Type
	current.PayloadVersion = descriptor.PayloadVersion
	current.Payload = payload
	current.CronExpression = cronExpression
	current.Timezone = timezone
	current.Enabled = enabled
	current.NextRunAt = nextRunAt
	current.UpdatedAt = m.now()
	updated, err := m.repository.UpdateSchedule(ctx, current)
	if err != nil {
		return PublicSchedule{}, err
	}
	if !updated.Enabled {
		if err := m.repository.CancelQueuedScheduleRuns(ctx, updated.ID, m.now()); err != nil {
			return PublicSchedule{}, err
		}
	}
	lastRun, err := m.repository.LastRunForSchedule(ctx, updated.ID)
	if err != nil {
		return PublicSchedule{}, err
	}
	return m.publicSchedule(updated, lastRun), nil
}

func (m *Manager) GetSchedule(ctx context.Context, id int64) (PublicSchedule, error) {
	schedule, err := m.repository.GetSchedule(ctx, id)
	if err != nil {
		return PublicSchedule{}, err
	}
	lastRun, err := m.repository.LastRunForSchedule(ctx, id)
	if err != nil {
		return PublicSchedule{}, err
	}
	return m.publicSchedule(schedule, lastRun), nil
}

func (m *Manager) ListSchedules(ctx context.Context, taskTypes ...string) (SchedulePage, error) {
	schedules, err := m.repository.ListSchedules(ctx)
	if err != nil {
		return SchedulePage{}, err
	}
	filterType := ""
	if len(taskTypes) > 0 {
		filterType = strings.TrimSpace(taskTypes[0])
	}
	if filterType != "" {
		filtered := make([]Schedule, 0, len(schedules))
		for _, schedule := range schedules {
			if schedule.Type == filterType {
				filtered = append(filtered, schedule)
			}
		}
		schedules = filtered
	}
	ids := make([]int64, len(schedules))
	for index, schedule := range schedules {
		ids[index] = schedule.ID
	}
	lastRuns, err := m.repository.LastRunsForSchedules(ctx, ids)
	if err != nil {
		return SchedulePage{}, err
	}
	items := make([]PublicSchedule, 0, len(schedules))
	for _, schedule := range schedules {
		lastRun, ok := lastRuns[schedule.ID]
		if ok {
			items = append(items, m.publicSchedule(schedule, &lastRun))
		} else {
			items = append(items, m.publicSchedule(schedule, nil))
		}
	}
	return SchedulePage{Items: items, Total: len(items)}, nil
}

func (m *Manager) DeleteSchedule(ctx context.Context, id int64) error {
	return m.repository.DeleteSchedule(ctx, id, m.now())
}

func (m *Manager) RunScheduleNow(ctx context.Context, id int64, username string) (PublicRun, error) {
	schedule, err := m.repository.GetSchedule(ctx, id)
	if err != nil {
		return PublicRun{}, err
	}
	handler, ok := m.registry.Handler(schedule.Type)
	if !ok || handler.Descriptor().PayloadVersion != schedule.PayloadVersion {
		return PublicRun{}, ErrTypeUnavailable
	}
	run, err := m.repository.CreateRun(ctx, NewRun{
		ScheduleID:     &schedule.ID,
		Type:           schedule.Type,
		PayloadVersion: schedule.PayloadVersion,
		Payload:        schedule.Payload,
		Trigger:        TriggerRunNow,
		MaxAttempts:    DefaultMaxAttempts,
		AvailableAt:    m.now(),
		CreatedBy:      strings.TrimSpace(username),
	})
	if err != nil {
		return PublicRun{}, err
	}
	m.notifyWorkers()
	return m.publicRun(run), nil
}

func (m *Manager) GetRun(ctx context.Context, id int64) (PublicRun, error) {
	run, err := m.repository.GetRun(ctx, id)
	if err != nil {
		return PublicRun{}, err
	}
	return m.publicRun(run), nil
}

func (m *Manager) ListRuns(ctx context.Context, filter RunFilter) (PublicRunPage, error) {
	page, err := m.repository.ListRuns(ctx, filter)
	if err != nil {
		return PublicRunPage{}, err
	}
	items := make([]PublicRun, 0, len(page.Items))
	for _, run := range page.Items {
		items = append(items, m.publicRun(run))
	}
	return PublicRunPage{Items: items, Total: page.Total, Page: page.Page, PerPage: page.PerPage}, nil
}

func (m *Manager) DeleteRun(ctx context.Context, id int64) error {
	if err := m.repository.DeleteRun(ctx, id); err != nil {
		return err
	}
	m.progressMu.Lock()
	delete(m.progressHistory, id)
	m.progressMu.Unlock()
	return nil
}

// DeleteCompletedRuns clears all succeeded and canceled runs, regardless of list filters.
func (m *Manager) DeleteCompletedRuns(ctx context.Context) (int, error) {
	ids, err := m.repository.DeleteCompletedRuns(ctx)
	if err != nil {
		return 0, err
	}
	m.progressMu.Lock()
	for _, id := range ids {
		delete(m.progressHistory, id)
	}
	m.progressMu.Unlock()
	return len(ids), nil
}

func (m *Manager) ListRunItems(ctx context.Context, runID int64, filter RunItemFilter) (RunItemPage, error) {
	filter, err := normalizeRunItemFilter(filter)
	if err != nil {
		return RunItemPage{}, err
	}
	return m.repository.ListRunItems(ctx, runID, filter)
}

func (m *Manager) CancelRun(ctx context.Context, id int64) (PublicRun, error) {
	run, err := m.repository.GetRun(ctx, id)
	if err != nil {
		return PublicRun{}, err
	}
	if run.Status.Terminal() {
		return PublicRun{}, ErrConflict
	}
	handler, ok := m.registry.Handler(run.Type)
	if !ok || handler.Descriptor().PayloadVersion != run.PayloadVersion {
		return PublicRun{}, ErrTypeUnavailable
	}
	if !handler.Descriptor().Cancelable {
		return PublicRun{}, ErrNotCancelable
	}
	run, err = m.repository.CancelRun(ctx, id, m.now())
	if err != nil {
		return PublicRun{}, err
	}
	if run.Status == StatusRunning {
		m.cancelActiveRun(run.ID, ErrRunCanceled)
	}
	return m.publicRun(run), nil
}

func (m *Manager) RetryRun(ctx context.Context, id int64, username string) (PublicRun, error) {
	previous, err := m.repository.GetRun(ctx, id)
	if err != nil {
		return PublicRun{}, err
	}
	if previous.Status != StatusFailed && previous.Status != StatusCanceled {
		return PublicRun{}, ErrConflict
	}
	handler, ok := m.registry.Handler(previous.Type)
	if !ok {
		return PublicRun{}, ErrTypeUnavailable
	}
	descriptor := handler.Descriptor()
	if previous.ScheduleID == nil && !descriptor.Manual {
		return PublicRun{}, ErrNotManual
	}
	if descriptor.PayloadVersion != previous.PayloadVersion {
		return PublicRun{}, ErrTypeUnavailable
	}
	now := m.now()
	run, err := m.repository.CreateRun(ctx, NewRun{
		ScheduleID:     previous.ScheduleID,
		Type:           previous.Type,
		PayloadVersion: previous.PayloadVersion,
		Payload:        previous.Payload,
		Trigger:        TriggerRetry,
		MaxAttempts:    DefaultMaxAttempts,
		AvailableAt:    now,
		RetryOfRunID:   &previous.ID,
		CreatedBy:      strings.TrimSpace(username),
	})
	if err != nil {
		return PublicRun{}, err
	}
	m.notifyWorkers()
	return m.publicRun(run), nil
}

func (m *Manager) WaitRun(ctx context.Context, id int64, report Reporter) (Run, error) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	lastProgress := Progress{Percent: -1}
	historyIndex := 0
	for {
		if report != nil {
			history := m.runProgressHistory(id)
			for historyIndex < len(history) {
				progress := history[historyIndex]
				historyIndex++
				if progress != lastProgress {
					report(progress)
					lastProgress = progress
				}
			}
		}
		run, err := m.repository.GetRun(ctx, id)
		if err != nil {
			return Run{}, err
		}
		progress := Progress{Stage: run.ProgressStage, Percent: run.ProgressPercent}
		if report != nil && progress != lastProgress {
			report(progress)
			lastProgress = progress
		}
		if run.Status.Terminal() {
			switch run.Status {
			case StatusSucceeded:
				return run, nil
			case StatusCanceled:
				return run, ErrRunCanceled
			default:
				return run, NewPublicError(run.ErrorCode, run.ErrorMessage, normalizeErrorStatus(run.ErrorStatus), false, nil)
			}
		}
		select {
		case <-ctx.Done():
			return Run{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (m *Manager) runProgressHistory(id int64) []Progress {
	m.progressMu.Lock()
	defer m.progressMu.Unlock()
	history := m.progressHistory[id]
	return append([]Progress(nil), history...)
}

func (m *Manager) prepareSchedule(input ScheduleInput) (Handler, Descriptor, *time.Time, error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, Descriptor{}, nil, NewPublicError("invalid_name", "task name is required", http.StatusBadRequest, false, nil)
	}
	if len(strings.TrimSpace(input.Name)) > 120 {
		return nil, Descriptor{}, nil, NewPublicError("invalid_name", "task name is too long", http.StatusBadRequest, false, nil)
	}
	handler, ok := m.registry.Handler(input.Type)
	if !ok {
		return nil, Descriptor{}, nil, ErrTypeUnavailable
	}
	descriptor := handler.Descriptor()
	expression := strings.TrimSpace(input.CronExpression)
	if expression == "" {
		if !descriptor.Manual {
			return nil, Descriptor{}, nil, ErrNotManual
		}
		return handler, descriptor, nil, nil
	}
	if !descriptor.Schedulable {
		return nil, Descriptor{}, nil, ErrNotSchedulable
	}
	next, err := m.nextRun(expression, input.Timezone, m.now())
	if err != nil {
		return nil, Descriptor{}, nil, err
	}
	if !input.Enabled {
		next = nil
	}
	return handler, descriptor, next, nil
}

func (m *Manager) nextRun(expression, timezone string, after time.Time) (*time.Time, error) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return nil, NewPublicError("invalid_cron", "cron expression is required", http.StatusBadRequest, false, ErrInvalidSchedule)
	}
	location, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		return nil, NewPublicError("invalid_timezone", "timezone is invalid", http.StatusBadRequest, false, err)
	}
	schedule, err := m.parser.Parse(expression)
	if err != nil {
		return nil, NewPublicError("invalid_cron", "cron expression is invalid", http.StatusBadRequest, false, err)
	}
	next := schedule.Next(after.In(location)).UTC()
	return &next, nil
}

func (m *Manager) requireUniqueScheduleIdentity(ctx context.Context, handler Handler, taskType string, payload json.RawMessage, exceptID int64) error {
	provider, ok := handler.(ScheduleIdentityProvider)
	if !ok {
		return nil
	}
	identity, ok := provider.ScheduleIdentity(payload)
	if !ok || strings.TrimSpace(identity) == "" {
		return nil
	}
	schedules, err := m.repository.ListSchedules(ctx)
	if err != nil {
		return err
	}
	for _, schedule := range schedules {
		if schedule.ID == exceptID || schedule.Type != taskType {
			continue
		}
		existing, valid := provider.ScheduleIdentity(schedule.Payload)
		if valid && existing == identity {
			return NewPublicError("duplicate_schedule", "a task schedule already exists for this target", http.StatusConflict, false, ErrConflict)
		}
	}
	return nil
}

func (m *Manager) manualHandler(taskType string) (Handler, Descriptor, error) {
	handler, ok := m.registry.Handler(taskType)
	if !ok {
		return nil, Descriptor{}, ErrTypeUnavailable
	}
	descriptor := handler.Descriptor()
	if !descriptor.Manual {
		return nil, Descriptor{}, ErrNotManual
	}
	return handler, descriptor, nil
}

func (m *Manager) publicSchedule(schedule Schedule, lastRun *Run) PublicSchedule {
	view := PublicSchedule{
		ID:             schedule.ID,
		Name:           schedule.Name,
		Type:           schedule.Type,
		PayloadVersion: schedule.PayloadVersion,
		CronExpression: schedule.CronExpression,
		Timezone:       schedule.Timezone,
		Enabled:        schedule.Enabled,
		NextRunAt:      schedule.NextRunAt,
		CreatedBy:      schedule.CreatedBy,
		CreatedAt:      schedule.CreatedAt,
		UpdatedAt:      schedule.UpdatedAt,
	}
	if handler, ok := m.registry.Handler(schedule.Type); ok {
		view.Parameters = handler.PublicSummary(schedule.Payload)
	}
	if lastRun != nil {
		value := m.publicRun(*lastRun)
		view.LastRun = &value
	}
	return view
}

func (m *Manager) publicRun(run Run) PublicRun {
	view := PublicRun{
		ID:             run.ID,
		ScheduleID:     run.ScheduleID,
		Type:           run.Type,
		PayloadVersion: run.PayloadVersion,
		Trigger:        run.Trigger,
		Status:         run.Status,
		Progress: Progress{
			Stage:              run.ProgressStage,
			Percent:            run.ProgressPercent,
			ScannedDirectories: run.ScannedDirectories,
			ScannedFiles:       run.ScannedFiles,
		},
		Attempt:         run.Attempt,
		MaxAttempts:     run.MaxAttempts,
		AvailableAt:     run.AvailableAt,
		ScheduledFor:    run.ScheduledFor,
		StartedAt:       run.StartedAt,
		FinishedAt:      run.FinishedAt,
		CancelRequested: run.CancelRequestedAt != nil,
		RetryOfRunID:    run.RetryOfRunID,
		CreatedBy:       run.CreatedBy,
		CreatedAt:       run.CreatedAt,
		UpdatedAt:       run.UpdatedAt,
	}
	if handler, ok := m.registry.Handler(run.Type); ok {
		view.Subject = handler.PublicSummary(run.Payload)
	}
	if run.ErrorCode != "" || run.ErrorMessage != "" {
		view.Error = &RunError{Code: run.ErrorCode, Message: run.ErrorMessage}
	}
	if len(run.Result) > 0 {
		var result any
		if json.Unmarshal(run.Result, &result) == nil {
			view.Result = result
		}
	}
	return view
}

func normalizeRawJSON(value json.RawMessage) json.RawMessage {
	if len(value) == 0 || string(value) == "null" {
		return json.RawMessage(`{}`)
	}
	return value
}

func (m *Manager) notifyWorkers() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Manager) cancelActiveRun(id int64, cause error) {
	m.activeMu.Lock()
	cancel := m.active[id]
	m.activeMu.Unlock()
	if cancel != nil {
		cancel(cause)
	}
}

func (m *Manager) now() time.Time {
	return m.options.Now().UTC()
}

func normalizeStatusFilter(statuses []Status) ([]Status, error) {
	seen := make(map[Status]struct{}, len(statuses))
	result := make([]Status, 0, len(statuses))
	for _, status := range statuses {
		switch status {
		case StatusQueued, StatusRunning, StatusSucceeded, StatusFailed, StatusCanceled:
		default:
			return nil, NewPublicError("invalid_status", "task status is invalid", http.StatusBadRequest, false, nil)
		}
		if _, ok := seen[status]; ok {
			continue
		}
		seen[status] = struct{}{}
		result = append(result, status)
	}
	return result, nil
}

func ParseStatuses(value string) ([]Status, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	statuses := make([]Status, 0, len(parts))
	for _, part := range parts {
		statuses = append(statuses, Status(strings.TrimSpace(part)))
	}
	return normalizeStatusFilter(statuses)
}

func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}
