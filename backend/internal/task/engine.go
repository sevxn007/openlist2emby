package task

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const finalizationTimeout = 5 * time.Second

func (m *Manager) Start(parent context.Context) error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	if m.started {
		return nil
	}
	if parent == nil {
		parent = context.Background()
	}
	if _, err := m.repository.RecoverExpiredRuns(parent, m.now(), 30*time.Second); err != nil {
		return fmt.Errorf("recover task runs: %w", err)
	}
	m.ctx, m.cancel = context.WithCancelCause(parent)
	m.started = true
	for worker := 0; worker < m.options.Workers; worker++ {
		owner, err := workerID(worker)
		if err != nil {
			m.cancel(err)
			m.started = false
			return err
		}
		m.waitGroup.Add(1)
		go m.workerLoop(owner)
	}
	m.waitGroup.Add(3)
	go m.schedulerLoop()
	go m.recoveryLoop()
	go m.retentionLoop()
	return nil
}

func (m *Manager) Shutdown(ctx context.Context) error {
	m.lifecycleMu.Lock()
	if !m.started {
		m.lifecycleMu.Unlock()
		return nil
	}
	m.cancel(ErrManagerStopping)
	m.lifecycleMu.Unlock()

	done := make(chan struct{})
	go func() {
		m.waitGroup.Wait()
		close(done)
	}()
	select {
	case <-done:
		m.lifecycleMu.Lock()
		m.started = false
		m.lifecycleMu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) workerLoop(owner string) {
	defer m.waitGroup.Done()
	ticker := time.NewTicker(m.options.PollInterval)
	defer ticker.Stop()
	for {
		if m.ctx.Err() != nil {
			return
		}
		now := m.now()
		run, err := m.repository.ClaimRun(m.ctx, owner, now, now.Add(m.options.LeaseDuration))
		if err != nil {
			if !waitForEngine(m.ctx, ticker.C, m.wake) {
				return
			}
			continue
		}
		if run == nil {
			if !waitForEngine(m.ctx, ticker.C, m.wake) {
				return
			}
			continue
		}
		m.executeRun(owner, *run)
	}
}

func (m *Manager) executeRun(owner string, run Run) {
	handler, ok := m.registry.Handler(run.Type)
	if !ok || handler.Descriptor().PayloadVersion != run.PayloadVersion {
		_ = m.repository.FailRun(
			m.ctx,
			run.ID,
			owner,
			"task_type_unavailable",
			"task type is unavailable",
			http.StatusInternalServerError,
			m.now(),
		)
		return
	}
	runContext, cancel := context.WithCancelCause(m.ctx)
	m.activeMu.Lock()
	m.active[run.ID] = cancel
	m.activeMu.Unlock()
	defer func() {
		m.activeMu.Lock()
		delete(m.active, run.ID)
		m.activeMu.Unlock()
		cancel(nil)
	}()

	heartbeatDone := make(chan struct{})
	var heartbeatWait sync.WaitGroup
	heartbeatWait.Add(1)
	go func() {
		defer heartbeatWait.Done()
		m.heartbeatLoop(runContext, cancel, heartbeatDone, run.ID, owner)
	}()

	execution := newExecution(
		runContext,
		m.repository,
		run.ID,
		owner,
		m.progressReporter(run.ID, owner),
		m.now,
	)
	result, runErr := handler.Run(runContext, run.Payload, execution)
	close(heartbeatDone)
	heartbeatWait.Wait()

	now := m.now()
	persistContext, cancelPersistence := m.persistenceContext()
	defer cancelPersistence()
	if runErr == nil {
		if len(result) == 0 {
			result = json.RawMessage(`{}`)
		}
		if err := m.repository.CompleteRun(persistContext, run.ID, owner, result, now); err == nil {
			m.notifyWorkers()
		} else if errors.Is(err, ErrRunCanceled) {
			m.expireProgressHistory(run.ID)
			return
		}
		m.expireProgressHistory(run.ID)
		return
	}

	cause := context.Cause(runContext)
	if errors.Is(cause, ErrRunCanceled) {
		_ = m.repository.MarkRunCanceled(persistContext, run.ID, owner, now)
		m.expireProgressHistory(run.ID)
		return
	}
	if errors.Is(cause, ErrManagerStopping) || errors.Is(context.Cause(m.ctx), ErrManagerStopping) {
		_ = m.repository.ReleaseRun(persistContext, run.ID, owner, now)
		return
	}
	if cause != nil && !errors.Is(cause, context.Canceled) {
		runErr = cause
	}
	code, message, status, retryable := ErrorDetails(runErr)
	if retryable && run.Attempt < run.MaxAttempts {
		availableAt := now.Add(retryBackoff(run.Attempt))
		if err := m.repository.RetryRun(persistContext, run.ID, owner, availableAt, code, message, status, now); err == nil {
			m.notifyWorkers()
		} else if errors.Is(err, ErrRunCanceled) {
			m.expireProgressHistory(run.ID)
		}
		return
	}
	_ = m.repository.FailRun(persistContext, run.ID, owner, code, message, status, now)
	m.expireProgressHistory(run.ID)
}

func (m *Manager) persistenceContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(m.ctx), finalizationTimeout)
}

