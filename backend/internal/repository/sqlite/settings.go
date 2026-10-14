package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"gorm.io/gorm"
)

func (s *Store) GetSettings(ctx context.Context) (domain.Settings, error) {
	var record settingsModel
	err := s.db.WithContext(ctx).Where("id = ?", 1).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.Settings{}, domain.ErrNotSetup
	}
	if err != nil {
		return domain.Settings{}, err
	}
	priority := []string{"tmdb", "tvdb"}
	if record.MetadataPriorityJSON != "" {
		if err := json.Unmarshal([]byte(record.MetadataPriorityJSON), &priority); err != nil {
			return domain.Settings{}, err
		}
	}
	return domain.Settings{TVDBAPIKey: record.TVDBAPIKey, TVDBPIN: record.TVDBPIN, MetadataPriority: priority,
		OpenListBaseURL:     record.OpenListBaseURL,
		OpenListAdminKey:    record.OpenListAdminKey,
		TMDBAPIKey:          record.TMDBAPIKey,
		MediaLibraryEnabled: record.MediaLibraryEnabled,
		EmbyPort:            record.EmbyPort,
		MediaMetadataPath:   record.MediaMetadataPath,
		CreatedAt:           record.CreatedAt.Time(),
		UpdatedAt:           record.UpdatedAt.Time(),
	}, nil
}

func (s *Store) SetMediaMetadataPath(ctx context.Context, path string) error {
	result := s.db.WithContext(ctx).Model(&settingsModel{}).Where("id = ?", 1).Updates(map[string]any{
		"media_metadata_path": strings.TrimSpace(path), "updated_at": newSQLiteTimestamp(s.now()),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrNotSetup
	}
	return nil
}

func (s *Store) SetMediaLibraryConfiguration(ctx context.Context, enabled bool, port int) error {
	result := s.db.WithContext(ctx).
		Model(&settingsModel{}).
		Where("id = ?", 1).
		Updates(map[string]any{
			"media_library_enabled": enabled,
			"emby_port":             port,
			"updated_at":            newSQLiteTimestamp(s.now()),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrNotSetup
	}
	return nil
}

func (s *Store) SaveInitialSettings(ctx context.Context, baseURL, adminKey string, tmdbKeys ...string) error {
	db := s.db.WithContext(ctx)
	var count int64
	if err := db.Model(&settingsModel{}).Where("id = ?", 1).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return domain.ErrAlreadySetup
	}
	now := newSQLiteTimestamp(s.now())
	err := db.Create(&settingsModel{
		ID:               1,
		OpenListBaseURL:  baseURL,
		OpenListAdminKey: adminKey,
		TMDBAPIKey:       optionalSecret(tmdbKeys),
		EmbyPort:         domain.DefaultEmbyPort,
		CreatedAt:        now,
		UpdatedAt:        now,
	}).Error
	if isUniqueConstraint(err) {
		return domain.ErrAlreadySetup
	}
	return err
}

func (s *Store) UpdateSettings(ctx context.Context, baseURL, adminKey string, tmdbKeys ...string) error {
	current, err := s.GetSettings(ctx)
	if err != nil {
		return err
	}
	if adminKey == "" {
		adminKey = current.OpenListAdminKey
	}
	tmdbKey := optionalSecret(tmdbKeys)
	if tmdbKey == "" {
		tmdbKey = current.TMDBAPIKey
	}
	result := s.db.WithContext(ctx).
		Model(&settingsModel{}).
		Where("id = ?", 1).
		Updates(map[string]any{
			"openlist_base_url":  baseURL,
			"openlist_admin_key": adminKey,
			"tmdb_api_key":       tmdbKey,
			"updated_at":         newSQLiteTimestamp(s.now()),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrNotSetup
	}
	return nil
}

func optionalSecret(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func isUniqueConstraint(err error) bool {
	return errors.Is(err, gorm.ErrDuplicatedKey) ||
		(err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint"))
}

// SetMetadataConfiguration persists a validated source configuration atomically.
func (s *Store) SetMetadataConfiguration(ctx context.Context, key, pin string, priority []string) error {
	encoded, err := json.Marshal(priority)
	if err != nil {
		return err
	}
	result := s.db.WithContext(ctx).Model(&settingsModel{}).Where("id = ?", 1).Updates(map[string]any{
		"tvdb_api_key": key, "tvdb_pin": pin, "metadata_priority_json": string(encoded), "updated_at": newSQLiteTimestamp(s.now()),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrNotSetup
	}
	return nil
}
