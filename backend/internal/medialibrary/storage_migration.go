package medialibrary

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/sevxn007/openlist2emby/backend/internal/task"
)

const StorageMigrationTaskType = "media.metadata.relocate"

type StorageMigrationPayload struct {
	Path string `json:"path"`
}

type mediaMetadataPathStore interface {
	SetMediaMetadataPath(context.Context, string) error
}

type StorageMigrationHandler struct {
	store  SettingsStore
	assets *AssetFiles
}

func NewStorageMigrationHandler(store SettingsStore, assets *AssetFiles) *StorageMigrationHandler {
	return &StorageMigrationHandler{store: store, assets: assets}
}

func (h *StorageMigrationHandler) Descriptor() task.Descriptor {
	return task.Descriptor{Type: StorageMigrationTaskType, PayloadVersion: 1, Manual: true, Cancelable: true}
}

func (h *StorageMigrationHandler) Prepare(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	if h == nil || h.assets == nil {
		return nil, task.ErrTypeUnavailable
	}
	var payload StorageMigrationPayload
	if err := decodeStrict(raw, &payload); err != nil {
		return nil, task.NewPublicError("invalid_payload", "media metadata path is required", http.StatusBadRequest, false, err)
	}
	path, err := validateAssetRoot(payload.Path)
	if err != nil {
		return nil, task.NewPublicError("invalid_path", err.Error(), http.StatusBadRequest, false, err)
	}
	if path == h.assets.Root() {
		return nil, task.NewPublicError("same_path", "media metadata path has not changed", http.StatusConflict, false, nil)
	}
	payload.Path = path
	return json.Marshal(payload)
}

func (h *StorageMigrationHandler) Run(ctx context.Context, raw json.RawMessage, execution task.Execution) (json.RawMessage, error) {
	var payload StorageMigrationPayload
	if err := decodeStrict(raw, &payload); err != nil {
		return nil, err
	}
	setter, ok := h.store.(mediaMetadataPathStore)
	if !ok {
		return nil, task.ErrTypeUnavailable
	}
	execution.Report(task.Progress{Stage: "copying", Percent: 10})
	err := h.assets.MigrateRoot(ctx, payload.Path, func(path string) error { return setter.SetMediaMetadataPath(ctx, path) })
	if err != nil {
		return nil, task.NewPublicError("migration_failed", err.Error(), http.StatusConflict, true, err)
	}
	execution.Report(task.Progress{Stage: "completed", Percent: 100})
	return json.Marshal(map[string]string{"path": payload.Path})
}

func (h *StorageMigrationHandler) PublicSummary(raw json.RawMessage) any {
	var payload StorageMigrationPayload
	if json.Unmarshal(raw, &payload) != nil {
		return nil
	}
	return map[string]string{"path": strings.TrimSpace(payload.Path)}
}
