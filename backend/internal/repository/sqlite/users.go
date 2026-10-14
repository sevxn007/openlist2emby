package sqlite

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"gorm.io/gorm"
)

func (s *Store) GetLocalUserByID(ctx context.Context, id int64) (domain.LocalUser, error) {
	if id <= 0 {
		return domain.LocalUser{}, domain.ErrNotFound
	}
	var record localUserModel
	err := s.db.WithContext(ctx).Where("id = ?", id).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.LocalUser{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.LocalUser{}, err
	}
	return s.localUserFromModel(ctx, record)
}

func (s *Store) GetLocalUserByUsername(ctx context.Context, username string) (domain.LocalUser, error) {
	var record localUserModel
	err := s.db.WithContext(ctx).Where("username = ?", username).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.LocalUser{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.LocalUser{}, err
	}
	return s.localUserFromModel(ctx, record)
}

func (s *Store) ListLocalUsers(ctx context.Context) ([]domain.LocalUser, error) {
	var records []localUserModel
	if err := s.db.WithContext(ctx).Order("username COLLATE NOCASE").Order("id").Find(&records).Error; err != nil {
		return nil, err
	}
	users := make([]domain.LocalUser, 0, len(records))
	for _, record := range records {
		user, err := s.localUserFromModel(ctx, record)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, nil
}

func (s *Store) ListEnabledLocalUsers(ctx context.Context) ([]domain.LocalUser, error) {
	var records []localUserModel
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Order("username COLLATE NOCASE").Order("id").Find(&records).Error; err != nil {
		return nil, err
	}
	users := make([]domain.LocalUser, 0, len(records))
	for _, record := range records {
		user, err := s.localUserFromModel(ctx, record)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, nil
}

func (s *Store) CreateUser(ctx context.Context, user domain.LocalUser) (domain.LocalUser, error) {
	if user.OpenListUserID <= 0 || user.InstanceID == "" {
		return domain.LocalUser{}, fmt.Errorf("OpenList identity binding is required")
	}
	if !user.Role.Valid() {
		return domain.LocalUser{}, fmt.Errorf("invalid local user role")
	}
	user.LibraryGrants = normalizeLibraryGrants(user.Role, user.LibraryGrants)
	if err := s.validateLibraryGrants(ctx, user.Role, user.LibraryGrants); err != nil {
		return domain.LocalUser{}, err
	}
	now := newSQLiteTimestamp(s.now())
	record := localUserModel{
		Username: user.Username, OpenListUserID: user.OpenListUserID, InstanceID: user.InstanceID, Role: string(user.Role), Enabled: user.Enabled,
		LibraryGrantsConfigured: user.LibraryGrantsConfigured,
		CreatedAt:               now, UpdatedAt: now,
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		return replaceUserLibraryGrants(tx, record.ID, persistedLibraryGrants(user), now)
	})
	if err != nil {
		return domain.LocalUser{}, err
	}
	return s.GetLocalUserByID(ctx, record.ID)
}

func (s *Store) UpdateUser(ctx context.Context, user domain.LocalUser) (domain.LocalUser, error) {
	if user.ID <= 0 {
		return domain.LocalUser{}, domain.ErrNotFound
	}
	if !user.Role.Valid() {
		return domain.LocalUser{}, fmt.Errorf("invalid local user role")
	}
	user.LibraryGrants = normalizeLibraryGrants(user.Role, user.LibraryGrants)
	if err := s.validateLibraryGrants(ctx, user.Role, user.LibraryGrants); err != nil {
		return domain.LocalUser{}, err
	}
	now := newSQLiteTimestamp(s.now())
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current localUserModel
		if err := tx.Where("id = ?", user.ID).Take(&current).Error; err != nil {
			return err
		}
		if current.Role == "admin" && current.Enabled && (!user.Role.IsAdmin() || !user.Enabled) {
			var count int64
			if err := tx.Model(&localUserModel{}).Where("id <> ? AND role = 'admin' AND enabled = ? AND open_list_user_id > 0", user.ID, true).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				return fmt.Errorf("at least one enabled bound administrator is required")
			}
		}
		if !user.Enabled {
			if err := tx.Where("user_id = ?", user.ID).Delete(&sessionModel{}).Error; err != nil {
				return err
			}
		}
		updates := map[string]any{
			"role":    string(user.Role),
			"enabled": user.Enabled, "library_grants_configured": user.LibraryGrantsConfigured, "updated_at": now,
		}
		result := tx.Model(&localUserModel{}).Where("id = ?", user.ID).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return domain.ErrNotFound
		}
		return replaceUserLibraryGrants(tx, user.ID, persistedLibraryGrants(user), now)
	})
	if err != nil {
		return domain.LocalUser{}, err
	}
	return s.GetLocalUserByID(ctx, user.ID)
}

func (s *Store) RevokeUserSessions(ctx context.Context, userID int64) error {
	return s.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&sessionModel{}).Error
}

