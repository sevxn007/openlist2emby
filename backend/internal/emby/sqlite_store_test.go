package emby

import (
	"path/filepath"
	"testing"
	"time"

	glebarezsqlite "github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestSQLiteStore(t *testing.T) (*SQLiteStore, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(glebarezsqlite.Open(filepath.Join(t.TempDir(), "emby-store.db")), &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Silent),
		TranslateError: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	store, err := newSQLiteStoreWithClock(t.Context(), db, func() time.Time {
		return time.Date(2026, 7, 27, 8, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	return store, db
}

func TestSQLiteStoreInitializesSchemaAndPersistsIdentityAndTokens(t *testing.T) {
	store, db := newTestSQLiteStore(t)
	if _, err := NewSQLiteStore(t.Context(), db); err != nil {
		t.Fatalf("second NewSQLiteStore() error = %v", err)
	}
	serverID, err := store.GetOrCreateEmbyServerID(t.Context(), "server-one")
	if err != nil || serverID != "server-one" {
		t.Fatalf("first server ID = %q error = %v", serverID, err)
	}
	serverID, err = store.GetOrCreateEmbyServerID(t.Context(), "server-two")
	if err != nil || serverID != "server-one" {
		t.Fatalf("stable server ID = %q error = %v", serverID, err)
	}

	input := TokenInput{UserID: 42, DeviceID: "device-1", DeviceName: "Phone", ClientName: "Emby", ClientVersion: "1.0"}
	if err := store.CreateEmbyToken(t.Context(), "token-hash", input); err != nil {
		t.Fatal(err)
	}
	identity, valid, err := store.GetEmbyTokenIdentity(t.Context(), "token-hash")
	if err != nil || !valid {
		t.Fatalf("token valid = %v error = %v", valid, err)
	}
	if identity.UserID != input.UserID {
		t.Fatalf("token user ID = %d, want %d", identity.UserID, input.UserID)
	}

	var record tokenRecord
	if err := store.db.WithContext(t.Context()).Where("token_hash = ?", "token-hash").First(&record).Error; err != nil {
		t.Fatal(err)
	}
	if record.UserID != input.UserID || record.DeviceID != input.DeviceID || record.DeviceName != input.DeviceName || record.ClientName != input.ClientName || record.ClientVersion != input.ClientVersion {
		t.Fatalf("stored token metadata = %#v", record)
	}

	if err := store.RevokeEmbyToken(t.Context(), "token-hash"); err != nil {
		t.Fatal(err)
	}
	valid, err = store.ValidateEmbyToken(t.Context(), "token-hash")
	if err != nil || valid {
		t.Fatalf("revoked token valid = %v error = %v", valid, err)
	}
}

func TestSQLiteStoreRevokesOnlyRequestedUserTokens(t *testing.T) {
	store, _ := newTestSQLiteStore(t)
	if err := store.CreateEmbyToken(t.Context(), "user-one-token", TokenInput{UserID: 1, DeviceID: "device-1"}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateEmbyToken(t.Context(), "user-two-token", TokenInput{UserID: 2, DeviceID: "device-2"}); err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeUserTokens(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	if valid, err := store.ValidateEmbyToken(t.Context(), "user-one-token"); err != nil || valid {
		t.Fatalf("revoked user-one token valid = %v error = %v", valid, err)
	}
	identity, valid, err := store.GetEmbyTokenIdentity(t.Context(), "user-two-token")
	if err != nil || !valid || identity.UserID != 2 {
		t.Fatalf("user-two token identity = %#v valid = %v error = %v", identity, valid, err)
	}
	if err := store.CreateEmbyToken(t.Context(), "invalid-user-token", TokenInput{DeviceID: "device-3"}); err == nil {
		t.Fatal("CreateEmbyToken() succeeded without a user ID")
	}
}
