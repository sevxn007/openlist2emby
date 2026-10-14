package httpapi

import (
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/task"
)

type taskRunItemResponse struct {
	ID                 int64                  `json:"id"`
	RunID              int64                  `json:"run_id"`
	Sequence           int                    `json:"sequence"`
	RelativePath       string                 `json:"relative_path"`
	ObjectType         task.RunItemObjectKind `json:"object_type"`
	Action             task.RunItemAction     `json:"action"`
	SourcePath         string                 `json:"source_path,omitempty"`
	TargetPath         string                 `json:"target_path,omitempty"`
	Size               int64                  `json:"size"`
	Status             task.RunItemStatus     `json:"status"`
	Percent            int                    `json:"percent"`
	UpstreamTaskID     string                 `json:"upstream_task_id,omitempty"`
	UpstreamTaskState  string                 `json:"upstream_task_state,omitempty"`
	UpstreamTaskStatus string                 `json:"upstream_task_status,omitempty"`
	Error              *task.RunError         `json:"error,omitempty"`
	StartedAt          *time.Time             `json:"started_at,omitempty"`
	FinishedAt         *time.Time             `json:"finished_at,omitempty"`
	CreatedAt          time.Time              `json:"created_at"`
	UpdatedAt          time.Time              `json:"updated_at"`
}

type taskRunItemSummaryResponse struct {
	Statuses map[task.RunItemStatus]int `json:"statuses"`
	Actions  map[task.RunItemAction]int `json:"actions"`
}

type taskRunItemPageResponse struct {
	Items   []taskRunItemResponse      `json:"items"`
	Total   int                        `json:"total"`
	Page    int                        `json:"page"`
	PerPage int                        `json:"per_page"`
	Summary taskRunItemSummaryResponse `json:"summary"`
}

func publicTaskRunItemPage(page task.RunItemPage) taskRunItemPageResponse {
	items := make([]taskRunItemResponse, 0, len(page.Items))
	for _, item := range page.Items {
		var itemError *task.RunError
		if item.ErrorMessage != "" {
			itemError = &task.RunError{Code: "item_failed", Message: item.ErrorMessage}
		}
		items = append(items, taskRunItemResponse{
			ID:                 item.ID,
			RunID:              item.RunID,
			Sequence:           item.Position,
			RelativePath:       item.RelativePath,
			ObjectType:         item.ObjectKind,
			Action:             item.Action,
			SourcePath:         item.SourcePath,
			TargetPath:         item.DestinationPath,
			Size:               item.Size,
			Status:             item.Status,
			Percent:            item.ProgressPercent,
			UpstreamTaskID:     item.UpstreamTaskID,
			UpstreamTaskState:  item.UpstreamTaskState,
			UpstreamTaskStatus: item.UpstreamTaskStatus,
			Error:              itemError,
			StartedAt:          item.StartedAt,
			FinishedAt:         item.FinishedAt,
			CreatedAt:          item.CreatedAt,
			UpdatedAt:          item.UpdatedAt,
		})
	}
	return taskRunItemPageResponse{
		Items:   items,
		Total:   page.Total,
		Page:    page.Page,
		PerPage: page.PerPage,
		Summary: taskRunItemSummaryResponse{
			Statuses: page.Summary.ByStatus,
			Actions:  page.Summary.ByAction,
		},
	}
}
