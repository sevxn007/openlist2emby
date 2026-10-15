package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
	"github.com/sevxn007/openlist2emby/backend/internal/task"
	"gorm.io/gorm"
)

func (s *Store) CreateSchedule(ctx context.Context, schedule task.Schedule) (task.Schedule, error) {
	record := scheduleModel(schedule)
	if err := s.db.WithContext(ctx).Create(&record).Error; err != nil {
		if isUniqueConstraint(err) {
			return task.Schedule{}, task.ErrConflict
		}
		return task.Schedule{}, err
	}
	schedule.ID = record.ID
	return schedule, nil
}

func (s *Store) UpdateSchedule(ctx context.Context, schedule task.Schedule) (task.Schedule, error) {
	nextRunAt := any(gorm.Expr("NULL"))
	if schedule.NextRunAt != nil {
		nextRunAt = newSQLiteTimestamp(*schedule.NextRunAt)
	}
	result := s.db.WithContext(ctx).Model(&taskModel{}).Where("id = ? AND deleted_at IS NULL", schedule.ID).Updates(map[string]any{
		"name": schedule.Name, "type": schedule.Type, "payload_version": schedule.PayloadVersion,
		"payload_json": string(schedule.Payload), "cron_expression": schedule.CronExpression,
		"timezone": schedule.Timezone, "enabled": schedule.Enabled,
		"next_run_at": nextRunAt,
		"updated_at":  newSQLiteTimestamp(schedule.UpdatedAt),
	})
	if result.Error != nil {
		if isUniqueConstraint(result.Error) {
			return task.Schedule{}, task.ErrConflict
		}
		return task.Schedule{}, result.Error
	}
	if result.RowsAffected == 0 {
		return task.Schedule{}, task.ErrConflict
	}
	return s.GetSchedule(ctx, schedule.ID)
}

func (s *Store) GetSchedule(ctx context.Context, id int64) (task.Schedule, error) {
	var record taskModel
	err := s.db.WithContext(ctx).Where("id = ? AND deleted_at IS NULL", id).First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return task.Schedule{}, task.ErrNotFound
	}
	if err != nil {
		return task.Schedule{}, err
	}
	return scheduleDomain(record), nil
}

func (s *Store) ListSchedules(ctx context.Context) ([]task.Schedule, error) {
	var records []taskModel
	if err := s.db.WithContext(ctx).Where("deleted_at IS NULL").Order("created_at DESC, id DESC").Find(&records).Error; err != nil {
		return nil, err
	}
	items := make([]task.Schedule, 0, len(records))
	for _, record := range records {
		items = append(items, scheduleDomain(record))
	}
	return items, nil
}

func (s *Store) DeleteSchedule(ctx context.Context, id int64, now time.Time) error {
	now = now.UTC()
	storedNow := newSQLiteTimestamp(now)
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&taskModel{}).Where("id = ? AND deleted_at IS NULL", id).Updates(map[string]any{
			"enabled": false, "next_run_at": nil, "deleted_at": storedNow, "updated_at": storedNow,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return task.ErrConflict
		}
		return tx.Model(&taskRunModel{}).Where("schedule_id = ? AND status = ?", id, task.StatusQueued).Updates(map[string]any{
			"status": task.StatusCanceled, "finished_at": storedNow, "error_code": "", "error_message": "", "updated_at": storedNow,
		}).Error
	})
}

func (s *Store) CancelQueuedScheduleRuns(ctx context.Context, id int64, now time.Time) error {
	now = now.UTC()
	storedNow := newSQLiteTimestamp(now)
	return s.db.WithContext(ctx).Model(&taskRunModel{}).Where("schedule_id = ? AND status = ?", id, task.StatusQueued).Updates(map[string]any{
		"status": task.StatusCanceled, "finished_at": storedNow, "error_code": "", "error_message": "", "updated_at": storedNow,
	}).Error
}

