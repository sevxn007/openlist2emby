package task

import (
	"encoding/json"
	"time"
)

const (
	DefaultMaxAttempts = 3
	DefaultPageSize    = 20
	MaxPageSize        = 100
)

type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"
)

func (s Status) Terminal() bool {
	return s == StatusSucceeded || s == StatusFailed || s == StatusCanceled
}

type Trigger string

const (
	TriggerManual   Trigger = "manual"
	TriggerSchedule Trigger = "schedule"
	TriggerRunNow   Trigger = "run_now"
	TriggerRetry    Trigger = "retry"
)

type Descriptor struct {
	Type           string `json:"type"`
	PayloadVersion int    `json:"payload_version"`
	Manual         bool   `json:"manual"`
	Schedulable    bool   `json:"schedulable"`
	Cancelable     bool   `json:"cancelable"`
}

type Schedule struct {
	ID             int64
	Name           string
	Type           string
	PayloadVersion int
	Payload        json.RawMessage
	CronExpression string
	Timezone       string
	Enabled        bool
	NextRunAt      *time.Time
	CreatedBy      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      *time.Time
}

type Run struct {
	ID                 int64
	ScheduleID         *int64
	Type               string
	PayloadVersion     int
	Payload            json.RawMessage
	Result             json.RawMessage
	Trigger            Trigger
	Status             Status
	ProgressStage      string
	ProgressPercent    int
	ScannedDirectories int
	ScannedFiles       int
	Attempt            int
	MaxAttempts        int
	AvailableAt        time.Time
	ScheduledFor       *time.Time
	StartedAt          *time.Time
	FinishedAt         *time.Time
	ErrorCode          string
	ErrorMessage       string
	ErrorStatus        int
	LeaseOwner         string
	LeaseExpiresAt     *time.Time
	CancelRequestedAt  *time.Time
	RetryOfRunID       *int64
	CreatedBy          string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type NewRun struct {
	ScheduleID     *int64
	Type           string
	PayloadVersion int
	Payload        json.RawMessage
	Trigger        Trigger
	MaxAttempts    int
	AvailableAt    time.Time
	ScheduledFor   *time.Time
	RetryOfRunID   *int64
	CreatedBy      string
}

type ScheduleInput struct {
	Name           string          `json:"name"`
	Type           string          `json:"type"`
	CronExpression string          `json:"cron_expression"`
	Timezone       string          `json:"timezone"`
	Enabled        bool            `json:"enabled"`
	Payload        json.RawMessage `json:"payload"`
}

type ManualRunInput struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type RunFilter struct {
	Statuses []Status
	Type     string
	Page     int
	PerPage  int
}

type RunPage struct {
	Items   []Run
	Total   int
	Page    int
	PerPage int
}

type Progress struct {
	Stage              string `json:"stage"`
	Percent            int    `json:"percent"`
	ScannedDirectories int    `json:"directories_scanned,omitempty"`
	ScannedFiles       int    `json:"files_scanned,omitempty"`
}

type PublicSchedule struct {
	ID             int64      `json:"id"`
	Name           string     `json:"name"`
	Type           string     `json:"type"`
	PayloadVersion int        `json:"payload_version"`
	Parameters     any        `json:"parameters,omitempty"`
	CronExpression string     `json:"cron_expression"`
	Timezone       string     `json:"timezone"`
	Enabled        bool       `json:"enabled"`
	NextRunAt      *time.Time `json:"next_run_at,omitempty"`
	CreatedBy      string     `json:"created_by"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	LastRun        *PublicRun `json:"last_run,omitempty"`
}

type PublicRun struct {
	ID              int64      `json:"id"`
	ScheduleID      *int64     `json:"schedule_id,omitempty"`
	Type            string     `json:"type"`
	PayloadVersion  int        `json:"payload_version"`
	Subject         any        `json:"subject,omitempty"`
	Trigger         Trigger    `json:"trigger"`
	Status          Status     `json:"status"`
	Progress        Progress   `json:"progress"`
	Attempt         int        `json:"attempt"`
	MaxAttempts     int        `json:"max_attempts"`
	AvailableAt     time.Time  `json:"available_at"`
	ScheduledFor    *time.Time `json:"scheduled_for,omitempty"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
	CancelRequested bool       `json:"cancel_requested"`
	RetryOfRunID    *int64     `json:"retry_of_run_id,omitempty"`
	CreatedBy       string     `json:"created_by"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	Error           *RunError  `json:"error,omitempty"`
	Result          any        `json:"result,omitempty"`
}

type RunError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type PublicRunPage struct {
	Items   []PublicRun `json:"items"`
	Total   int         `json:"total"`
	Page    int         `json:"page"`
	PerPage int         `json:"per_page"`
}

type SchedulePage struct {
	Items []PublicSchedule `json:"items"`
	Total int              `json:"total"`
}
