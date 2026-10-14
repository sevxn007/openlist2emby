package task

import (
	"context"
	"errors"
	"testing"
	"time"
)

type runItemQueryRepository struct {
	Repository
	runID  int64
	filter RunItemFilter
	page   RunItemPage
	calls  int
}

func (r *runItemQueryRepository) ListRunItems(_ context.Context, runID int64, filter RunItemFilter) (RunItemPage, error) {
	r.calls++
	r.runID = runID
	r.filter = filter
	return r.page, nil
}

func TestManagerListRunItemsNormalizesFilters(t *testing.T) {
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	want := RunItemPage{Total: 3, Page: 2, PerPage: 1}
	repository := &runItemQueryRepository{page: want}
	manager := newTestManager(t, repository, now)

	page, err := manager.ListRunItems(t.Context(), 42, RunItemFilter{
		Statuses: []RunItemStatus{RunItemStatusRunning, RunItemStatusRunning, RunItemStatusPending},
		Actions:  []RunItemAction{RunItemActionCopy, RunItemActionCopy},
		Page:     2,
		PerPage:  1,
	})
	if err != nil {
		t.Fatalf("ListRunItems() error = %v", err)
	}
	if page.Total != want.Total || repository.calls != 1 || repository.runID != 42 {
		t.Fatalf("ListRunItems() page/repository = %#v/%#v", page, repository)
	}
	if got := repository.filter.Statuses; len(got) != 2 || got[0] != RunItemStatusRunning || got[1] != RunItemStatusPending {
		t.Fatalf("normalized statuses = %#v", got)
	}
	if got := repository.filter.Actions; len(got) != 1 || got[0] != RunItemActionCopy {
		t.Fatalf("normalized actions = %#v", got)
	}
}

func TestManagerListRunItemsRejectsInvalidFilters(t *testing.T) {
	repository := &runItemQueryRepository{}
	manager := newTestManager(t, repository, time.Now())

	_, err := manager.ListRunItems(t.Context(), 42, RunItemFilter{
		Statuses: []RunItemStatus{"unknown"},
	})
	if err == nil {
		t.Fatal("ListRunItems() error = nil, want invalid status")
	}
	var publicErr *PublicError
	if !errors.As(err, &publicErr) || publicErr.Code != "invalid_item_status" {
		t.Fatalf("ListRunItems() error = %#v", err)
	}
	if repository.calls != 0 {
		t.Fatalf("repository calls = %d, want 0", repository.calls)
	}
}

func TestParseRunItemFilters(t *testing.T) {
	statuses, err := ParseRunItemStatuses(" pending, running,pending ")
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 || statuses[0] != RunItemStatusPending || statuses[1] != RunItemStatusRunning {
		t.Fatalf("statuses = %#v", statuses)
	}
	actions, err := ParseRunItemActions("copy,move,copy")
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 2 || actions[0] != RunItemActionCopy || actions[1] != RunItemActionMove {
		t.Fatalf("actions = %#v", actions)
	}
}
