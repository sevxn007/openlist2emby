package task

import (
	"net/http"
	"strings"
	"time"
)

type RunItemStatus string

const (
	RunItemStatusPending   RunItemStatus = "pending"
	RunItemStatusRunning   RunItemStatus = "running"
	RunItemStatusSucceeded RunItemStatus = "succeeded"
	RunItemStatusFailed    RunItemStatus = "failed"
	RunItemStatusSkipped   RunItemStatus = "skipped"
	RunItemStatusCanceled  RunItemStatus = "canceled"
)

func (s RunItemStatus) Valid() bool {
	switch s {
	case RunItemStatusPending,
		RunItemStatusRunning,
		RunItemStatusSucceeded,
		RunItemStatusFailed,
		RunItemStatusSkipped,
		RunItemStatusCanceled:
		return true
	default:
		return false
	}
}

type RunItemAction string

const (
	RunItemActionNone   RunItemAction = "none"
	RunItemActionMkdir  RunItemAction = "mkdir"
	RunItemActionCopy   RunItemAction = "copy"
	RunItemActionMove   RunItemAction = "move"
	RunItemActionDelete RunItemAction = "delete"
)

func (a RunItemAction) Valid() bool {
	switch a {
	case RunItemActionNone,
		RunItemActionMkdir,
		RunItemActionCopy,
		RunItemActionMove,
		RunItemActionDelete:
		return true
	default:
		return false
	}
}

type RunItemObjectKind string

const (
	RunItemObjectFile      RunItemObjectKind = "file"
	RunItemObjectDirectory RunItemObjectKind = "directory"
)

func (k RunItemObjectKind) Valid() bool {
	return k == RunItemObjectFile || k == RunItemObjectDirectory
}

type RunItem struct {
	ID                 int64             `json:"id"`
	RunID              int64             `json:"run_id"`
	Position           int               `json:"position"`
	RelativePath       string            `json:"relative_path"`
	ObjectKind         RunItemObjectKind `json:"object_kind"`
	Action             RunItemAction     `json:"action"`
	SourcePath         string            `json:"source_path,omitempty"`
	DestinationPath    string            `json:"destination_path,omitempty"`
	Size               int64             `json:"size"`
	Status             RunItemStatus     `json:"status"`
	ProgressPercent    int               `json:"progress_percent"`
	UpstreamTaskID     string            `json:"upstream_task_id,omitempty"`
	UpstreamTaskState  string            `json:"upstream_task_state,omitempty"`
	UpstreamTaskStatus string            `json:"upstream_task_status,omitempty"`
	ErrorMessage       string            `json:"error,omitempty"`
	StartedAt          *time.Time        `json:"started_at,omitempty"`
	FinishedAt         *time.Time        `json:"finished_at,omitempty"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
}

type NewRunItem struct {
	Position        int
	RelativePath    string
	ObjectKind      RunItemObjectKind
	Action          RunItemAction
	SourcePath      string
	DestinationPath string
	Size            int64
	Status          RunItemStatus
	ProgressPercent int
}

type RunItemUpdate struct {
	ExpectedStatus     *RunItemStatus
	Status             RunItemStatus
	ProgressPercent    int
	UpstreamTaskID     string
	UpstreamTaskState  string
	UpstreamTaskStatus string
	// ErrorMessage must be sanitized before it crosses the repository boundary.
	ErrorMessage string
}

type RunItemFilter struct {
	Statuses []RunItemStatus
	Actions  []RunItemAction
	Page     int
	PerPage  int
}

type RunItemSummary struct {
	Total    int                   `json:"total"`
	ByStatus map[RunItemStatus]int `json:"by_status"`
	ByAction map[RunItemAction]int `json:"by_action"`
}

type RunItemPage struct {
	Items   []RunItem      `json:"items"`
	Total   int            `json:"total"`
	Page    int            `json:"page"`
	PerPage int            `json:"per_page"`
	Summary RunItemSummary `json:"summary"`
}

func ParseRunItemStatuses(value string) ([]RunItemStatus, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	statuses := make([]RunItemStatus, 0, len(parts))
	for _, part := range parts {
		statuses = append(statuses, RunItemStatus(strings.TrimSpace(part)))
	}
	return normalizeRunItemStatuses(statuses)
}

func ParseRunItemActions(value string) ([]RunItemAction, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	actions := make([]RunItemAction, 0, len(parts))
	for _, part := range parts {
		actions = append(actions, RunItemAction(strings.TrimSpace(part)))
	}
	return normalizeRunItemActions(actions)
}

func normalizeRunItemFilter(filter RunItemFilter) (RunItemFilter, error) {
	statuses, err := normalizeRunItemStatuses(filter.Statuses)
	if err != nil {
		return RunItemFilter{}, err
	}
	actions, err := normalizeRunItemActions(filter.Actions)
	if err != nil {
		return RunItemFilter{}, err
	}
	filter.Statuses = statuses
	filter.Actions = actions
	return filter, nil
}

func normalizeRunItemStatuses(statuses []RunItemStatus) ([]RunItemStatus, error) {
	seen := make(map[RunItemStatus]struct{}, len(statuses))
	result := make([]RunItemStatus, 0, len(statuses))
	for _, status := range statuses {
		if !status.Valid() {
			return nil, NewPublicError(
				"invalid_item_status",
				"task item status is invalid",
				http.StatusBadRequest,
				false,
				nil,
			)
		}
		if _, ok := seen[status]; ok {
			continue
		}
		seen[status] = struct{}{}
		result = append(result, status)
	}
	return result, nil
}

func normalizeRunItemActions(actions []RunItemAction) ([]RunItemAction, error) {
	seen := make(map[RunItemAction]struct{}, len(actions))
	result := make([]RunItemAction, 0, len(actions))
	for _, action := range actions {
		if !action.Valid() {
			return nil, NewPublicError(
				"invalid_item_action",
				"task item action is invalid",
				http.StatusBadRequest,
				false,
				nil,
			)
		}
		if _, ok := seen[action]; ok {
			continue
		}
		seen[action] = struct{}{}
		result = append(result, action)
	}
	return result, nil
}
