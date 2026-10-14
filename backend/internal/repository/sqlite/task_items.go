package sqlite

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/task"
	"gorm.io/gorm"
)

func (s *Store) CreateRunItems(ctx context.Context, runID int64, owner string, inputs []task.NewRunItem) ([]task.RunItem, error) {
	if runID <= 0 {
		return nil, fmt.Errorf("create task run items: run ID must be positive")
	}
	if strings.TrimSpace(owner) == "" {
		return nil, fmt.Errorf("create task run items: lease owner is required")
	}
	normalized := make([]task.NewRunItem, len(inputs))
	positions := make(map[int]struct{}, len(inputs))
	for index, input := range inputs {
		item, err := normalizeNewRunItem(input)
		if err != nil {
			return nil, fmt.Errorf("create task run item %d: %w", index, err)
		}
		if _, exists := positions[item.Position]; exists {
			return nil, fmt.Errorf("create task run items: duplicate position %d", item.Position)
		}
		positions[item.Position] = struct{}{}
		normalized[index] = item
	}

	now := s.now().UTC()
	items := make([]task.RunItem, len(normalized))
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireOwnedRunningRun(tx, runID, owner); err != nil {
			return err
		}
		records := make([]taskRunItemModel, len(normalized))
		for index, input := range normalized {
			items[index] = newRunItem(runID, input, now)
			records[index] = runItemModel(items[index])
		}
		if len(records) == 0 {
			return nil
		}
		if err := tx.Create(&records).Error; err != nil {
			if isUniqueConstraint(err) {
				return task.ErrConflict
			}
			return err
		}
		for index := range records {
			items[index].ID = records[index].ID
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) GetRunItem(ctx context.Context, runID, itemID int64) (task.RunItem, error) {
	var record taskRunItemModel
	err := s.db.WithContext(ctx).Where("run_id = ? AND id = ?", runID, itemID).First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return task.RunItem{}, task.ErrNotFound
	}
	if err != nil {
		return task.RunItem{}, err
	}
	return runItemDomain(record), nil
}

func (s *Store) ListRunItems(ctx context.Context, runID int64, filter task.RunItemFilter) (task.RunItemPage, error) {
	if runID <= 0 {
		return task.RunItemPage{}, task.ErrNotFound
	}
	if err := validateRunItemFilter(filter); err != nil {
		return task.RunItemPage{}, err
	}
	filter.Page, filter.PerPage = normalizePagination(filter.Page, filter.PerPage)

	var page task.RunItemPage
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireRunExists(tx, runID); err != nil {
			return err
		}
		query := filterRunItems(tx.Model(&taskRunItemModel{}).Where("run_id = ?", runID), filter)
		var total int64
		if err := query.Count(&total).Error; err != nil {
			return err
		}
		var records []taskRunItemModel
		if err := query.Order("position, id").Limit(filter.PerPage).Offset((filter.Page - 1) * filter.PerPage).Find(&records).Error; err != nil {
			return err
		}
		items := make([]task.RunItem, 0, len(records))
		for _, record := range records {
			items = append(items, runItemDomain(record))
		}
		summary, err := summarizeRunItems(tx, runID)
		if err != nil {
			return err
		}
		page = task.RunItemPage{Items: items, Total: int(total), Page: filter.Page, PerPage: filter.PerPage, Summary: summary}
		return nil
	})
	if err != nil {
		return task.RunItemPage{}, err
	}
	return page, nil
}

func (s *Store) UpdateRunItem(ctx context.Context, runID, itemID int64, owner string, update task.RunItemUpdate, now time.Time) (task.RunItem, error) {
	if runID <= 0 || itemID <= 0 {
		return task.RunItem{}, task.ErrNotFound
	}
	if strings.TrimSpace(owner) == "" {
		return task.RunItem{}, fmt.Errorf("update task run item: lease owner is required")
	}
	if !update.Status.Valid() {
		return task.RunItem{}, fmt.Errorf("update task run item: invalid status %q", update.Status)
	}
	if update.ExpectedStatus != nil && !update.ExpectedStatus.Valid() {
		return task.RunItem{}, fmt.Errorf("update task run item: invalid expected status %q", *update.ExpectedStatus)
	}
	if update.ProgressPercent < 0 || update.ProgressPercent > 100 {
		return task.RunItem{}, fmt.Errorf("update task run item: progress must be between 0 and 100")
	}
	now = now.UTC()
	if now.IsZero() {
		return task.RunItem{}, fmt.Errorf("update task run item: timestamp is required")
	}

	var item task.RunItem
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireOwnedRunningRun(tx, runID, owner); err != nil {
			return err
		}
		var record taskRunItemModel
		if err := tx.Where("run_id = ? AND id = ?", runID, itemID).First(&record).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return task.ErrNotFound
			}
			return err
		}
		item = runItemDomain(record)
		if update.ExpectedStatus != nil && item.Status != *update.ExpectedStatus {
			return task.ErrConflict
		}
		previousStatus := item.Status
		applyRunItemUpdate(&item, update, now)
		result := tx.Model(&taskRunItemModel{}).Where("run_id = ? AND id = ? AND status = ?", runID, itemID, previousStatus).Updates(map[string]any{
			"status": item.Status, "progress_percent": item.ProgressPercent,
			"upstream_task_id": item.UpstreamTaskID, "upstream_task_state": item.UpstreamTaskState,
			"upstream_task_status": item.UpstreamTaskStatus, "error_message": item.ErrorMessage,
			"started_at": newNullableSQLiteTimestamp(item.StartedAt), "finished_at": newNullableSQLiteTimestamp(item.FinishedAt),
			"updated_at": newSQLiteTimestamp(item.UpdatedAt),
		})
		if result.Error != nil {
			return result.Error
		}
		return requireGormAffected(result)
	})
	if err != nil {
		return task.RunItem{}, err
	}
	return item, nil
}