func (s *Store) ListUserLibraryGrants(ctx context.Context, userID int64) ([]domain.LibraryGrant, error) {
	var user localUserModel
	if err := s.db.WithContext(ctx).Select("id", "role", "library_grants_configured").Where("id = ?", userID).Take(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return s.libraryGrantsFromModel(ctx, user)
}

func (s *Store) libraryGrantsFromModel(ctx context.Context, user localUserModel) ([]domain.LibraryGrant, error) {
	if domain.LocalUserRole(user.Role).IsAdmin() && !user.LibraryGrantsConfigured {
		var libraryIDs []int64
		if err := s.db.WithContext(ctx).Model(&mediaLibraryModel{}).Order("id").Pluck("id", &libraryIDs).Error; err != nil {
			return nil, err
		}
		grants := make([]domain.LibraryGrant, 0, len(libraryIDs))
		for _, libraryID := range libraryIDs {
			grants = append(grants, domain.LibraryGrant{LibraryID: libraryID, Access: domain.LibraryAccessView})
		}
		return grants, nil
	}
	var records []userLibraryGrantModel
	if err := s.db.WithContext(ctx).Where("user_id = ?", user.ID).Order("library_id").Find(&records).Error; err != nil {
		return nil, err
	}
	grants := make([]domain.LibraryGrant, 0, len(records))
	for _, record := range records {
		access := domain.LibraryAccessLevel(record.AccessLevel)
		grants = append(grants, domain.LibraryGrant{LibraryID: record.LibraryID, Access: access})
	}
	return grants, nil
}

func (s *Store) UserCanAccessLibrary(ctx context.Context, userID, libraryID int64) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&userLibraryGrantModel{}).
		Where("user_id = ? AND library_id = ?", userID, libraryID).Count(&count).Error
	return count > 0, err
}

func (s *Store) UserCanEditLibrary(ctx context.Context, userID, libraryID int64) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&userLibraryGrantModel{}).
		Where("user_id = ? AND library_id = ? AND access_level = ?", userID, libraryID, domain.LibraryAccessEdit).Count(&count).Error
	return count > 0, err
}

func (s *Store) ListAccessibleLibraryIDs(ctx context.Context, userID int64) ([]int64, error) {
	grants, err := s.ListUserLibraryGrants(ctx, userID)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(grants))
	for _, grant := range grants {
		ids = append(ids, grant.LibraryID)
	}
	return ids, nil
}

func (s *Store) localUserFromModel(ctx context.Context, record localUserModel) (domain.LocalUser, error) {
	libraryGrants, err := s.libraryGrantsFromModel(ctx, record)
	if err != nil {
		return domain.LocalUser{}, err
	}
	return domain.LocalUser{
		ID: record.ID, Username: record.Username, OpenListUserID: record.OpenListUserID, InstanceID: record.InstanceID,
		Role: domain.LocalUserRole(record.Role), Enabled: record.Enabled, LibraryGrants: libraryGrants,
		LibraryGrantsConfigured: record.LibraryGrantsConfigured,
		CreatedAt:               record.CreatedAt.Time(), UpdatedAt: record.UpdatedAt.Time(),
	}, nil
}

func (s *Store) validateLibraryGrants(ctx context.Context, role domain.LocalUserRole, grants []domain.LibraryGrant) error {
	if !role.IsAdmin() && len(grants) == 0 {
		return fmt.Errorf("at least one media library is required for a media user")
	}
	if len(grants) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(grants))
	libraryIDs := make([]int64, 0, len(grants))
	for _, grant := range grants {
		libraryID := grant.LibraryID
		if libraryID <= 0 {
			return fmt.Errorf("media library ids must be positive")
		}
		if !grant.Access.Valid() {
			return fmt.Errorf("media library access must be view or edit")
		}
		if _, exists := seen[libraryID]; exists {
			return fmt.Errorf("media library ids must be unique")
		}
		seen[libraryID] = struct{}{}
		libraryIDs = append(libraryIDs, libraryID)
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(&mediaLibraryModel{}).Where("id IN ?", libraryIDs).Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(libraryIDs)) {
		return fmt.Errorf("one or more media libraries do not exist")
	}
	return nil
}

func replaceUserLibraryGrants(
	tx *gorm.DB,
	userID int64,
	grants []domain.LibraryGrant,
	now sqliteTimestamp,
) error {
	if err := tx.Where("user_id = ?", userID).Delete(&userLibraryGrantModel{}).Error; err != nil {
		return err
	}
	ordered := append([]domain.LibraryGrant(nil), grants...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].LibraryID < ordered[j].LibraryID })
	models := make([]userLibraryGrantModel, 0, len(ordered))
	for _, grant := range ordered {
		models = append(models, userLibraryGrantModel{
			UserID: userID, LibraryID: grant.LibraryID, AccessLevel: string(grant.Access), CreatedAt: now,
		})
	}
	if len(models) == 0 {
		return nil
	}
	return tx.Create(&models).Error
}

func persistedLibraryGrants(user domain.LocalUser) []domain.LibraryGrant {
	if user.Role.IsAdmin() && !user.LibraryGrantsConfigured {
		return nil
	}
	return user.LibraryGrants
}

func normalizeLibraryGrants(role domain.LocalUserRole, grants []domain.LibraryGrant) []domain.LibraryGrant {
	normalized := append([]domain.LibraryGrant(nil), grants...)
	if role.IsAdmin() {
		for index := range normalized {
			normalized[index].Access = domain.LibraryAccessView
		}
	}
	return normalized
}