func (s *Store) ListDueSchedules(ctx context.Context, now time.Time, limit int) ([]task.Schedule, error) {
	if limit <= 0 {
		limit = 100
	}
	var records []taskModel
	if err := s.db.WithContext(ctx).Where("deleted_at IS NULL AND enabled = ? AND next_run_at IS NOT NULL AND next_run_at <= ?", true, newSQLiteTimestamp(now)).Order("next_run_at, id").Limit(limit).Find(&records).Error; err != nil {
		return nil, err
	}
	items := make([]task.Schedule, 0, len(records))
	for _, record := range records {
		items = append(items, scheduleDomain(record))
	}
	return items, nil
}

func (s *Store) TriggerSchedule(ctx context.Context, schedule task.Schedule, nextRun, now time.Time) (*task.Run, error) {
	if schedule.NextRunAt == nil {
		return nil, task.ErrConflict
	}
	now, nextRun = now.UTC(), nextRun.UTC()
	storedNow := newSQLiteTimestamp(now)
	var output *task.Run
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&taskModel{}).Where("id = ? AND enabled = ? AND deleted_at IS NULL AND next_run_at = ?", schedule.ID, true, newSQLiteTimestamp(*schedule.NextRunAt)).Updates(map[string]any{
			"next_run_at": newSQLiteTimestamp(nextRun), "updated_at": storedNow,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		var queued int64
		if err := tx.Model(&taskRunModel{}).Where("schedule_id = ? AND status = ?", schedule.ID, task.StatusQueued).Count(&queued).Error; err != nil {
			return err
		}
		if queued > 0 {
			return nil
		}
		run, err := insertRun(ctx, tx, task.NewRun{ScheduleID: &schedule.ID, Type: schedule.Type, PayloadVersion: schedule.PayloadVersion, Payload: schedule.Payload, Trigger: task.TriggerSchedule, MaxAttempts: task.DefaultMaxAttempts, AvailableAt: now, ScheduledFor: schedule.NextRunAt, CreatedBy: schedule.CreatedBy}, now)
		if err != nil {
			return err
		}
		output = &run
		return nil
	})
	if err != nil {
		return nil, err
	}
	return output, nil
}

func (s *Store) CreateRun(ctx context.Context, input task.NewRun) (task.Run, error) {
	return insertRun(ctx, s.db.WithContext(ctx), input, s.now().UTC())
}

func (s *Store) GetRun(ctx context.Context, id int64) (task.Run, error) {
	var record taskRunModel
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return task.Run{}, task.ErrNotFound
	}
	if err != nil {
		return task.Run{}, err
	}
	return runDomain(record), nil
}

func (s *Store) ListRuns(ctx context.Context, filter task.RunFilter) (task.RunPage, error) {
	filter.Page, filter.PerPage = normalizePagination(filter.Page, filter.PerPage)
	db := s.db.WithContext(ctx).Model(&taskRunModel{})
	if len(filter.Statuses) > 0 {
		db = db.Where("status IN ?", filter.Statuses)
	}
	if filter.Type != "" {
		db = db.Where("type = ?", filter.Type)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return task.RunPage{}, err
	}
	var records []taskRunModel
	if err := db.Order("created_at DESC, id DESC").Limit(filter.PerPage).Offset((filter.Page - 1) * filter.PerPage).Find(&records).Error; err != nil {
		return task.RunPage{}, err
	}
	items := make([]task.Run, 0, len(records))
	for _, record := range records {
		items = append(items, runDomain(record))
	}
	return task.RunPage{Items: items, Total: int(total), Page: filter.Page, PerPage: filter.PerPage}, nil
}

func (s *Store) DeleteRun(ctx context.Context, id int64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record taskRunModel
		if err := tx.Where("id = ?", id).First(&record).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return task.ErrNotFound
			}
			return err
		}
		if task.Status(record.Status) == task.StatusRunning {
			return task.ErrRunActive
		}
		if err := tx.Model(&taskRunModel{}).Where("retry_of_run_id = ?", id).Update("retry_of_run_id", nil).Error; err != nil {
			return err
		}
		result := tx.Where("id = ? AND status <> ?", id, task.StatusRunning).Delete(&taskRunModel{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return task.ErrRunActive
		}
		return nil
	})
}

