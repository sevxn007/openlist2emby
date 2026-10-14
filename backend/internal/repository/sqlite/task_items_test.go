package sqlite

import (
	"errors"
	"testing"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/task"
)

func TestTaskRunItemsCreateListAndUpdate(t *testing.T) {
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	store := newTaskTestStore(t, now)
	run := createTaskTestRun(t, store, now, "fixture.items", "local-admin")

	inputs := []task.NewRunItem{
		{
			Position:        2,
			RelativePath:    "existing.flac",
			ObjectKind:      task.RunItemObjectFile,
			Action:          task.RunItemActionNone,
			SourcePath:      "/A/existing.flac",
			DestinationPath: "/B/existing.flac",
			Size:            300,
			Status:          task.RunItemStatusSkipped,
		},
		{
			Position:        0,
			RelativePath:    "album/track.flac",
			ObjectKind:      task.RunItemObjectFile,
			Action:          task.RunItemActionCopy,
			SourcePath:      "/A/album/track.flac",
			DestinationPath: "/B/album/track.flac",
			Size:            100,
		},
		{
			Position:        1,
			RelativePath:    "album",
			ObjectKind:      task.RunItemObjectDirectory,
			Action:          task.RunItemActionMkdir,
			DestinationPath: "/B/album",
			Status:          task.RunItemStatusPending,
		},
	}
	if _, err := store.CreateRunItems(t.Context(), run.ID, "worker-1", inputs); !errors.Is(err, task.ErrConflict) {
		t.Fatalf("CreateRunItems() before claim error = %v, want ErrConflict", err)
	}
	claimed, err := store.ClaimRun(t.Context(), "worker-1", now, now.Add(time.Minute))
	if err != nil || claimed == nil || claimed.ID != run.ID {
		t.Fatalf("ClaimRun() = %#v, %v", claimed, err)
	}
	created, err := store.CreateRunItems(t.Context(), run.ID, "worker-1", inputs)
	if err != nil {
		t.Fatalf("CreateRunItems() error = %v", err)
	}
	if len(created) != 3 || created[0].Position != 2 || created[0].ProgressPercent != 100 || created[0].FinishedAt == nil {
		t.Fatalf("created items = %#v", created)
	}

	page, err := store.ListRunItems(t.Context(), run.ID, task.RunItemFilter{Page: 1, PerPage: 2})
	if err != nil {
		t.Fatalf("ListRunItems() error = %v", err)
	}
	if page.Total != 3 || len(page.Items) != 2 || page.Items[0].Position != 0 || page.Items[1].Position != 1 {
		t.Fatalf("item page = %#v", page)
	}
	if page.Summary.Total != 3 ||
		page.Summary.ByStatus[task.RunItemStatusPending] != 2 ||
		page.Summary.ByStatus[task.RunItemStatusSkipped] != 1 ||
		page.Summary.ByAction[task.RunItemActionCopy] != 1 ||
		page.Summary.ByAction[task.RunItemActionNone] != 1 {
		t.Fatalf("item summary = %#v", page.Summary)
	}

	copyItem := page.Items[0]
	expectedPending := task.RunItemStatusPending
	updatedAt := now.Add(time.Second)
	running, err := store.UpdateRunItem(t.Context(), run.ID, copyItem.ID, "worker-1", task.RunItemUpdate{
		ExpectedStatus:     &expectedPending,
		Status:             task.RunItemStatusRunning,
		ProgressPercent:    35,
		UpstreamTaskID:     "synthetic-upstream-id",
		UpstreamTaskState:  "running",
		UpstreamTaskStatus: "copying",
	}, updatedAt)
	if err != nil {
		t.Fatalf("UpdateRunItem() running error = %v", err)
	}
	if running.Status != task.RunItemStatusRunning || running.ProgressPercent != 35 || running.StartedAt == nil || running.FinishedAt != nil {
		t.Fatalf("running item = %#v", running)
	}
	if running.UpstreamTaskID != "synthetic-upstream-id" || !running.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("running upstream/timestamp = %#v", running)
	}
	if _, err := store.UpdateRunItem(t.Context(), run.ID, copyItem.ID, "worker-1", task.RunItemUpdate{
		ExpectedStatus:  &expectedPending,
		Status:          task.RunItemStatusSucceeded,
		ProgressPercent: 100,
	}, updatedAt); !errors.Is(err, task.ErrConflict) {
		t.Fatalf("UpdateRunItem() stale status error = %v, want ErrConflict", err)
	}
	expectedRunning := task.RunItemStatusRunning
	succeeded, err := store.UpdateRunItem(t.Context(), run.ID, copyItem.ID, "worker-1", task.RunItemUpdate{
		ExpectedStatus:  &expectedRunning,
		Status:          task.RunItemStatusSucceeded,
		ProgressPercent: 70,
	}, updatedAt.Add(time.Second))
	if err != nil {
		t.Fatalf("UpdateRunItem() success error = %v", err)
	}
	if succeeded.ProgressPercent != 100 || succeeded.FinishedAt == nil || succeeded.ErrorMessage != "" {
		t.Fatalf("succeeded item = %#v", succeeded)
	}
	if _, err := store.UpdateRunItem(t.Context(), run.ID, page.Items[1].ID, "stale-worker", task.RunItemUpdate{
		Status: task.RunItemStatusRunning,
	}, updatedAt); !errors.Is(err, task.ErrConflict) {
		t.Fatalf("UpdateRunItem() wrong owner error = %v, want ErrConflict", err)
	}
	expectedMkdirPending := task.RunItemStatusPending
	failed, err := store.UpdateRunItem(t.Context(), run.ID, page.Items[1].ID, "worker-1", task.RunItemUpdate{
		ExpectedStatus:  &expectedMkdirPending,
		Status:          task.RunItemStatusFailed,
		ProgressPercent: 10,
		ErrorMessage:    "synthetic safe failure",
	}, updatedAt.Add(2*time.Second))
	if err != nil {
		t.Fatalf("UpdateRunItem() failed error = %v", err)
	}
	persistedFailure, err := store.GetRunItem(t.Context(), run.ID, failed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persistedFailure.ErrorMessage != "synthetic safe failure" || persistedFailure.FinishedAt == nil {
		t.Fatalf("persisted failed item = %#v", persistedFailure)
	}

	filtered, err := store.ListRunItems(t.Context(), run.ID, task.RunItemFilter{
		Statuses: []task.RunItemStatus{task.RunItemStatusSucceeded},
		Actions:  []task.RunItemAction{task.RunItemActionCopy},
		Page:     1,
		PerPage:  1,
	})
	if err != nil {
		t.Fatalf("ListRunItems() filtered error = %v", err)
	}
	if filtered.Total != 1 || len(filtered.Items) != 1 || filtered.Items[0].ID != copyItem.ID {
		t.Fatalf("filtered items = %#v", filtered)
	}
	if filtered.Summary.Total != 3 || filtered.Summary.ByStatus[task.RunItemStatusFailed] != 1 {
		t.Fatalf("filtered summary = %#v", filtered.Summary)
	}
}

