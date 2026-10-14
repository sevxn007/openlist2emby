package sqlite

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	glebarezsqlite "github.com/glebarez/sqlite"
	"github.com/sevxn007/openlist2emby/backend/internal/task"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTaskRunCreateClaimAndComplete(t *testing.T) {
	now := time.Date(2026, time.July, 22, 8, 0, 0, 0, time.UTC)
	store := newTaskTestStore(t, now)
	created, err := store.CreateRun(t.Context(), task.NewRun{
		Type:           "fixture.manual",
		PayloadVersion: 1,
		Payload:        json.RawMessage(`{"path":"/synthetic/song.flac"}`),
		Trigger:        task.TriggerManual,
		MaxAttempts:    3,
		AvailableAt:    now.Add(-time.Minute),
		CreatedBy:      "local-admin",
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if created.Status != task.StatusQueued || created.ScheduleID != nil {
		t.Fatalf("created run = %#v", created)
	}

	leaseUntil := now.Add(time.Minute)
	claimed, err := store.ClaimRun(t.Context(), "worker-1", now, leaseUntil)
	if err != nil {
		t.Fatalf("ClaimRun() error = %v", err)
	}
	if claimed == nil || claimed.ID != created.ID || claimed.Status != task.StatusRunning {
		t.Fatalf("claimed run = %#v", claimed)
	}
	if claimed.Attempt != 1 || claimed.LeaseOwner != "worker-1" || claimed.LeaseExpiresAt == nil || !claimed.LeaseExpiresAt.Equal(leaseUntil) {
		t.Fatalf("claimed lease = %#v", claimed)
	}
	scanProgress := task.Progress{
		Stage:              "scanning",
		Percent:            0,
		ScannedDirectories: 12,
		ScannedFiles:       345,
	}
	if err := store.UpdateRunProgress(t.Context(), created.ID, "worker-1", scanProgress, now); err != nil {
		t.Fatalf("UpdateRunProgress() error = %v", err)
	}
	withProgress, err := store.GetRun(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if withProgress.ProgressStage != "scanning" || withProgress.ScannedDirectories != 12 || withProgress.ScannedFiles != 345 {
		t.Fatalf("stored scan progress = %#v", withProgress)
	}
	if err := store.CompleteRun(t.Context(), created.ID, "another-worker", []byte(`{"ok":true}`), now); !errors.Is(err, task.ErrConflict) {
		t.Fatalf("CompleteRun() wrong owner error = %v, want ErrConflict", err)
	}
	if err := store.CompleteRun(t.Context(), created.ID, "worker-1", []byte(`{"ok":true}`), now); err != nil {
		t.Fatalf("CompleteRun() error = %v", err)
	}

	completed, err := store.GetRun(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != task.StatusSucceeded || completed.ProgressPercent != 100 || completed.FinishedAt == nil {
		t.Fatalf("completed run = %#v", completed)
	}
	if string(completed.Result) != `{"ok":true}` || completed.LeaseOwner != "" || completed.LeaseExpiresAt != nil {
		t.Fatalf("completed result/lease = %#v", completed)
	}
	next, err := store.ClaimRun(t.Context(), "worker-2", now, leaseUntil)
	if err != nil || next != nil {
		t.Fatalf("ClaimRun() after completion = %#v, %v; want nil, nil", next, err)
	}
}

func TestTaskScheduleStoresManualDefinition(t *testing.T) {
	now := time.Date(2026, time.July, 22, 8, 0, 0, 0, time.UTC)
	store := newTaskTestStore(t, now)
	created, err := store.CreateSchedule(t.Context(), task.Schedule{
		Name:           "On-demand scan",
		Type:           "media.scan",
		PayloadVersion: 1,
		Payload:        json.RawMessage(`{"library_id":1}`),
		CreatedBy:      "local-admin",
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	if err != nil {
		t.Fatalf("CreateSchedule() error = %v", err)
	}
	stored, err := store.GetSchedule(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("GetSchedule() error = %v", err)
	}
	if stored.CronExpression != "" || stored.Timezone != "" || stored.Enabled || stored.NextRunAt != nil {
		t.Fatalf("manual definition = %#v", stored)
	}
}

func TestTaskRunCancellation(t *testing.T) {
	now := time.Date(2026, time.July, 22, 9, 0, 0, 0, time.UTC)

	t.Run("queued run becomes terminal", func(t *testing.T) {
		store := newTaskTestStore(t, now)
		run := createTaskTestRun(t, store, now, "fixture.cancel", "local-admin")
		canceled, err := store.CancelRun(t.Context(), run.ID, now)
		if err != nil {
			t.Fatalf("CancelRun() error = %v", err)
		}
		if canceled.Status != task.StatusCanceled || canceled.FinishedAt == nil || !canceled.FinishedAt.Equal(now) {
			t.Fatalf("canceled queued run = %#v", canceled)
		}
	})

	t.Run("running run records a cooperative cancellation request", func(t *testing.T) {
		store := newTaskTestStore(t, now)
		run := createTaskTestRun(t, store, now, "fixture.cancel", "local-admin")
		claimed, err := store.ClaimRun(t.Context(), "worker-1", now, now.Add(time.Minute))
		if err != nil || claimed == nil {
			t.Fatalf("ClaimRun() = %#v, %v", claimed, err)
		}
		canceling, err := store.CancelRun(t.Context(), run.ID, now)
		if err != nil {
			t.Fatalf("CancelRun() error = %v", err)
		}
		if canceling.Status != task.StatusRunning || canceling.CancelRequestedAt == nil {
			t.Fatalf("canceling run = %#v", canceling)
		}
		requested, err := store.HeartbeatRun(t.Context(), run.ID, "worker-1", now.Add(2*time.Minute), now)
		if err != nil || !requested {
			t.Fatalf("HeartbeatRun() requested/error = %v/%v", requested, err)
		}
		if err := store.MarkRunCanceled(t.Context(), run.ID, "worker-1", now); err != nil {
			t.Fatalf("MarkRunCanceled() error = %v", err)
		}
		canceled, err := store.GetRun(t.Context(), run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if canceled.Status != task.StatusCanceled || canceled.FinishedAt == nil {
			t.Fatalf("canceled running run = %#v", canceled)
		}
	})

	t.Run("persisted cancellation wins over completion", func(t *testing.T) {
		store := newTaskTestStore(t, now)
		run := createTaskTestRun(t, store, now, "fixture.cancel", "local-admin")
		claimed, err := store.ClaimRun(t.Context(), "worker-1", now, now.Add(time.Minute))
		if err != nil || claimed == nil {
			t.Fatalf("ClaimRun() = %#v, %v", claimed, err)
		}
		if _, err := store.CancelRun(t.Context(), run.ID, now); err != nil {
			t.Fatal(err)
		}
		if err := store.CompleteRun(t.Context(), run.ID, "worker-1", []byte(`{"ok":true}`), now); !errors.Is(err, task.ErrRunCanceled) {
			t.Fatalf("CompleteRun() error = %v, want ErrRunCanceled", err)
		}
		canceled, err := store.GetRun(t.Context(), run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if canceled.Status != task.StatusCanceled || canceled.FinishedAt == nil {
			t.Fatalf("canceled run = %#v", canceled)
		}
	})
}

func TestRecoverExpiredRunsHonorsCancellationAndRetryBudget(t *testing.T) {
	now := time.Date(2026, time.July, 22, 9, 30, 0, 0, time.UTC)

	t.Run("requeues an interrupted run with progress reset", func(t *testing.T) {
		store := newTaskTestStore(t, now)
		run := createTaskTestRun(t, store, now, "fixture.recover", "local-admin")
		claimed, err := store.ClaimRun(t.Context(), "worker-1", now, now.Add(-time.Second))
		if err != nil || claimed == nil || claimed.ID != run.ID {
			t.Fatalf("ClaimRun() = %#v, %v", claimed, err)
		}
		if err := store.UpdateRunProgress(t.Context(), run.ID, "worker-1", task.Progress{
			Stage:              "scanning",
			Percent:            0,
			ScannedDirectories: 12,
			ScannedFiles:       345,
		}, now); err != nil {
			t.Fatal(err)
		}

		count, err := store.RecoverExpiredRuns(t.Context(), now, 30*time.Second)
		if err != nil || count != 1 {
			t.Fatalf("RecoverExpiredRuns() = %d, %v", count, err)
		}
		recovered, err := store.GetRun(t.Context(), run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if recovered.Status != task.StatusQueued || !recovered.AvailableAt.Equal(now.Add(30*time.Second)) {
			t.Fatalf("recovered run = %#v", recovered)
		}
		if recovered.ProgressStage != "" || recovered.ProgressPercent != 0 || recovered.ScannedDirectories != 0 || recovered.ScannedFiles != 0 || recovered.LeaseOwner != "" || recovered.LeaseExpiresAt != nil {
			t.Fatalf("recovered queue state = %#v", recovered)
		}
	})

	t.Run("finishes a cancellation requested before the lease expired", func(t *testing.T) {
		store := newTaskTestStore(t, now)
		run := createTaskTestRun(t, store, now, "fixture.cancel", "local-admin")
		claimed, err := store.ClaimRun(t.Context(), "worker-1", now, now.Add(-time.Second))
		if err != nil || claimed == nil {
			t.Fatalf("ClaimRun() = %#v, %v", claimed, err)
		}
		if _, err := store.CancelRun(t.Context(), run.ID, now); err != nil {
			t.Fatal(err)
		}

		count, err := store.RecoverExpiredRuns(t.Context(), now, 30*time.Second)
		if err != nil || count != 1 {
			t.Fatalf("RecoverExpiredRuns() = %d, %v", count, err)
		}
		canceled, err := store.GetRun(t.Context(), run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if canceled.Status != task.StatusCanceled || canceled.FinishedAt == nil || canceled.LeaseOwner != "" {
			t.Fatalf("recovered canceled run = %#v", canceled)
		}
	})

	t.Run("fails an interrupted run with no attempts remaining", func(t *testing.T) {
		store := newTaskTestStore(t, now)
		run, err := store.CreateRun(t.Context(), task.NewRun{
			Type:           "fixture.exhausted",
			PayloadVersion: 1,
			Payload:        json.RawMessage(`{}`),
			Trigger:        task.TriggerManual,
			MaxAttempts:    1,
			AvailableAt:    now,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.ClaimRun(t.Context(), "worker-1", now, now.Add(-time.Second)); err != nil {
			t.Fatal(err)
		}

		count, err := store.RecoverExpiredRuns(t.Context(), now, 30*time.Second)
		if err != nil || count != 1 {
			t.Fatalf("RecoverExpiredRuns() = %d, %v", count, err)
		}
		failed, err := store.GetRun(t.Context(), run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if failed.Status != task.StatusFailed || failed.ErrorCode != "worker_interrupted" || failed.FinishedAt == nil {
			t.Fatalf("exhausted run = %#v", failed)
		}
	})
}

func TestDeleteTerminalRunsBeforeClearsRetryReferences(t *testing.T) {
	now := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	store := newTaskTestStore(t, now)
	old := createTaskTestRun(t, store, now, "fixture.cleanup", "local-admin")
	claimed, err := store.ClaimRun(t.Context(), "worker-old", now, now.Add(time.Minute))
	if err != nil || claimed == nil || claimed.ID != old.ID {
		t.Fatalf("ClaimRun() = %#v, %v", claimed, err)
	}
	if err := store.CompleteRun(
		t.Context(),
		old.ID,
		"worker-old",
		[]byte(`{"ok":true}`),
		now.Add(-40*24*time.Hour),
	); err != nil {
		t.Fatal(err)
	}

	retry, err := store.CreateRun(t.Context(), task.NewRun{
		Type:           old.Type,
		PayloadVersion: old.PayloadVersion,
		Payload:        old.Payload,
		Trigger:        task.TriggerRetry,
		RetryOfRunID:   &old.ID,
		AvailableAt:    now,
		CreatedBy:      "local-admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CancelRun(t.Context(), retry.ID, now); err != nil {
		t.Fatal(err)
	}

	deleted, err := store.DeleteTerminalRunsBefore(t.Context(), now.Add(-30*24*time.Hour))
	if err != nil || deleted != 1 {
		t.Fatalf("DeleteTerminalRunsBefore() = %d, %v", deleted, err)
	}
	if _, err := store.GetRun(t.Context(), old.ID); !errors.Is(err, task.ErrNotFound) {
		t.Fatalf("GetRun(old) error = %v, want ErrNotFound", err)
	}
	retained, err := store.GetRun(t.Context(), retry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retained.RetryOfRunID != nil || retained.Status != task.StatusCanceled {
		t.Fatalf("retained retry = %#v", retained)
	}
}

func TestDeleteRunRejectsRunningAndDeletesQueuedAndTerminalRuns(t *testing.T) {
	now := time.Date(2026, time.July, 22, 10, 30, 0, 0, time.UTC)
	store := newTaskTestStore(t, now)
	queued := createTaskTestRun(t, store, now, "fixture.delete", "local-admin")
	if err := store.DeleteRun(t.Context(), queued.ID); err != nil {
		t.Fatalf("DeleteRun(queued) error = %v", err)
	}
	if _, err := store.GetRun(t.Context(), queued.ID); !errors.Is(err, task.ErrNotFound) {
		t.Fatalf("GetRun(deleted queued) error = %v, want ErrNotFound", err)
	}

	running := createTaskTestRun(t, store, now, "fixture.delete", "local-admin")
	claimed, err := store.ClaimRun(t.Context(), "worker-delete", now, now.Add(time.Minute))
	if err != nil || claimed == nil || claimed.ID != running.ID {
		t.Fatalf("ClaimRun() = %#v, %v", claimed, err)
	}
	if err := store.DeleteRun(t.Context(), running.ID); !errors.Is(err, task.ErrRunActive) {
		t.Fatalf("DeleteRun(running) error = %v, want ErrRunActive", err)
	}

	original := createTaskTestRun(t, store, now, "fixture.delete", "local-admin")
	if _, err := store.CancelRun(t.Context(), original.ID, now); err != nil {
		t.Fatal(err)
	}
	retry, err := store.CreateRun(t.Context(), task.NewRun{
		Type:           original.Type,
		PayloadVersion: original.PayloadVersion,
		Payload:        original.Payload,
		Trigger:        task.TriggerRetry,
		RetryOfRunID:   &original.ID,
		AvailableAt:    now,
		CreatedBy:      "local-admin",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.DeleteRun(t.Context(), original.ID); err != nil {
		t.Fatalf("DeleteRun(terminal) error = %v", err)
	}
	if _, err := store.GetRun(t.Context(), original.ID); !errors.Is(err, task.ErrNotFound) {
		t.Fatalf("GetRun(deleted) error = %v, want ErrNotFound", err)
	}
	retained, err := store.GetRun(t.Context(), retry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retained.RetryOfRunID != nil {
		t.Fatalf("retained retry reference = %v, want nil", *retained.RetryOfRunID)
	}
	if err := store.DeleteRun(t.Context(), 999); !errors.Is(err, task.ErrNotFound) {
		t.Fatalf("DeleteRun(missing) error = %v, want ErrNotFound", err)
	}
}

func TestDeleteCompletedRunsAcrossPagesAndTypes(t *testing.T) {
	now := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	store := newTaskTestStore(t, now)
	if err := store.db.Exec(`PRAGMA foreign_keys = ON`).Error; err != nil {
		t.Fatal(err)
	}
	var completedIDs []int64
	for i := range 25 {
		taskType, status := "fixture.first", task.StatusSucceeded
		if i%2 == 1 {
			taskType, status = "fixture.second", task.StatusCanceled
		}
		run := createTaskTestRun(t, store, now, taskType, "local-admin")
		if err := store.db.Model(&taskRunModel{}).Where("id = ?", run.ID).Update("status", status).Error; err != nil {
			t.Fatal(err)
		}
		completedIDs = append(completedIDs, run.ID)
	}
	// Completed run items must be removed by the current schema's cascade.
	item := taskRunItemModel{
		RunID: completedIDs[0], RelativePath: "synthetic.flac", ObjectKind: "file",
		Action: "copy", Status: "succeeded", CreatedAt: newSQLiteTimestamp(now), UpdatedAt: newSQLiteTimestamp(now),
	}
	if err := store.db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	retained := make(map[int64]task.Status)
	for i, status := range []task.Status{task.StatusQueued, task.StatusRunning, task.StatusFailed} {
		run, err := store.CreateRun(t.Context(), task.NewRun{
			Type: "fixture.retained", PayloadVersion: 1, Payload: json.RawMessage(`{}`),
			Trigger: task.TriggerRetry, RetryOfRunID: &completedIDs[i], AvailableAt: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.db.Model(&taskRunModel{}).Where("id = ?", run.ID).Update("status", status).Error; err != nil {
			t.Fatal(err)
		}
		retained[run.ID] = status
	}
	page, err := store.ListRuns(t.Context(), task.RunFilter{Page: 1, PerPage: 20})
	if err != nil || page.Total != 28 || len(page.Items) != 20 {
		t.Fatalf("ListRuns(before) = %#v, %v", page, err)
	}
	ids, err := store.DeleteCompletedRuns(t.Context())
	if err != nil || len(ids) != len(completedIDs) {
		t.Fatalf("DeleteCompletedRuns() = %v, %v", ids, err)
	}
	deletedIDs := make(map[int64]bool)
	for _, id := range ids {
		deletedIDs[id] = true
	}
	for _, id := range completedIDs {
		if !deletedIDs[id] {
			t.Fatalf("completed run %d missing from deleted IDs", id)
		}
		if _, err := store.GetRun(t.Context(), id); !errors.Is(err, task.ErrNotFound) {
			t.Fatalf("GetRun(%d) error = %v, want ErrNotFound", id, err)
		}
	}
	for id, status := range retained {
		run, err := store.GetRun(t.Context(), id)
		if err != nil || run.Status != status || run.RetryOfRunID != nil {
			t.Fatalf("retained run %d = %#v, %v", id, run, err)
		}
	}
	var items int64
	if err := store.db.Model(&taskRunItemModel{}).Count(&items).Error; err != nil || items != 0 {
		t.Fatalf("remaining items = %d, %v", items, err)
	}
	ids, err = store.DeleteCompletedRuns(t.Context())
	if err != nil || len(ids) != 0 {
		t.Fatalf("DeleteCompletedRuns(empty) = %v, %v", ids, err)
	}
}

func TestDeleteCompletedRunsRollsBackRetryReferencesOnFailure(t *testing.T) {
	now := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	store := newTaskTestStore(t, now)
	completed := createTaskTestRun(t, store, now, "fixture.completed", "local-admin")
	if _, err := store.CancelRun(t.Context(), completed.ID, now); err != nil {
		t.Fatal(err)
	}
	retry, err := store.CreateRun(t.Context(), task.NewRun{
		Type: completed.Type, PayloadVersion: 1, Payload: json.RawMessage(`{}`),
		Trigger: task.TriggerRetry, RetryOfRunID: &completed.ID, AvailableAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.db.Exec(`CREATE TRIGGER reject_completed_delete BEFORE DELETE ON task_runs BEGIN SELECT RAISE(ABORT, 'synthetic delete failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if ids, err := store.DeleteCompletedRuns(t.Context()); err == nil || len(ids) != 0 {
		t.Fatalf("DeleteCompletedRuns() = %v, %v; want no IDs and an error", ids, err)
	}
	if _, err := store.GetRun(t.Context(), completed.ID); err != nil {
		t.Fatal(err)
	}
	retained, err := store.GetRun(t.Context(), retry.ID)
	if err != nil || retained.RetryOfRunID == nil || *retained.RetryOfRunID != completed.ID {
		t.Fatalf("retry after rollback = %#v, %v", retained, err)
	}
}

func TestListRunsFiltersAndPaginates(t *testing.T) {
	now := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	store := newTaskTestStore(t, now)

	canceled := createTaskTestRun(t, store, now, "fixture.alpha", "local-admin")
	if _, err := store.CancelRun(t.Context(), canceled.ID, now); err != nil {
		t.Fatal(err)
	}
	succeeded := createTaskTestRun(t, store, now, "fixture.alpha", "local-admin")
	claimAndCompleteTaskTestRun(t, store, succeeded.ID, "worker-success", now)
	failed := createTaskTestRun(t, store, now, "fixture.beta", "local-admin")
	claimed, err := store.ClaimRun(t.Context(), "worker-failure", now, now.Add(time.Minute))
	if err != nil || claimed == nil || claimed.ID != failed.ID {
		t.Fatalf("ClaimRun() = %#v, %v", claimed, err)
	}
	if err := store.FailRun(t.Context(), failed.ID, "worker-failure", "synthetic_failure", "synthetic failure", 500, now); err != nil {
		t.Fatal(err)
	}
	firstQueued := createTaskTestRun(t, store, now, "fixture.alpha", "local-admin")
	secondQueued := createTaskTestRun(t, store, now, "fixture.alpha", "local-admin")
	createTaskTestRun(t, store, now, "fixture.beta", "local-admin")

	pageOne, err := store.ListRuns(t.Context(), task.RunFilter{
		Statuses: []task.Status{task.StatusQueued},
		Type:     "fixture.alpha",
		Page:     1,
		PerPage:  1,
	})
	if err != nil {
		t.Fatalf("ListRuns() page 1 error = %v", err)
	}
	if pageOne.Total != 2 || pageOne.Page != 1 || pageOne.PerPage != 1 || len(pageOne.Items) != 1 {
		t.Fatalf("page one = %#v", pageOne)
	}
	if pageOne.Items[0].ID != secondQueued.ID {
		t.Fatalf("page one run ID = %d, want %d", pageOne.Items[0].ID, secondQueued.ID)
	}
	pageTwo, err := store.ListRuns(t.Context(), task.RunFilter{
		Statuses: []task.Status{task.StatusQueued},
		Type:     "fixture.alpha",
		Page:     2,
		PerPage:  1,
	})
	if err != nil {
		t.Fatalf("ListRuns() page 2 error = %v", err)
	}
	if len(pageTwo.Items) != 1 || pageTwo.Items[0].ID != firstQueued.ID || pageTwo.Total != 2 {
		t.Fatalf("page two = %#v", pageTwo)
	}
}

func TestTriggerScheduleCoalescesOverlappingRuns(t *testing.T) {
	now := time.Date(2026, time.July, 22, 11, 0, 0, 0, time.UTC)
	store := newTaskTestStore(t, now)
	dueAt := now.Add(-time.Minute)
	schedule, err := store.CreateSchedule(t.Context(), task.Schedule{
		Name:           "Synthetic schedule",
		Type:           "fixture.scheduled",
		PayloadVersion: 1,
		Payload:        json.RawMessage(`{"path":"/synthetic"}`),
		CronExpression: "*/5 * * * *",
		Timezone:       "UTC",
		Enabled:        true,
		NextRunAt:      &dueAt,
		CreatedBy:      "local-admin",
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	if err != nil {
		t.Fatalf("CreateSchedule() error = %v", err)
	}

	nextOne := now.Add(5 * time.Minute)
	first, err := store.TriggerSchedule(t.Context(), schedule, nextOne, now)
	if err != nil || first == nil {
		t.Fatalf("first TriggerSchedule() = %#v, %v", first, err)
	}
	if first.ScheduledFor == nil || !first.ScheduledFor.Equal(dueAt) {
		t.Fatalf("first scheduled_for = %v, want %v", first.ScheduledFor, dueAt)
	}

	schedule, err = store.GetSchedule(t.Context(), schedule.ID)
	if err != nil {
		t.Fatal(err)
	}
	nextTwo := now.Add(10 * time.Minute)
	coalesced, err := store.TriggerSchedule(t.Context(), schedule, nextTwo, now)
	if err != nil || coalesced != nil {
		t.Fatalf("overlapping queued trigger = %#v, %v; want nil, nil", coalesced, err)
	}

	claimed, err := store.ClaimRun(t.Context(), "worker-1", now, now.Add(time.Minute))
	if err != nil || claimed == nil || claimed.ID != first.ID {
		t.Fatalf("ClaimRun() = %#v, %v", claimed, err)
	}
	schedule, err = store.GetSchedule(t.Context(), schedule.ID)
	if err != nil {
		t.Fatal(err)
	}
	nextThree := now.Add(15 * time.Minute)
	second, err := store.TriggerSchedule(t.Context(), schedule, nextThree, now)
	if err != nil || second == nil {
		t.Fatalf("trigger alongside running run = %#v, %v", second, err)
	}

	schedule, err = store.GetSchedule(t.Context(), schedule.ID)
	if err != nil {
		t.Fatal(err)
	}
	nextFour := now.Add(20 * time.Minute)
	coalesced, err = store.TriggerSchedule(t.Context(), schedule, nextFour, now)
	if err != nil || coalesced != nil {
		t.Fatalf("second overlapping queued trigger = %#v, %v; want nil, nil", coalesced, err)
	}

	page, err := store.ListRuns(t.Context(), task.RunFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 {
		t.Fatalf("schedule run count = %d, want 2", page.Total)
	}
	statusCount := map[task.Status]int{}
	for _, run := range page.Items {
		statusCount[run.Status]++
	}
	if statusCount[task.StatusRunning] != 1 || statusCount[task.StatusQueued] != 1 {
		t.Fatalf("schedule run statuses = %#v", statusCount)
	}
	updated, err := store.GetSchedule(t.Context(), schedule.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.NextRunAt == nil || !updated.NextRunAt.Equal(nextFour) {
		t.Fatalf("next_run_at = %v, want %v", updated.NextRunAt, nextFour)
	}
}

func openTaskTestDB(t *testing.T, name string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(glebarezsqlite.Open(filepath.Join(t.TempDir(), name)), &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Silent),
		TranslateError: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func newTaskTestStore(t *testing.T, now time.Time) *Store {
	t.Helper()
	db := openTaskTestDB(t, "tasks.db")
	store := NewStoreWithClock(db, func() time.Time { return now })
	if err := store.InitializeSchema(t.Context()); err != nil {
		t.Fatalf("InitializeSchema() error = %v", err)
	}
	return store
}

func createTaskTestRun(t *testing.T, store *Store, now time.Time, taskType, createdBy string) task.Run {
	t.Helper()
	run, err := store.CreateRun(t.Context(), task.NewRun{
		Type:           taskType,
		PayloadVersion: 1,
		Payload:        json.RawMessage(`{"synthetic":true}`),
		Trigger:        task.TriggerManual,
		AvailableAt:    now,
		CreatedBy:      createdBy,
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	return run
}

func claimAndCompleteTaskTestRun(t *testing.T, store *Store, id int64, owner string, now time.Time) {
	t.Helper()
	claimed, err := store.ClaimRun(t.Context(), owner, now, now.Add(time.Minute))
	if err != nil || claimed == nil || claimed.ID != id {
		t.Fatalf("ClaimRun() = %#v, %v; want ID %d", claimed, err, id)
	}
	if err := store.CompleteRun(t.Context(), id, owner, []byte(`{"ok":true}`), now); err != nil {
		t.Fatalf("CompleteRun() error = %v", err)
	}
}