func (m *Manager) heartbeatLoop(
	ctx context.Context,
	cancel context.CancelCauseFunc,
	done <-chan struct{},
	runID int64,
	owner string,
) {
	ticker := time.NewTicker(m.options.Heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := m.now()
			requested, err := m.repository.HeartbeatRun(ctx, runID, owner, now.Add(m.options.LeaseDuration), now)
			if err != nil {
				cancel(NewPublicError("heartbeat_failed", "task heartbeat failed", 500, true, err))
				return
			}
			if requested {
				cancel(ErrRunCanceled)
				return
			}
		}
	}
}

func (m *Manager) progressReporter(runID int64, owner string) Reporter {
	var mutex sync.Mutex
	lastProgress := Progress{Percent: -1}
	lastUpdate := time.Time{}
	return func(progress Progress) {
		mutex.Lock()
		defer mutex.Unlock()
		if progress.Percent < 0 {
			progress.Percent = 0
		}
		if progress.Percent > 100 {
			progress.Percent = 100
		}
		now := m.now()
		stageChanged := progress.Stage != lastProgress.Stage
		force := progress.Percent == 100 || lastProgress.Percent < 0
		if progress == lastProgress || (!stageChanged && !force && now.Sub(lastUpdate) < m.options.ProgressInterval) {
			return
		}
		if err := m.repository.UpdateRunProgress(m.ctx, runID, owner, progress, now); err != nil {
			return
		}
		m.progressMu.Lock()
		history := append(m.progressHistory[runID], progress)
		if len(history) > 32 {
			history = history[len(history)-32:]
		}
		m.progressHistory[runID] = history
		m.progressMu.Unlock()
		lastProgress = progress
		lastUpdate = now
	}
}

func (m *Manager) expireProgressHistory(runID int64) {
	time.AfterFunc(time.Minute, func() {
		m.progressMu.Lock()
		delete(m.progressHistory, runID)
		m.progressMu.Unlock()
	})
}

func (m *Manager) schedulerLoop() {
	defer m.waitGroup.Done()
	ticker := time.NewTicker(m.options.ScheduleInterval)
	defer ticker.Stop()
	for {
		m.scheduleDueTasks()
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *Manager) scheduleDueTasks() {
	now := m.now()
	schedules, err := m.repository.ListDueSchedules(m.ctx, now, 100)
	if err != nil {
		return
	}
	for _, schedule := range schedules {
		next, err := m.nextRun(schedule.CronExpression, schedule.Timezone, now)
		if err != nil || next == nil {
			continue
		}
		run, err := m.repository.TriggerSchedule(m.ctx, schedule, *next, now)
		if err == nil && run != nil {
			m.notifyWorkers()
		}
	}
}

func (m *Manager) recoveryLoop() {
	defer m.waitGroup.Done()
	interval := m.options.Heartbeat
	if interval > 30*time.Second {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			count, err := m.repository.RecoverExpiredRuns(m.ctx, m.now(), 30*time.Second)
			if err == nil && count > 0 {
				m.notifyWorkers()
			}
		}
	}
}

func (m *Manager) retentionLoop() {
	defer m.waitGroup.Done()
	cleanup := func() {
		_, _ = m.repository.DeleteTerminalRunsBefore(m.ctx, m.now().Add(-m.options.Retention))
	}
	cleanup()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			cleanup()
		}
	}
}

func retryBackoff(attempt int) time.Duration {
	if attempt <= 1 {
		return 30 * time.Second
	}
	return 2 * time.Minute
}

func waitForEngine(ctx context.Context, ticker <-chan time.Time, wake <-chan struct{}) bool {
	select {
	case <-ctx.Done():
		return false
	case <-ticker:
		return true
	case <-wake:
		return true
	}
}

func workerID(index int) (string, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("create worker id: %w", err)
	}
	return fmt.Sprintf("worker-%d-%s", index, hex.EncodeToString(random)), nil
}