func TestTaskRunItemsBatchCreationIsAtomic(t *testing.T) {
	now := time.Date(2026, time.July, 22, 13, 0, 0, 0, time.UTC)
	store := newTaskTestStore(t, now)
	run := createTaskTestRun(t, store, now, "fixture.items", "local-admin")
	if _, err := store.ClaimRun(t.Context(), "worker-1", now, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	initial := task.NewRunItem{
		Position:     1,
		RelativePath: "initial.txt",
		ObjectKind:   task.RunItemObjectFile,
		Action:       task.RunItemActionCopy,
	}
	if _, err := store.CreateRunItems(t.Context(), run.ID, "worker-1", []task.NewRunItem{initial}); err != nil {
		t.Fatal(err)
	}

	_, err := store.CreateRunItems(t.Context(), run.ID, "worker-1", []task.NewRunItem{
		{
			Position:     2,
			RelativePath: "rolled-back.txt",
			ObjectKind:   task.RunItemObjectFile,
			Action:       task.RunItemActionCopy,
		},
		initial,
	})
	if !errors.Is(err, task.ErrConflict) {
		t.Fatalf("CreateRunItems() error = %v, want ErrConflict", err)
	}
	page, err := store.ListRunItems(t.Context(), run.ID, task.RunItemFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].RelativePath != "initial.txt" {
		t.Fatalf("items after rollback = %#v", page)
	}
}

func TestTaskRunItemsCascadeWithRunRetention(t *testing.T) {
	now := time.Date(2026, time.July, 22, 14, 0, 0, 0, time.UTC)
	store := newTaskTestStore(t, now)
	if err := store.db.Exec(`PRAGMA foreign_keys = ON`).Error; err != nil {
		t.Fatal(err)
	}
	run := createTaskTestRun(t, store, now, "fixture.cleanup-items", "local-admin")
	if _, err := store.ClaimRun(t.Context(), "worker-1", now, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	items, err := store.CreateRunItems(t.Context(), run.ID, "worker-1", []task.NewRunItem{{
		Position:     0,
		RelativePath: "cleanup.txt",
		ObjectKind:   task.RunItemObjectFile,
		Action:       task.RunItemActionDelete,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteRun(t.Context(), run.ID, "worker-1", []byte(`{}`), now.Add(-40*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	deleted, err := store.DeleteTerminalRunsBefore(t.Context(), now.Add(-30*24*time.Hour))
	if err != nil || deleted != 1 {
		t.Fatalf("DeleteTerminalRunsBefore() = %d, %v", deleted, err)
	}
	if _, err := store.GetRunItem(t.Context(), run.ID, items[0].ID); !errors.Is(err, task.ErrNotFound) {
		t.Fatalf("GetRunItem() after retention error = %v, want ErrNotFound", err)
	}
}