// DeleteCompletedRuns deletes all succeeded and canceled runs and returns their IDs.
func (s *Store) DeleteCompletedRuns(ctx context.Context) ([]int64, error) {
	var ids []int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		statuses := []task.Status{task.StatusSucceeded, task.StatusCanceled}
		completed := tx.Model(&taskRunModel{}).Where("status IN ?", statuses)
		if err := completed.Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		// Use a subquery so clearing a large history cannot exceed SQLite's bind limit.
		completedIDs := tx.Model(&taskRunModel{}).Select("id").Where("status IN ?", statuses)
		if err := tx.Model(&taskRunModel{}).Where("retry_of_run_id IN (?)", completedIDs).Update("retry_of_run_id", nil).Error; err != nil {
			return err
		}
		return tx.Where("status IN ?", statuses).Delete(&taskRunModel{}).Error
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func (s *Store) LastRunForSchedule(ctx context.Context, scheduleID int64) (*task.Run, error) {
	var record taskRunModel
	err := s.db.WithContext(ctx).Where("schedule_id = ?", scheduleID).Order("created_at DESC, id DESC").First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	run := runDomain(record)
	return &run, nil
}

func (s *Store) LastRunsForSchedules(ctx context.Context, scheduleIDs []int64) (map[int64]task.Run, error) {
	result := make(map[int64]task.Run, len(scheduleIDs))
	if len(scheduleIDs) == 0 {
		return result, nil
	}
	latestIDs := s.db.WithContext(ctx).
		Model(&taskRunModel{}).
		Select("MAX(id)").
		Where("schedule_id IN ?", scheduleIDs).
		Group("schedule_id")
	var records []taskRunModel
	if err := s.db.WithContext(ctx).Where("id IN (?)", latestIDs).Find(&records).Error; err != nil {
		return nil, err
	}
	for _, record := range records {
		if record.ScheduleID != nil {
			result[*record.ScheduleID] = runDomain(record)
		}
	}
	return result, nil
}

func (s *Store) ClaimRun(ctx context.Context, owner string, now, leaseUntil time.Time) (*task.Run, error) {
	now, leaseUntil = now.UTC(), leaseUntil.UTC()
	storedNow := newSQLiteTimestamp(now)
	storedLeaseUntil := newSQLiteTimestamp(leaseUntil)
	var output *task.Run
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		runningSchedules := tx.Model(&taskRunModel{}).
			Select("schedule_id").
			Where("status = ? AND schedule_id IS NOT NULL", task.StatusRunning)
		query := tx.Model(&taskRunModel{}).
			Where("status = ? AND available_at <= ? AND cancel_requested_at IS NULL", task.StatusQueued, storedNow).
			Where("schedule_id IS NULL OR schedule_id NOT IN (?)", runningSchedules)
		// Full scans delete unseen generations. Serialize every scan/import for
		// a library, including manual runs, before any worker reads its roots.
		query = query.Where(`type <> ? OR NOT EXISTS (
			SELECT 1 FROM task_runs AS active
			WHERE active.type = ? AND active.status = ?
			AND json_extract(active.payload_json, '$.library_id') = json_extract(task_runs.payload_json, '$.library_id')
		)`, medialibrary.ScanTaskType, medialibrary.ScanTaskType, task.StatusRunning)
		var candidate taskRunModel
		if err := query.Order("available_at, id").First(&candidate).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		started := candidate.StartedAt
		if started == nil {
			started = &storedNow
		}
		result := tx.Model(&taskRunModel{}).Where("id = ? AND status = ?", candidate.ID, task.StatusQueued).Updates(map[string]any{
			"status": task.StatusRunning, "attempt": candidate.Attempt + 1, "started_at": started,
			"lease_owner": owner, "lease_expires_at": storedLeaseUntil, "updated_at": storedNow,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		candidate.Status = string(task.StatusRunning)
		candidate.Attempt++
		candidate.StartedAt = started
		candidate.LeaseOwner = owner
		candidate.LeaseExpiresAt = &storedLeaseUntil
		candidate.UpdatedAt = storedNow
		run := runDomain(candidate)
		output = &run
		return nil
	})
	if err != nil {
		return nil, err
	}
	return output, nil
}

func (s *Store) HeartbeatRun(ctx context.Context, id int64, owner string, leaseUntil, now time.Time) (bool, error) {
	leaseUntil, now = leaseUntil.UTC(), now.UTC()
	result := s.db.WithContext(ctx).Model(&taskRunModel{}).Where("id = ? AND status = ? AND lease_owner = ?", id, task.StatusRunning, owner).Updates(map[string]any{
		"lease_expires_at": newSQLiteTimestamp(leaseUntil), "updated_at": newSQLiteTimestamp(now),
	})
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 0 {
		return false, task.ErrConflict
	}
	var record taskRunModel
	if err := s.db.WithContext(ctx).Select("cancel_requested_at").Where("id = ?", id).First(&record).Error; err != nil {
		return false, err
	}
	return record.CancelRequestedAt != nil, nil
}

func (s *Store) UpdateRunProgress(ctx context.Context, id int64, owner string, progress task.Progress, now time.Time) error {
	return s.db.WithContext(ctx).Model(&taskRunModel{}).Where("id = ? AND status = ? AND lease_owner = ?", id, task.StatusRunning, owner).Updates(map[string]any{"progress_stage": progress.Stage, "progress_percent": progress.Percent, "progress_directories": progress.ScannedDirectories, "progress_files": progress.ScannedFiles, "updated_at": newSQLiteTimestamp(now)}).Error
}

func (s *Store) CompleteRun(ctx context.Context, id int64, owner string, resultJSON []byte, now time.Time) error {
	now = now.UTC()
	storedNow := newSQLiteTimestamp(now)
	wasCanceled := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		canceled, err := finishCancellationIfRequested(ctx, tx, id, owner, now)
		if err != nil {
			return err
		}
		if canceled {
			wasCanceled = true
			return nil
		}
		result := tx.Model(&taskRunModel{}).Where("id = ? AND status = ? AND lease_owner = ? AND cancel_requested_at IS NULL", id, task.StatusRunning, owner).Updates(map[string]any{"status": task.StatusSucceeded, "result_json": string(resultJSON), "progress_percent": 100, "finished_at": storedNow, "error_code": "", "error_message": "", "error_status": 0, "lease_owner": "", "lease_expires_at": nil, "updated_at": storedNow})
		if result.Error != nil {
			return result.Error
		}
		return requireGormAffected(result)
	})
	if err == nil && wasCanceled {
		return task.ErrRunCanceled
	}
	return err
}

