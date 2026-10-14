package sqlite

import (
	"context"
	"errors"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"gorm.io/gorm"
)

func (s *Store) CreateSessionForUser(ctx context.Context, userID int64, ttl time.Duration) (domain.Session, error) {
	user, err := s.GetLocalUserByID(ctx, userID)
	if err != nil {
		return domain.Session{}, err
	}
	if !user.Enabled {
		return domain.Session{}, domain.ErrNotFound
	}
	now := s.now().UTC()
	token, err := randomToken()
	if err != nil {
		return domain.Session{}, err
	}
	session := domain.Session{
		Token: token, UserID: user.ID, Username: user.Username, Role: user.Role,
		ExpiresAt: now.Add(ttl), CreatedAt: now, UpdatedAt: now,
	}
	record := sessionModel{
		Token: session.Token, UserID: session.UserID,
		ExpiresAt: newSQLiteTimestamp(session.ExpiresAt),
		CreatedAt: newSQLiteTimestamp(session.CreatedAt), UpdatedAt: newSQLiteTimestamp(session.UpdatedAt),
	}
	return session, s.db.WithContext(ctx).Create(&record).Error
}

func (s *Store) GetSession(ctx context.Context, token string) (domain.Session, error) {
	if token == "" {
		return domain.Session{}, domain.ErrNotFound
	}
	var record sessionModel
	err := s.db.WithContext(ctx).Where("token = ?", token).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.Session{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Session{}, err
	}
	if !record.ExpiresAt.Time().After(s.now()) {
		// A concurrent renewal may have extended the session after this read.
		_ = s.db.WithContext(ctx).Where("token = ? AND expires_at = ?", token, record.ExpiresAt).Delete(&sessionModel{}).Error
		return domain.Session{}, domain.ErrNotFound
	}
	user, err := s.GetLocalUserByID(ctx, record.UserID)
	if errors.Is(err, domain.ErrNotFound) || (err == nil && !user.Enabled) {
		_ = s.DeleteSession(ctx, token)
		return domain.Session{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Session{}, err
	}
	return domain.Session{
		Token: record.Token, UserID: user.ID, Username: user.Username, Role: user.Role,
		ExpiresAt: record.ExpiresAt.Time(), CreatedAt: record.CreatedAt.Time(), UpdatedAt: record.UpdatedAt.Time(),
	}, nil
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	return s.db.WithContext(ctx).Where("token = ?", token).Delete(&sessionModel{}).Error
}

// RenewSession extends a live session only after local authentication.
// The transaction prevents renewal from recreating a revoked session or
// shortening a newer expiry. Compare parsed timestamps to retain nanosecond precision.
func (s *Store) RenewSession(ctx context.Context, token string, emby bool, ttl time.Duration) (domain.Session, error) {
	var session domain.Session
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record sessionModel
		err := tx.Where("token = ? AND emby = ?", token, emby).Take(&record).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.ErrNotFound
		}
		if err != nil {
			return err
		}
		now := s.now().UTC()
		if !record.ExpiresAt.Time().After(now) {
			return domain.ErrNotFound
		}
		store := NewStoreWithClock(tx, func() time.Time { return now })
		session, err = store.GetSession(ctx, token)
		if err != nil {
			return err
		}
		expires := now.Add(ttl)
		if !expires.After(session.ExpiresAt) {
			return nil
		}
		result := tx.Model(&sessionModel{}).
			Where("token = ? AND emby = ? AND expires_at = ?", token, emby, record.ExpiresAt).
			Updates(map[string]any{"expires_at": newSQLiteTimestamp(expires), "updated_at": newSQLiteTimestamp(now)})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return domain.ErrNotFound
		}
		session.ExpiresAt = expires
		session.UpdatedAt = now
		return nil
	})
	if err != nil {
		return domain.Session{}, err
	}
	return session, nil
}
