package sqlite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	glebarezsqlite "github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Store struct {
	db  *gorm.DB
	now func() time.Time
}

// SQLite permits only one writer at a time. Scans intentionally parallelize
// network and metadata work, so a single pooled connection prevents concurrent
// goroutines from holding competing write transactions on low-resource hosts.
const sqliteMaxOpenConnections = 1

func Open(path string) (*Store, error) {
	if err := prepareDatabasePath(path); err != nil {
		return nil, err
	}
	// Connection-local pragmas belong in the DSN so every pooled connection
	// enforces the same constraints and lock wait policy.
	dsn := path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db, err := gorm.Open(glebarezsqlite.Open(dsn), gormConfig())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("access database connection: %w", err)
	}
	// Keep schema initialization on one connection. WAL is persistent, and the
	// pool is widened only after initialization succeeds.
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	closeOnError := func(openErr error) (*Store, error) {
		_ = sqlDB.Close()
		return nil, openErr
	}
	for _, pragma := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
	} {
		if err := db.Exec(pragma).Error; err != nil {
			return closeOnError(fmt.Errorf("configure database: %w", err))
		}
	}
	store := NewStore(db)
	if err := store.InitializeSchema(context.Background()); err != nil {
		return closeOnError(fmt.Errorf("initialize database schema: %w", err))
	}
	sqlDB.SetMaxOpenConns(sqliteMaxOpenConnections)
	sqlDB.SetMaxIdleConns(sqliteMaxOpenConnections)
	if err := secureDatabaseFiles(path); err != nil {
		return closeOnError(err)
	}
	return store, nil
}

func gormConfig() *gorm.Config {
	return &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Silent),
		TranslateError: true,
	}
}

func prepareDatabasePath(path string) error {
	if path == "" {
		return fmt.Errorf("database path is required")
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}
	private, err := containsOnlyDatabaseFiles(directory, filepath.Base(path))
	if err != nil {
		return err
	}
	if private {
		if err := os.Chmod(directory, 0o700); err != nil {
			return fmt.Errorf("secure database directory: %w", err)
		}
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("open database file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close database file: %w", err)
	}
	return secureDatabaseFiles(path)
}

func containsOnlyDatabaseFiles(directory, databaseName string) (bool, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return false, fmt.Errorf("read database directory: %w", err)
	}
	allowed := map[string]struct{}{
		databaseName:              {},
		databaseName + "-journal": {},
		databaseName + "-shm":     {},
		databaseName + "-wal":     {},
	}
	for _, entry := range entries {
		if _, ok := allowed[entry.Name()]; !ok {
			return false, nil
		}
	}
	return true, nil
}

func secureDatabaseFiles(path string) error {
	for _, candidate := range []string{path, path + "-journal", path + "-shm", path + "-wal"} {
		if err := os.Chmod(candidate, 0o600); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("secure database file %q: %w", filepath.Base(candidate), err)
		}
	}
	return nil
}

func NewStore(db *gorm.DB) *Store {
	return NewStoreWithClock(db, time.Now)
}

func NewStoreWithClock(db *gorm.DB, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	// Repository queries always use a silent logger because records include
	// administrator keys, password hashes, and session tokens.
	db = db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
	return &Store{db: db, now: now}
}

func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// DB returns the shared GORM session for package-owned persistence adapters.
// The Store retains ownership of the connection; callers must not close it.
func (s *Store) DB() *gorm.DB {
	return s.db
}