func (s *Store) RetryRun(ctx context.Context, id int64, owner string, availableAt time.Time, code, message string, status int, now time.Time) error {
	now = now.UTC()
	availableAt = availableAt.UTC()
	storedNow := newSQLiteTimestamp(now)
	storedAvailableAt := newSQLiteTimestamp(availableAt)
	wasCanceled := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		canceled, err := finishCancellationIfRequested(ctx, tx, id, owner, now)
		if err != nil {
			return err
		}
		if canceled {
			wasCanceled = true
			return nil
		}
		if err := cancelOtherQueuedRuns(ctx, tx, id, now); err != nil {
			return err
		}
		result := tx.Model(&taskRunModel{}).Where("id = ? AND status = ? AND lease_owner = ?", id, task.StatusRunning, owner).Updates(map[string]any{"status": task.StatusQueued, "available_at": storedAvailableAt, "error_code": code, "error_message": message, "error_status": status, "lease_owner": "", "lease_expires_at": nil, "cancel_requested_at": nil, "progress_stage": "", "progress_percent": 0, "progress_directories": 0, "progress_files": 0, "updated_at": storedNow})
		if result.Error != nil {
			return result.Error
		}
		return requireGormAffected(result)
	})
	if err == nil && wasCanceled {
		return task.ErrRunCanceled
	}
	return err
}

