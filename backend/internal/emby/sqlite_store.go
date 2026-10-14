package emby

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type tokenRecord struct {
	IdentitySession string     `gorm:"not null;default:''"`
	ID              int64      `gorm:"column:id;primaryKey;autoIncrement"`
	TokenHash       string     `gorm:"column:token_hash;not null;unique"`
	UserID          int64      `gorm:"column:user_id;not null;index:idx_emby_tokens_user"`
	DeviceID        string     `gorm:"column:device_id;not null;index:idx_emby_tokens_device,priority:1"`
	DeviceName      string     `gorm:"column:device_name;not null"`
	ClientName      string     `gorm:"column:client_name;not null"`
	ClientVersion   string     `gorm:"column:client_version;not null"`
	CreatedAt       time.Time  `gorm:"column:created_at;not null"`
	LastUsedAt      time.Time  `gorm:"column:last_used_at;not null"`
	RevokedAt       *time.Time `gorm:"column:revoked_at;index:idx_emby_tokens_device,priority:2"`
}

func (tokenRecord) TableName() string {
	return "emby_tokens"
}

type serverStateRecord struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement:false"`
	ServerID  string    `gorm:"column:server_id;not null"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
}

func (serverStateRecord) TableName() string {
	return "emby_state"
}

// SQLiteStore persists Emby identity and access tokens in the application's
// shared GORM session. The caller retains ownership of the underlying database.
type SQLiteStore struct {
	db  *gorm.DB
	now func() time.Time
}

var _ TokenStore = (*SQLiteStore)(nil)

func NewSQLiteStore(ctx context.Context, db *gorm.DB) (*SQLiteStore, error) {
	return newSQLiteStoreWithClock(ctx, db, time.Now)
}

func newSQLiteStoreWithClock(ctx context.Context, db *gorm.DB, now func() time.Time) (*SQLiteStore, error) {
	if db == nil {
		return nil, errors.New("Emby database is required")
	}
	if now == nil {
		now = time.Now
	}
	db = db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
	store := &SQLiteStore{db: db, now: now}
	if err := initializeSQLiteSchema(ctx, store.db); err != nil {
		return nil, fmt.Errorf("initialize Emby schema: %w", err)
	}
	return store, nil
}

func initializeSQLiteSchema(ctx context.Context, db *gorm.DB) error {
	db = db.WithContext(ctx)
	for _, model := range []any{&tokenRecord{}, &serverStateRecord{}} {
		if db.Migrator().HasTable(model) {
			continue
		}
		if err := db.Migrator().CreateTable(model); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) CreateEmbyToken(ctx context.Context, tokenHash string, input TokenInput) error {
	if input.UserID <= 0 {
		return errors.New("Emby token user ID is required")
	}
	now := s.now().UTC()
	record := &tokenRecord{
		TokenHash:       tokenHash,
		IdentitySession: input.IdentitySession,
		UserID:          input.UserID,
		DeviceID:        input.DeviceID,
		DeviceName:      input.DeviceName,
		ClientName:      input.ClientName,
		ClientVersion:   input.ClientVersion,
		CreatedAt:       now,
		LastUsedAt:      now,
	}
	if err := s.db.WithContext(ctx).Create(record).Error; err != nil {
		return fmt.Errorf("persist access token: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetEmbyTokenIdentity(ctx context.Context, tokenHash string) (TokenIdentity, bool, error) {
	var record tokenRecord
	result := s.db.WithContext(ctx).
		Where("token_hash = ?", tokenHash).
		Where("revoked_at IS NULL").
		First(&record)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return TokenIdentity{}, false, nil
	}
	if result.Error != nil {
		return TokenIdentity{}, false, fmt.Errorf("read access token: %w", result.Error)
	}
	if err := s.db.WithContext(ctx).Model(&record).Update("last_used_at", s.now().UTC()).Error; err != nil {
		return TokenIdentity{}, false, fmt.Errorf("update access token: %w", err)
	}
	return TokenIdentity{UserID: record.UserID, IdentitySession: record.IdentitySession}, true, nil
}

// ValidateEmbyToken remains available for callers that only need a validity
// check. New authentication paths should use GetEmbyTokenIdentity.
func (s *SQLiteStore) ValidateEmbyToken(ctx context.Context, tokenHash string) (bool, error) {
	_, valid, err := s.GetEmbyTokenIdentity(ctx, tokenHash)
	return valid, err
}

func (s *SQLiteStore) RevokeEmbyToken(ctx context.Context, tokenHash string) error {
	result := s.db.WithContext(ctx).
		Model(&tokenRecord{}).
		Where("token_hash = ?", tokenHash).
		Where("revoked_at IS NULL").
		Update("revoked_at", s.now().UTC())
	if result.Error != nil {
		return fmt.Errorf("revoke access token: %w", result.Error)
	}
	return nil
}

func (s *SQLiteStore) RevokeEmbyTokensForUser(ctx context.Context, userID int64) error {
	if userID <= 0 {
		return errors.New("Emby token user ID is required")
	}
	result := s.db.WithContext(ctx).
		Model(&tokenRecord{}).
		Where("user_id = ?", userID).
		Where("revoked_at IS NULL").
		Update("revoked_at", s.now().UTC())
	if result.Error != nil {
		return fmt.Errorf("revoke user access tokens: %w", result.Error)
	}
	return nil
}

// RevokeUserTokens lets app.Service revoke Emby device credentials through its
// transport-agnostic account-lifecycle callback.
func (s *SQLiteStore) RevokeUserTokens(ctx context.Context, userID int64) error {
	return s.RevokeEmbyTokensForUser(ctx, userID)
}

func (s *SQLiteStore) GetOrCreateEmbyServerID(ctx context.Context, proposed string) (string, error) {
	record := &serverStateRecord{ID: 1, ServerID: proposed, CreatedAt: s.now().UTC()}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(record).Error; err != nil {
		return "", fmt.Errorf("persist server ID: %w", err)
	}
	if err := s.db.WithContext(ctx).First(record, 1).Error; err != nil {
		return "", fmt.Errorf("read server ID: %w", err)
	}
	return record.ServerID, nil
}