func normalizeNewRunItem(item task.NewRunItem) (task.NewRunItem, error) {
	if item.Position < 0 {
		return task.NewRunItem{}, fmt.Errorf("position must not be negative")
	}
	if item.RelativePath == "" || strings.HasPrefix(item.RelativePath, "/") {
		return task.NewRunItem{}, fmt.Errorf("relative path must be non-empty and relative")
	}
	if !item.ObjectKind.Valid() {
		return task.NewRunItem{}, fmt.Errorf("invalid object kind %q", item.ObjectKind)
	}
	if item.Action == "" {
		item.Action = task.RunItemActionNone
	}
	if !item.Action.Valid() {
		return task.NewRunItem{}, fmt.Errorf("invalid action %q", item.Action)
	}
	if item.Status == "" {
		item.Status = task.RunItemStatusPending
	}
	if !item.Status.Valid() {
		return task.NewRunItem{}, fmt.Errorf("invalid status %q", item.Status)
	}
	if item.Size < 0 {
		return task.NewRunItem{}, fmt.Errorf("size must not be negative")
	}
	if item.ProgressPercent < 0 || item.ProgressPercent > 100 {
		return task.NewRunItem{}, fmt.Errorf("progress must be between 0 and 100")
	}
	if item.Status == task.RunItemStatusSucceeded || item.Status == task.RunItemStatusSkipped {
		item.ProgressPercent = 100
	}
	return item, nil
}

func newRunItem(runID int64, input task.NewRunItem, now time.Time) task.RunItem {
	item := task.RunItem{
		RunID: runID, Position: input.Position, RelativePath: input.RelativePath,
		ObjectKind: input.ObjectKind, Action: input.Action, SourcePath: input.SourcePath,
		DestinationPath: input.DestinationPath, Size: input.Size, Status: input.Status,
		ProgressPercent: input.ProgressPercent, CreatedAt: now, UpdatedAt: now,
	}
	applyRunItemTimestamps(&item, now)
	return item
}

func applyRunItemUpdate(item *task.RunItem, update task.RunItemUpdate, now time.Time) {
	item.Status = update.Status
	item.ProgressPercent = update.ProgressPercent
	if item.Status == task.RunItemStatusSucceeded || item.Status == task.RunItemStatusSkipped {
		item.ProgressPercent = 100
	}
	item.UpstreamTaskID = update.UpstreamTaskID
	item.UpstreamTaskState = update.UpstreamTaskState
	item.UpstreamTaskStatus = update.UpstreamTaskStatus
	item.ErrorMessage = update.ErrorMessage
	item.UpdatedAt = now
	applyRunItemTimestamps(item, now)
}

func applyRunItemTimestamps(item *task.RunItem, now time.Time) {
	switch item.Status {
	case task.RunItemStatusPending:
		item.StartedAt = nil
		item.FinishedAt = nil
	case task.RunItemStatusRunning:
		if item.StartedAt == nil {
			item.StartedAt = timePointerValue(now)
		}
		item.FinishedAt = nil
	case task.RunItemStatusSkipped:
		if item.FinishedAt == nil {
			item.FinishedAt = timePointerValue(now)
		}
	case task.RunItemStatusSucceeded, task.RunItemStatusFailed, task.RunItemStatusCanceled:
		if item.StartedAt == nil {
			item.StartedAt = timePointerValue(now)
		}
		if item.FinishedAt == nil {
			item.FinishedAt = timePointerValue(now)
		}
	}
}