func (s *Store) FailRun(ctx context.Context, id int64, owner, code, message string, status int, now time.Time) error {
	now = now.UTC()
	storedNow := newSQLiteTimestamp(now)
	wasCanceled := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		canceled, err := finishCancellationIfRequested(ctx, tx, id, owner, now)
		if err != nil {
			return err
		}
		if canceled {
			wasCanceled = true
			return nil
		}
		result := tx.Model(&taskRunModel{}).Where("id = ? AND status = ? AND lease_owner = ? AND cancel_requested_at IS NULL", id, task.StatusRunning, owner).Updates(map[string]any{"status": task.StatusFailed, "error_code": code, "error_message": message, "error_status": status, "finished_at": storedNow, "lease_owner": "", "lease_expires_at": nil, "updated_at": storedNow})
		if result.Error != nil {
			return result.Error
		}
		return requireGormAffected(result)
	})
	if err == nil && wasCanceled {
		return task.ErrRunCanceled
	}
	return err
}

func (s *Store) ReleaseRun(ctx context.Context, id int64, owner string, now time.Time) error {
	now = now.UTC()
	storedNow := newSQLiteTimestamp(now)
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		canceled, err := finishCancellationIfRequested(ctx, tx, id, owner, now)
		if err != nil {
			return err
		}
		if canceled {
			return nil
		}
		if err := cancelOtherQueuedRuns(ctx, tx, id, now); err != nil {
			return err
		}
		var current taskRunModel
		if err := tx.Select("attempt").Where("id = ?", id).First(&current).Error; err != nil {
			return err
		}
		attempt := current.Attempt
		if attempt > 0 {
			attempt--
		}
		result := tx.Model(&taskRunModel{}).Where("id = ? AND status = ? AND lease_owner = ?", id, task.StatusRunning, owner).Updates(map[string]any{"status": task.StatusQueued, "available_at": storedNow, "attempt": attempt, "lease_owner": "", "lease_expires_at": nil, "cancel_requested_at": nil, "progress_stage": "", "progress_percent": 0, "progress_directories": 0, "progress_files": 0, "updated_at": storedNow})
		if result.Error != nil {
			return result.Error
		}
		return requireGormAffected(result)
	})
}

func (s *Store) CancelRun(ctx context.Context, id int64, now time.Time) (task.Run, error) {
	now = now.UTC()
	storedNow := newSQLiteTimestamp(now)
	var output task.Run
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record taskRunModel
		if err := tx.Where("id = ?", id).First(&record).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return task.ErrNotFound
			}
			return err
		}
		switch task.Status(record.Status) {
		case task.StatusQueued:
			if err := tx.Model(&taskRunModel{}).Where("id = ? AND status = ?", id, task.StatusQueued).Updates(map[string]any{"status": task.StatusCanceled, "finished_at": storedNow, "updated_at": storedNow}).Error; err != nil {
				return err
			}
		case task.StatusRunning:
			if err := tx.Model(&taskRunModel{}).Where("id = ? AND status = ?", id, task.StatusRunning).Updates(map[string]any{"cancel_requested_at": storedNow, "updated_at": storedNow}).Error; err != nil {
				return err
			}
		default:
			return task.ErrConflict
		}
		if err := tx.Where("id = ?", id).First(&record).Error; err != nil {
			return err
		}
		output = runDomain(record)
		return nil
	})
	if err != nil {
		return task.Run{}, err
	}
	return output, nil
}

func (s *Store) MarkRunCanceled(ctx context.Context, id int64, owner string, now time.Time) error {
	storedNow := newSQLiteTimestamp(now)
	result := s.db.WithContext(ctx).Model(&taskRunModel{}).Where("id = ? AND status = ? AND lease_owner = ?", id, task.StatusRunning, owner).Updates(map[string]any{"status": task.StatusCanceled, "finished_at": storedNow, "error_code": "", "error_message": "", "lease_owner": "", "lease_expires_at": nil, "updated_at": storedNow})
	if result.Error != nil {
		return result.Error
	}
	return requireGormAffected(result)
}

