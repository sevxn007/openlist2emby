package sqlite

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

var schemaModels = []any{
	&settingsModel{},
	&identityProviderModel{},
	&localUserModel{},
	&sessionModel{},
	&userLibraryGrantModel{},
	&taskModel{},
	&taskRunModel{},
	&taskRunItemModel{},
	&mediaLibraryModel{},
	&mediaLibraryRootModel{},
	&mediaItemModel{},
	&metadataBindingModel{},
	&mediaItemImageModel{},
	&mediaPersonModel{},
	&mediaPersonIdentityModel{},
	&mediaItemPersonModel{},
	&mediaUserDataModel{},
}

// InitializeSchema creates the current application schema.
func (s *Store) InitializeSchema(ctx context.Context) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.AutoMigrate(schemaModels...); err != nil {
			return fmt.Errorf("initialize schema: %w", err)
		}
		if err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_task_runs_one_active_media_metadata_migration
			ON task_runs(type) WHERE type = 'media.metadata.relocate' AND status IN ('queued', 'running')`).Error; err != nil {
			return fmt.Errorf("initialize media metadata migration index: %w", err)
		}
		if err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_tasks_one_media_scan_library
			ON tasks(json_extract(payload_json, '$.library_id'))
			WHERE type = 'media.scan' AND deleted_at IS NULL`).Error; err != nil {
			return fmt.Errorf("initialize media scan schedule index: %w", err)
		}
		if err := tx.Exec("UPDATE media_items SET resolved_metadata_source = 'tmdb' WHERE tmdb_id IS NOT NULL AND resolved_metadata_source = ''").Error; err != nil {
			return err
		}
		if err := tx.Exec("UPDATE media_people SET external_id = id WHERE source = 'tmdb' AND external_id = 0").Error; err != nil {
			return err
		}
		return nil
	})
}
