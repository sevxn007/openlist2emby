package sqlite

import (
	"context"
	"errors"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"gorm.io/gorm"
)

// The installation ID prevents upstream user IDs from being portable between instances.
type identityProviderModel struct {
	ID      string `gorm:"primaryKey"`
	BaseURL string `gorm:"not null;unique"`
}

func (identityProviderModel) TableName() string { return "identity_provider" }

func (s *Store) IdentityInstance(ctx context.Context, base string) (string, error) {
	var record identityProviderModel
	err := s.db.WithContext(ctx).Where("base_url = ?", base).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", domain.ErrNotFound
	}
	return record.ID, err
}
func (s *Store) FindIdentity(ctx context.Context, instance string, id int64) (domain.LocalUser, error) {
	var record localUserModel
	err := s.db.WithContext(ctx).Where("instance_id = ? AND open_list_user_id = ?", instance, id).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.LocalUser{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.LocalUser{}, err
	}
	return s.localUserFromModel(ctx, record)
}

// SaveIdentitySetup atomically initializes integration settings and the first authorization.
func (s *Store) SaveIdentitySetup(ctx context.Context, base, key string, user domain.OpenListIdentity) error {
	instance, err := randomToken()
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		store := NewStoreWithClock(tx, s.now)
		if err := store.SaveInitialSettings(ctx, base, key); err != nil {
			return err
		}
		if err := tx.Create(&identityProviderModel{ID: instance, BaseURL: base}).Error; err != nil {
			return err
		}
		now := newSQLiteTimestamp(s.now())
		return tx.Create(&localUserModel{ID: 1, Username: user.Username, InstanceID: instance, OpenListUserID: user.ID, Role: "admin", Enabled: true, CreatedAt: now, UpdatedAt: now}).Error
	})
}
func (s *Store) SetSessionType(ctx context.Context, token string, emby bool) error {
	return s.db.WithContext(ctx).Model(&sessionModel{}).Where("token = ?", token).Update("emby", emby).Error
}
func (s *Store) SessionType(ctx context.Context, token string) (bool, error) {
	var record sessionModel
	err := s.db.WithContext(ctx).Select("emby").Where("token = ?", token).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, domain.ErrNotFound
	}
	return record.Emby, err
}
func (s *Store) UpdateIdentityName(ctx context.Context, id int64, name string) error {
	return s.db.WithContext(ctx).Model(&localUserModel{}).Where("id = ?", id).Update("username", name).Error
}