func (s *Store) RecoverExpiredRuns(ctx context.Context, now time.Time, retryDelay time.Duration) (int, error) {
	now = now.UTC()
	storedNow := newSQLiteTimestamp(now)
	var expired []taskRunModel
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?", task.StatusRunning, storedNow).Find(&expired).Error; err != nil {
			return err
		}
		for _, run := range expired {
			var result *gorm.DB
			if run.CancelRequestedAt != nil {
				result = tx.Model(&taskRunModel{}).Where("id = ? AND status = ?", run.ID, task.StatusRunning).Updates(map[string]any{"status": task.StatusCanceled, "finished_at": storedNow, "error_code": "", "error_message": "", "lease_owner": "", "lease_expires_at": nil, "updated_at": storedNow})
			} else if run.Attempt < run.MaxAttempts {
				if err := cancelOtherQueuedRuns(ctx, tx, run.ID, now); err != nil {
					return err
				}
				result = tx.Model(&taskRunModel{}).Where("id = ? AND status = ?", run.ID, task.StatusRunning).Updates(map[string]any{"status": task.StatusQueued, "available_at": newSQLiteTimestamp(now.Add(retryDelay)), "error_code": "worker_interrupted", "error_message": "task worker was interrupted", "error_status": 500, "lease_owner": "", "lease_expires_at": nil, "cancel_requested_at": nil, "progress_stage": "", "progress_percent": 0, "progress_directories": 0, "progress_files": 0, "updated_at": storedNow})
			} else {
				result = tx.Model(&taskRunModel{}).Where("id = ? AND status = ?", run.ID, task.StatusRunning).Updates(map[string]any{"status": task.StatusFailed, "finished_at": storedNow, "error_code": "worker_interrupted", "error_message": "task worker was interrupted", "error_status": 500, "lease_owner": "", "lease_expires_at": nil, "updated_at": storedNow})
			}
			if result.Error != nil {
				return result.Error
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(expired), nil
}

func (s *Store) DeleteTerminalRunsBefore(ctx context.Context, before time.Time) (int, error) {
	var deleted int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ids []int64
		if err := tx.Model(&taskRunModel{}).Where("status IN ? AND finished_at IS NOT NULL AND finished_at < ?", []task.Status{task.StatusSucceeded, task.StatusFailed, task.StatusCanceled}, newSQLiteTimestamp(before)).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if err := tx.Model(&taskRunModel{}).Where("retry_of_run_id IN ?", ids).Update("retry_of_run_id", nil).Error; err != nil {
			return err
		}
		result := tx.Where("id IN ?", ids).Delete(&taskRunModel{})
		deleted = result.RowsAffected
		return result.Error
	})
	if err != nil {
		return 0, err
	}
	return int(deleted), nil
}

func cancelOtherQueuedRuns(ctx context.Context, tx *gorm.DB, runID int64, now time.Time) error {
	var run taskRunModel
	if err := tx.Select("schedule_id").Where("id = ?", runID).First(&run).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return task.ErrNotFound
		}
		return err
	}
	if run.ScheduleID == nil {
		return nil
	}
	storedNow := newSQLiteTimestamp(now)
	return tx.Model(&taskRunModel{}).Where("schedule_id = ? AND id <> ? AND status = ?", *run.ScheduleID, runID, task.StatusQueued).Updates(map[string]any{"status": task.StatusCanceled, "finished_at": storedNow, "error_code": "coalesced", "error_message": "task run was coalesced with an interrupted run", "updated_at": storedNow}).Error
}