func validateRunItemFilter(filter task.RunItemFilter) error {
	for _, status := range filter.Statuses {
		if !status.Valid() {
			return fmt.Errorf("invalid task run item status %q", status)
		}
	}
	for _, action := range filter.Actions {
		if !action.Valid() {
			return fmt.Errorf("invalid task run item action %q", action)
		}
	}
	return nil
}

func filterRunItems(db *gorm.DB, filter task.RunItemFilter) *gorm.DB {
	if len(filter.Statuses) > 0 {
		db = db.Where("status IN ?", filter.Statuses)
	}
	if len(filter.Actions) > 0 {
		db = db.Where("action IN ?", filter.Actions)
	}
	return db
}

func summarizeRunItems(db *gorm.DB, runID int64) (task.RunItemSummary, error) {
	summary := task.RunItemSummary{
		ByStatus: map[task.RunItemStatus]int{
			task.RunItemStatusPending: 0, task.RunItemStatusRunning: 0,
			task.RunItemStatusSucceeded: 0, task.RunItemStatusFailed: 0,
			task.RunItemStatusSkipped: 0, task.RunItemStatusCanceled: 0,
		},
		ByAction: map[task.RunItemAction]int{
			task.RunItemActionNone: 0, task.RunItemActionMkdir: 0,
			task.RunItemActionCopy: 0, task.RunItemActionMove: 0, task.RunItemActionDelete: 0,
		},
	}
	type groupCount struct {
		Status string
		Action string
		Count  int
	}
	var groups []groupCount
	if err := db.Model(&taskRunItemModel{}).Select("status, action, count(*) AS count").Where("run_id = ?", runID).Group("status, action").Find(&groups).Error; err != nil {
		return task.RunItemSummary{}, err
	}
	for _, group := range groups {
		summary.Total += group.Count
		summary.ByStatus[task.RunItemStatus(group.Status)] += group.Count
		summary.ByAction[task.RunItemAction(group.Action)] += group.Count
	}
	return summary, nil
}

func requireOwnedRunningRun(db *gorm.DB, runID int64, owner string) error {
	var record taskRunModel
	err := db.Select("status", "lease_owner").Where("id = ?", runID).First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return task.ErrNotFound
	}
	if err != nil {
		return err
	}
	if task.Status(record.Status) != task.StatusRunning || record.LeaseOwner != owner {
		return task.ErrConflict
	}
	return nil
}

func requireRunExists(db *gorm.DB, runID int64) error {
	var count int64
	if err := db.Model(&taskRunModel{}).Where("id = ?", runID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return task.ErrNotFound
	}
	return nil
}

func runItemModel(value task.RunItem) taskRunItemModel {
	return taskRunItemModel{
		ID: value.ID, RunID: value.RunID, Position: value.Position, RelativePath: value.RelativePath,
		ObjectKind: string(value.ObjectKind), Action: string(value.Action), SourcePath: value.SourcePath,
		DestinationPath: value.DestinationPath, SizeBytes: value.Size, Status: string(value.Status),
		ProgressPercent: value.ProgressPercent, UpstreamTaskID: value.UpstreamTaskID,
		UpstreamTaskState: value.UpstreamTaskState, UpstreamTaskStatus: value.UpstreamTaskStatus,
		ErrorMessage: value.ErrorMessage, StartedAt: newNullableSQLiteTimestamp(value.StartedAt),
		FinishedAt: newNullableSQLiteTimestamp(value.FinishedAt),
		CreatedAt:  newSQLiteTimestamp(value.CreatedAt), UpdatedAt: newSQLiteTimestamp(value.UpdatedAt),
	}
}

func runItemDomain(value taskRunItemModel) task.RunItem {
	return task.RunItem{
		ID: value.ID, RunID: value.RunID, Position: value.Position, RelativePath: value.RelativePath,
		ObjectKind: task.RunItemObjectKind(value.ObjectKind), Action: task.RunItemAction(value.Action),
		SourcePath: value.SourcePath, DestinationPath: value.DestinationPath, Size: value.SizeBytes,
		Status: task.RunItemStatus(value.Status), ProgressPercent: value.ProgressPercent,
		UpstreamTaskID: value.UpstreamTaskID, UpstreamTaskState: value.UpstreamTaskState,
		UpstreamTaskStatus: value.UpstreamTaskStatus, ErrorMessage: value.ErrorMessage,
		StartedAt:  timeFromNullableSQLiteTimestamp(value.StartedAt),
		FinishedAt: timeFromNullableSQLiteTimestamp(value.FinishedAt),
		CreatedAt:  value.CreatedAt.Time(), UpdatedAt: value.UpdatedAt.Time(),
	}
}

func timePointerValue(value time.Time) *time.Time { return &value }
