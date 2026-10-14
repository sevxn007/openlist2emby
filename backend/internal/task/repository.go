package task

import (
	"context"
	"time"
)

type Repository interface {
	CreateSchedule(context.Context, Schedule) (Schedule, error)
	UpdateSchedule(context.Context, Schedule) (Schedule, error)
	GetSchedule(context.Context, int64) (Schedule, error)
	ListSchedules(context.Context) ([]Schedule, error)
	DeleteSchedule(context.Context, int64, time.Time) error
	CancelQueuedScheduleRuns(context.Context, int64, time.Time) error
	ListDueSchedules(context.Context, time.Time, int) ([]Schedule, error)
	TriggerSchedule(context.Context, Schedule, time.Time, time.Time) (*Run, error)

	CreateRun(context.Context, NewRun) (Run, error)
	GetRun(context.Context, int64) (Run, error)
	ListRuns(context.Context, RunFilter) (RunPage, error)
	DeleteRun(context.Context, int64) error
	DeleteCompletedRuns(context.Context) ([]int64, error)
	CreateRunItems(context.Context, int64, string, []NewRunItem) ([]RunItem, error)
	GetRunItem(context.Context, int64, int64) (RunItem, error)
	ListRunItems(context.Context, int64, RunItemFilter) (RunItemPage, error)
	UpdateRunItem(context.Context, int64, int64, string, RunItemUpdate, time.Time) (RunItem, error)
	LastRunForSchedule(context.Context, int64) (*Run, error)
	LastRunsForSchedules(context.Context, []int64) (map[int64]Run, error)
	ClaimRun(context.Context, string, time.Time, time.Time) (*Run, error)
	HeartbeatRun(context.Context, int64, string, time.Time, time.Time) (bool, error)
	UpdateRunProgress(context.Context, int64, string, Progress, time.Time) error
	CompleteRun(context.Context, int64, string, []byte, time.Time) error
	RetryRun(context.Context, int64, string, time.Time, string, string, int, time.Time) error
	FailRun(context.Context, int64, string, string, string, int, time.Time) error
	ReleaseRun(context.Context, int64, string, time.Time) error
	CancelRun(context.Context, int64, time.Time) (Run, error)
	MarkRunCanceled(context.Context, int64, string, time.Time) error
	RecoverExpiredRuns(context.Context, time.Time, time.Duration) (int, error)
	DeleteTerminalRunsBefore(context.Context, time.Time) (int, error)
}