func finishCancellationIfRequested(ctx context.Context, tx *gorm.DB, runID int64, owner string, now time.Time) (bool, error) {
	storedNow := newSQLiteTimestamp(now)
	result := tx.Model(&taskRunModel{}).Where("id = ? AND status = ? AND lease_owner = ? AND cancel_requested_at IS NOT NULL", runID, task.StatusRunning, owner).Updates(map[string]any{"status": task.StatusCanceled, "finished_at": storedNow, "error_code": "", "error_message": "", "lease_owner": "", "lease_expires_at": nil, "updated_at": storedNow})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func scheduleModel(value task.Schedule) taskModel {
	return taskModel{ID: value.ID, Name: value.Name, Type: value.Type, PayloadVersion: value.PayloadVersion, PayloadJSON: string(value.Payload), CronExpression: value.CronExpression, Timezone: value.Timezone, Enabled: value.Enabled, NextRunAt: newNullableSQLiteTimestamp(value.NextRunAt), CreatedBy: value.CreatedBy, CreatedAt: newSQLiteTimestamp(value.CreatedAt), UpdatedAt: newSQLiteTimestamp(value.UpdatedAt), DeletedAt: newNullableSQLiteTimestamp(value.DeletedAt)}
}

func scheduleDomain(value taskModel) task.Schedule {
	return task.Schedule{ID: value.ID, Name: value.Name, Type: value.Type, PayloadVersion: value.PayloadVersion, Payload: json.RawMessage(value.PayloadJSON), CronExpression: value.CronExpression, Timezone: value.Timezone, Enabled: value.Enabled, NextRunAt: timeFromNullableSQLiteTimestamp(value.NextRunAt), CreatedBy: value.CreatedBy, CreatedAt: value.CreatedAt.Time(), UpdatedAt: value.UpdatedAt.Time(), DeletedAt: timeFromNullableSQLiteTimestamp(value.DeletedAt)}
}

func runDomain(value taskRunModel) task.Run {
	result := task.Run{ID: value.ID, ScheduleID: value.ScheduleID, Type: value.Type, PayloadVersion: value.PayloadVersion, Payload: json.RawMessage(value.PayloadJSON), Trigger: task.Trigger(value.Trigger), Status: task.Status(value.Status), ProgressStage: value.ProgressStage, ProgressPercent: value.ProgressPercent, ScannedDirectories: value.ProgressDirectories, ScannedFiles: value.ProgressFiles, Attempt: value.Attempt, MaxAttempts: value.MaxAttempts, AvailableAt: value.AvailableAt.Time(), ScheduledFor: timeFromNullableSQLiteTimestamp(value.ScheduledFor), StartedAt: timeFromNullableSQLiteTimestamp(value.StartedAt), FinishedAt: timeFromNullableSQLiteTimestamp(value.FinishedAt), ErrorCode: value.ErrorCode, ErrorMessage: value.ErrorMessage, ErrorStatus: value.ErrorStatus, LeaseOwner: value.LeaseOwner, LeaseExpiresAt: timeFromNullableSQLiteTimestamp(value.LeaseExpiresAt), CancelRequestedAt: timeFromNullableSQLiteTimestamp(value.CancelRequestedAt), RetryOfRunID: value.RetryOfRunID, CreatedBy: value.CreatedBy, CreatedAt: value.CreatedAt.Time(), UpdatedAt: value.UpdatedAt.Time()}
	if value.ResultJSON != "" {
		result.Result = json.RawMessage(value.ResultJSON)
	}
	return result
}

func insertRun(ctx context.Context, db *gorm.DB, input task.NewRun, now time.Time) (task.Run, error) {
	if input.MaxAttempts <= 0 {
		input.MaxAttempts = task.DefaultMaxAttempts
	}
	if input.AvailableAt.IsZero() {
		input.AvailableAt = now
	}
	record := taskRunModel{ScheduleID: input.ScheduleID, Type: input.Type, PayloadVersion: input.PayloadVersion, PayloadJSON: string(input.Payload), Trigger: string(input.Trigger), Status: string(task.StatusQueued), MaxAttempts: input.MaxAttempts, AvailableAt: newSQLiteTimestamp(input.AvailableAt), ScheduledFor: newNullableSQLiteTimestamp(input.ScheduledFor), RetryOfRunID: input.RetryOfRunID, CreatedBy: input.CreatedBy, CreatedAt: newSQLiteTimestamp(now), UpdatedAt: newSQLiteTimestamp(now)}
	if err := db.WithContext(ctx).Create(&record).Error; err != nil {
		if isUniqueConstraint(err) {
			return task.Run{}, task.ErrConflict
		}
		return task.Run{}, err
	}
	return runDomain(record), nil
}

func requireGormAffected(result *gorm.DB) error {
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return task.ErrConflict
	}
	return nil
}

func normalizePagination(page, perPage int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if perPage <= 0 {
		perPage = task.DefaultPageSize
	}
	if perPage > task.MaxPageSize {
		perPage = task.MaxPageSize
	}
	return page, perPage
}
