package sqlite

import (
	"context"
	"errors"

	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
	"gorm.io/gorm"
)

type metadataBindingModel struct {
	ItemID     int64  `gorm:"primaryKey;autoIncrement:false"`
	SourceType string `gorm:"not null"`
	SourceID   int64  `gorm:"not null;check:check_media_metadata_bindings_source_id,source_id > 0"`

	Item *mediaItemModel `gorm:"foreignKey:ItemID;references:ID;constraint:OnUpdate:NO ACTION,OnDelete:CASCADE"`
}

func (metadataBindingModel) TableName() string { return "media_metadata_bindings" }

// GetMediaMetadataBinding returns nil when the item has no standalone binding.
func (s *Store) GetMediaMetadataBinding(ctx context.Context, itemID int64) (*medialibrary.MetadataBinding, error) {
	var record metadataBindingModel
	err := s.db.WithContext(ctx).Where("item_id = ?", itemID).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return metadataBindingFromModel(record), nil
}

func mediaMetadataBindingsByItem(ctx context.Context, db *gorm.DB, items []mediaItemModel) (map[int64]*medialibrary.MetadataBinding, error) {
	bindings := make(map[int64]*medialibrary.MetadataBinding, len(items))
	if len(items) == 0 {
		return bindings, nil
	}
	ids := make([]int64, len(items))
	for index, item := range items {
		ids[index] = item.ID
	}
	var records []metadataBindingModel
	if err := db.WithContext(ctx).Where("item_id IN ?", ids).Find(&records).Error; err != nil {
		return nil, err
	}
	for _, record := range records {
		bindings[record.ItemID] = metadataBindingFromModel(record)
	}
	return bindings, nil
}

func metadataBindingFromModel(record metadataBindingModel) *medialibrary.MetadataBinding {
	return &medialibrary.MetadataBinding{SourceType: record.SourceType, SourceID: record.SourceID}
}
