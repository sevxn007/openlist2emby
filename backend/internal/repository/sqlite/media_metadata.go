package sqlite

import (
	"context"
	"errors"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Store) GetMediaItemImage(ctx context.Context, itemID int64, imageType medialibrary.ImageType) (medialibrary.LocalImage, error) {
	var record mediaItemImageModel
	err := s.db.WithContext(ctx).Where("item_id = ? AND image_type = ?", itemID, string(imageType)).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return medialibrary.LocalImage{}, domain.ErrNotFound
	}
	if err != nil {
		return medialibrary.LocalImage{}, err
	}
	return localImageFromModel(record), nil
}

func (s *Store) ListMediaItemImages(ctx context.Context, itemIDs []int64) (map[int64]map[medialibrary.ImageType]medialibrary.LocalImage, error) {
	result := make(map[int64]map[medialibrary.ImageType]medialibrary.LocalImage)
	if len(itemIDs) == 0 {
		return result, nil
	}
	var records []mediaItemImageModel
	if err := s.db.WithContext(ctx).Where("item_id IN ?", itemIDs).Find(&records).Error; err != nil {
		return nil, err
	}
	for _, record := range records {
		if result[record.ItemID] == nil {
			result[record.ItemID] = make(map[medialibrary.ImageType]medialibrary.LocalImage)
		}
		image := localImageFromModel(record)
		result[record.ItemID][image.ImageType] = image
	}
	return result, nil
}

func (s *Store) UpsertMediaItemImage(ctx context.Context, image medialibrary.LocalImage) error {
	record := mediaItemImageModel{
		ItemID: image.ItemID, ImageType: string(image.ImageType), SourceType: string(image.SourceType), SourceURL: image.SourceURL,
		SourceVideoFingerprint: image.SourceVideoFingerprint, RelativePath: image.RelativePath, MIMEType: image.MIMEType, SizeBytes: image.SizeBytes,
		Width: image.Width, Height: image.Height, SHA256: image.SHA256, UpdatedAt: newSQLiteTimestamp(image.UpdatedAt),
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "item_id"}, {Name: "image_type"}}, UpdateAll: true,
		}).Create(&record).Error; err != nil {
			return err
		}
		column := "image_url"
		if image.ImageType == medialibrary.ImageBackdrop {
			column = "backdrop_url"
		}
		result := tx.Model(&mediaItemModel{}).Where("id = ?", image.ItemID).Update(column, image.SourceURL)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
}

func (s *Store) ReplaceMediaItemPeople(ctx context.Context, itemID int64, people []medialibrary.LocalPerson) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("item_id = ?", itemID).Delete(&mediaItemPersonModel{}).Error; err != nil {
			return err
		}
		now := newSQLiteTimestamp(s.now())
		for index, value := range people {
			person := value.Person
			if person.ID <= 0 || person.Name == "" {
				continue
			}
			if person.Source == "" {
				person.Source = "tmdb"
			}
			if person.ExternalID == 0 {
				person.ExternalID = person.ID
			}
			if person.Source != "tmdb" {
				var identity mediaPersonIdentityModel
				err := tx.Where("source = ? AND external_id = ?", person.Source, person.ExternalID).Take(&identity).Error
				if errors.Is(err, gorm.ErrRecordNotFound) {
					var maxID int64
					if err := tx.Model(&mediaPersonModel{}).Select("COALESCE(MAX(id),0)").Scan(&maxID).Error; err != nil {
						return err
					}
					if maxID < 1_000_000_000_000 {
						maxID = 1_000_000_000_000
					}
					identity = mediaPersonIdentityModel{Source: person.Source, ExternalID: person.ExternalID, LocalID: maxID + 1}
					if err := tx.Create(&identity).Error; err != nil {
						return err
					}
				} else if err != nil {
					return err
				}
				person.ID = identity.LocalID
			} else {
				person.ID = person.ExternalID
			}
			people[index].Person = person
			record := mediaPersonModel{ID: person.ID, Source: person.Source, ExternalID: person.ExternalID, Name: person.Name, UpdatedAt: now}
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "id"}},
				DoUpdates: clause.Assignments(map[string]any{"name": person.Name, "updated_at": now}),
			}).Create(&record).Error; err != nil {
				return err
			}
			relation := mediaItemPersonModel{ItemID: itemID, PersonID: person.ID, Type: person.Type, Role: person.Role, DisplayRole: person.DisplayRole, SortOrder: person.SortOrder}
			if err := tx.Create(&relation).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) ListMediaItemPeople(ctx context.Context, itemID int64) ([]domain.MediaPerson, error) {
	type personRow struct {
		Source        string
		ExternalID    int64
		ID            int64
		Name          string
		ProfileSHA256 string
		Type          string
		Role          string
		DisplayRole   string
		SortOrder     int
	}
	var rows []personRow
	err := s.db.WithContext(ctx).Table("media_item_people AS relation").
		Select("person.source,person.external_id,person.id, person.name, person.profile_sha256, relation.type, relation.role, relation.display_role, relation.sort_order").
		Joins("JOIN media_people AS person ON person.id = relation.person_id").
		Where("relation.item_id = ?", itemID).
		Order("relation.sort_order, person.name COLLATE NOCASE").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	people := make([]domain.MediaPerson, 0, len(rows))
	for _, row := range rows {
		people = append(people, domain.MediaPerson{Source: row.Source, ExternalID: row.ExternalID, ID: row.ID, Name: row.Name, Type: row.Type, Role: row.Role, DisplayRole: row.DisplayRole, SortOrder: row.SortOrder, ProfileTag: row.ProfileSHA256})
	}
	return people, nil
}

