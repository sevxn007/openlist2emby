package sqlite

import (
	"context"
	"errors"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Store) SetMediaPlayedStateForUser(ctx context.Context, userID, itemID int64, played bool, now time.Time) (medialibrary.UserData, error) {
	var result medialibrary.UserData
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item mediaItemModel
		if err := tx.Where("id = ?", itemID).Take(&item).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.ErrNotFound
		} else if err != nil {
			return err
		}

		ids, err := descendantMediaItemIDs(tx, itemID)
		if err != nil {
			return err
		}
		var existing []mediaUserDataModel
		if err := tx.Where("user_id = ? AND item_id IN ?", userID, ids).Find(&existing).Error; err != nil {
			return err
		}
		byItem := make(map[int64]mediaUserDataModel, len(existing))
		for _, record := range existing {
			byItem[record.ItemID] = record
		}
		for _, id := range ids {
			record := byItem[id]
			record.UserID, record.ItemID = userID, id
			applyPlayedState(&record, played, now)
			if err := saveMediaUserData(tx, record); err != nil {
				return err
			}
		}

		folders, err := playedStateFolders(tx, item)
		if err != nil {
			return err
		}
		for _, folder := range folders {
			total, watched, countErr := episodePlayedCounts(tx, userID, folder)
			if countErr != nil {
				return countErr
			}
			if total == 0 {
				continue
			}
			var record mediaUserDataModel
			err := tx.Where("user_id = ? AND item_id = ?", userID, folder.ID).Take(&record).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			record.UserID, record.ItemID = userID, folder.ID
			applyPlayedState(&record, watched == total, now)
			if err := saveMediaUserData(tx, record); err != nil {
				return err
			}
		}

		var requested mediaUserDataModel
		if err := tx.Where("user_id = ? AND item_id = ?", userID, itemID).Take(&requested).Error; err != nil {
			return err
		}
		result = mediaUserDataFromModel(requested)
		if item.Kind == string(medialibrary.KindSeries) || item.Kind == string(medialibrary.KindSeason) {
			total, watched, countErr := episodePlayedCounts(tx, userID, item)
			if countErr != nil {
				return countErr
			}
			unplayed := int(total - watched)
			result.UnplayedItemCount = &unplayed
		}
		return nil
	})
	return result, err
}

func descendantMediaItemIDs(tx *gorm.DB, itemID int64) ([]int64, error) {
	var ids []int64
	err := tx.Raw(`WITH RECURSIVE descendants(id) AS (
		SELECT id FROM media_items WHERE id = ?
		UNION ALL
		SELECT child.id FROM media_items child JOIN descendants parent ON child.parent_id = parent.id
	) SELECT id FROM descendants`, itemID).Scan(&ids).Error
	return ids, err
}

func playedStateFolders(tx *gorm.DB, item mediaItemModel) ([]mediaItemModel, error) {
	result := make([]mediaItemModel, 0, 2)
	if item.Kind == string(medialibrary.KindSeries) || item.Kind == string(medialibrary.KindSeason) {
		result = append(result, item)
	}
	parentID := item.ParentID
	for parentID != nil {
		var parent mediaItemModel
		if err := tx.Select("id", "parent_id", "kind").Where("id = ?", *parentID).Take(&parent).Error; err != nil {
			return nil, err
		}
		if parent.Kind == string(medialibrary.KindSeries) || parent.Kind == string(medialibrary.KindSeason) {
			result = append(result, parent)
		}
		parentID = parent.ParentID
	}
	return result, nil
}

func episodePlayedCounts(tx *gorm.DB, userID int64, folder mediaItemModel) (int64, int64, error) {
	var counts struct {
		Total   int64
		Watched int64
	}
	err := tx.Raw(`WITH RECURSIVE descendants(id, kind) AS (
		SELECT id, kind FROM media_items WHERE parent_id = ?
		UNION ALL
		SELECT child.id, child.kind FROM media_items child JOIN descendants parent ON child.parent_id = parent.id
	)
	SELECT COUNT(*) AS total,
		COALESCE(SUM(CASE WHEN user_data.played = 1 THEN 1 ELSE 0 END), 0) AS watched
	FROM descendants
	LEFT JOIN media_user_data user_data ON user_data.item_id = descendants.id AND user_data.user_id = ?
	WHERE descendants.kind = ?`, folder.ID, userID, string(medialibrary.KindEpisode)).Scan(&counts).Error
	return counts.Total, counts.Watched, err
}

func applyPlayedState(record *mediaUserDataModel, played bool, now time.Time) {
	record.PlaybackPositionTicks = 0
	if played {
		if !record.Played {
			record.PlayCount++
		}
		record.Played = true
		record.LastPlayedAt = newNullableSQLiteTimestamp(&now)
	} else {
		record.Played = false
	}
	record.UpdatedAt = newSQLiteTimestamp(now)
}

func saveMediaUserData(tx *gorm.DB, record mediaUserDataModel) error {
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "item_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"playback_position_ticks", "played", "play_count", "favorite", "last_played_at", "updated_at"}),
	}).Create(&record).Error
}

var _ medialibrary.PlayedStateStore = (*Store)(nil)
