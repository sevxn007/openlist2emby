package task

import (
	"context"
	"time"
)

// RunItemTracker persists item-level progress for the currently leased run.
type RunItemTracker interface {
	Create([]NewRunItem) ([]RunItem, error)
	Get(itemID int64) (RunItem, error)
	List(RunItemFilter) (RunItemPage, error)
	Update(itemID int64, update RunItemUpdate) (RunItem, error)
}

// Execution exposes progress facilities bound to a single task run.
type Execution struct {
	report Reporter
	items  RunItemTracker
}

func newExecution(
	ctx context.Context,
	repository Repository,
	runID int64,
	owner string,
	report Reporter,
	now func() time.Time,
) Execution {
	return Execution{
		report: report,
		items: &repositoryRunItemTracker{
			ctx:        ctx,
			repository: repository,
			runID:      runID,
			owner:      owner,
			now:        now,
		},
	}
}

func (e Execution) Report(progress Progress) {
	if e.report != nil {
		e.report(progress)
	}
}

func (e Execution) Items() RunItemTracker {
	return e.items
}

type repositoryRunItemTracker struct {
	ctx        context.Context
	repository Repository
	runID      int64
	owner      string
	now        func() time.Time
}

func (t *repositoryRunItemTracker) Create(items []NewRunItem) ([]RunItem, error) {
	return t.repository.CreateRunItems(t.ctx, t.runID, t.owner, items)
}

func (t *repositoryRunItemTracker) Get(itemID int64) (RunItem, error) {
	return t.repository.GetRunItem(t.ctx, t.runID, itemID)
}

func (t *repositoryRunItemTracker) List(filter RunItemFilter) (RunItemPage, error) {
	return t.repository.ListRunItems(t.ctx, t.runID, filter)
}

func (t *repositoryRunItemTracker) Update(itemID int64, update RunItemUpdate) (RunItem, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.ctx), finalizationTimeout)
	defer cancel()
	return t.repository.UpdateRunItem(ctx, t.runID, itemID, t.owner, update, t.now())
}