func (s *Store) GetMediaPerson(ctx context.Context, personID int64) (medialibrary.LocalPerson, error) {
	var record mediaPersonModel
	err := s.db.WithContext(ctx).Where("id = ?", personID).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return medialibrary.LocalPerson{}, domain.ErrNotFound
	}
	if err != nil {
		return medialibrary.LocalPerson{}, err
	}
	return localPersonFromModel(record), nil
}

func (s *Store) SetMediaPersonProfile(ctx context.Context, personID int64, value medialibrary.LocalPerson) error {
	result := s.db.WithContext(ctx).Model(&mediaPersonModel{}).Where("id = ?", personID).Updates(map[string]any{
		"profile_source_url": value.ProfileSourceURL, "profile_relative_path": value.ProfileRelativePath,
		"profile_mime_type": value.ProfileMIMEType, "profile_size_bytes": value.ProfileSizeBytes,
		"profile_width": value.ProfileWidth, "profile_height": value.ProfileHeight,
		"profile_sha256": value.ProfileSHA256, "updated_at": newSQLiteTimestamp(value.UpdatedAt),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) SetMediaItemLocalized(ctx context.Context, itemID int64, localized bool) error {
	return s.db.WithContext(ctx).Model(&mediaItemModel{}).Where("id = ?", itemID).
		Update("metadata_localized", localized).Error
}

func (s *Store) ListMediaAssetReferences(ctx context.Context) ([]string, []string, error) {
	paths, tags := []string{}, []string{}
	var itemImages []mediaItemImageModel
	if err := s.db.WithContext(ctx).Find(&itemImages).Error; err != nil {
		return nil, nil, err
	}
	for _, image := range itemImages {
		paths = append(paths, image.RelativePath)
		tags = append(tags, image.SHA256)
	}
	var people []mediaPersonModel
	if err := s.db.WithContext(ctx).Where("profile_relative_path <> ''").Find(&people).Error; err != nil {
		return nil, nil, err
	}
	for _, person := range people {
		paths = append(paths, person.ProfileRelativePath)
		tags = append(tags, person.ProfileSHA256)
	}
	return paths, tags, nil
}

func localImageFromModel(record mediaItemImageModel) medialibrary.LocalImage {
	return medialibrary.LocalImage{ItemID: record.ItemID, ImageType: medialibrary.ImageType(record.ImageType), SourceType: medialibrary.ImageSourceType(record.SourceType), SourceURL: record.SourceURL, SourceVideoFingerprint: record.SourceVideoFingerprint, RelativePath: record.RelativePath, MIMEType: record.MIMEType, SizeBytes: record.SizeBytes, Width: record.Width, Height: record.Height, SHA256: record.SHA256, UpdatedAt: record.UpdatedAt.Time()}
}

func localPersonFromModel(record mediaPersonModel) medialibrary.LocalPerson {
	return medialibrary.LocalPerson{Person: domain.MediaPerson{Source: record.Source, ExternalID: record.ExternalID, ID: record.ID, Name: record.Name}, ProfileSourceURL: record.ProfileSourceURL, ProfileRelativePath: record.ProfileRelativePath, ProfileMIMEType: record.ProfileMIMEType, ProfileSizeBytes: record.ProfileSizeBytes, ProfileWidth: record.ProfileWidth, ProfileHeight: record.ProfileHeight, ProfileSHA256: record.ProfileSHA256, UpdatedAt: record.UpdatedAt.Time()}
}

var _ medialibrary.LocalizedMetadataStore = (*Store)(nil)
