package sqlite

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	glebarezsqlite "github.com/glebarez/sqlite"
	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"gorm.io/gorm"
)

func openUninitializedTestStore(t *testing.T, path string) (*Store, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(glebarezsqlite.Open(path), gormConfig())
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	t.Cleanup(func() { _ = store.Close() })
	return store, db
}

func assertFileMode(t *testing.T, path string, expected os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != expected {
		t.Fatalf("%s mode = %04o, want %04o", path, mode, expected)
	}
}

func TestInitializeSchemaCreatesCurrentSchema(t *testing.T) {
	store, db := openUninitializedTestStore(t, filepath.Join(t.TempDir(), "current.db"))
	if err := store.InitializeSchema(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeSchema(t.Context()); err != nil {
		t.Fatalf("second InitializeSchema() error = %v", err)
	}

	for _, name := range []string{
		"settings", "user_authorizations", "sessions", "user_library_grants", "tasks", "task_runs", "task_run_items",
		"media_libraries", "media_library_roots", "media_items", "media_item_images", "media_people", "media_item_people", "media_user_data",
	} {
		if !db.Migrator().HasTable(name) {
			t.Errorf("current schema is missing table %q", name)
		}
	}
	columns, err := db.Migrator().ColumnTypes(&settingsModel{})
	if err != nil {
		t.Fatal(err)
	}
	for _, column := range columns {
		if column.Name() == "music_api_url" {
			t.Fatal("current schema still creates the removed music_api_url column")
		}
	}
	for _, index := range []struct {
		model any
		name  string
	}{
		{&taskModel{}, "idx_tasks_due"},
		{&taskRunModel{}, "idx_task_runs_queue"},
		{&taskRunModel{}, "idx_task_runs_history"},
		{&taskRunModel{}, "idx_task_runs_schedule"},
		{&taskRunModel{}, "idx_task_runs_finished"},
		{&taskRunModel{}, "idx_task_runs_one_queued_schedule"},
		{&taskRunModel{}, "idx_task_runs_one_running_schedule"},
		{&taskRunModel{}, "idx_task_runs_one_active_media_metadata_migration"},
		{&taskRunItemModel{}, "idx_task_run_items_status"},
		{&taskRunItemModel{}, "idx_task_run_items_action"},
		{&mediaItemModel{}, "idx_media_items_library_parent"},
		{&mediaItemModel{}, "idx_media_items_kind"},
		{&mediaItemModel{}, "idx_media_items_generation"},
		{&mediaItemModel{}, "idx_media_items_parent_kind_created"},
		{&mediaLibraryRootModel{}, "idx_media_library_roots_library"},
		{&mediaLibraryRootModel{}, "idx_media_library_roots_library_position"},
		{&mediaLibraryRootModel{}, "idx_media_library_roots_path"},
	} {
		if !db.Migrator().HasIndex(index.model, index.name) {
			t.Errorf("current schema is missing index %q", index.name)
		}
	}
}

func TestUpdateSettingsPreservesBlankSecrets(t *testing.T) {
	store, _ := openUninitializedTestStore(t, filepath.Join(t.TempDir(), "settings.db"))
	if err := store.InitializeSchema(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveInitialSettings(
		t.Context(),
		"http://openlist.local",
		"admin-key",
		"tmdb-key",
	); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateSettings(t.Context(), "http://openlist.example", "", ""); err != nil {
		t.Fatal(err)
	}

	settings, err := store.GetSettings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if settings.OpenListAdminKey != "admin-key" || settings.TMDBAPIKey != "tmdb-key" {
		t.Fatalf("blank update replaced stored secrets: %#v", settings)
	}
}

func TestCoreTimestampsRoundTripAsRFC3339UTC(t *testing.T) {
	store, db := openUninitializedTestStore(t, filepath.Join(t.TempDir(), "timestamps.db"))
	if err := store.InitializeSchema(t.Context()); err != nil {
		t.Fatal(err)
	}

	zone := time.FixedZone("test-offset", 8*60*60)
	createdAt := time.Date(2042, time.March, 4, 5, 6, 7, 123456789, zone)
	store.now = func() time.Time { return createdAt }
	if err := store.SaveIdentitySetup(
		t.Context(),
		"http://openlist.local",
		"admin-key",
		domain.OpenListIdentity{ID: 1, Username: "local-admin", Role: 2},
	); err != nil {
		t.Fatal(err)
	}

	updatedAt := createdAt.Add(15 * time.Minute)
	store.now = func() time.Time { return updatedAt }
	if err := store.SetMediaLibraryConfiguration(t.Context(), true, domain.DefaultEmbyPort); err != nil {
		t.Fatal(err)
	}
	updatedAt = updatedAt.Add(15 * time.Minute)
	store.now = func() time.Time { return updatedAt }
	if err := store.UpdateSettings(t.Context(), "http://openlist.example", "admin-key-2", "tmdb-key"); err != nil {
		t.Fatal(err)
	}

	settings, err := store.GetSettings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if settings.CreatedAt != createdAt.UTC() || settings.UpdatedAt != updatedAt.UTC() {
		t.Fatalf("settings timestamps = (%s, %s), want (%s, %s)", settings.CreatedAt, settings.UpdatedAt, createdAt.UTC(), updatedAt.UTC())
	}
	user, err := store.GetLocalUserByID(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if user.CreatedAt != createdAt.UTC() || user.UpdatedAt != createdAt.UTC() {
		t.Fatalf("user timestamps = (%s, %s), want %s", user.CreatedAt, user.UpdatedAt, createdAt.UTC())
	}
	session, err := store.CreateSessionForUser(t.Context(), user.ID, 90*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	loadedSession, err := store.GetSession(t.Context(), session.Token)
	if err != nil {
		t.Fatal(err)
	}
	if loadedSession.CreatedAt != updatedAt.UTC() || loadedSession.UpdatedAt != updatedAt.UTC() || loadedSession.ExpiresAt != updatedAt.Add(90*time.Minute).UTC() {
		t.Fatalf("session timestamps = (%s, %s, %s)", loadedSession.CreatedAt, loadedSession.UpdatedAt, loadedSession.ExpiresAt)
	}

	type persistedTimestamps struct {
		CreatedAt string
		UpdatedAt string
		ExpiresAt string
	}
	for _, check := range []struct {
		name  string
		query *gorm.DB
		want  persistedTimestamps
	}{
		{
			name:  "settings",
			query: db.Model(&settingsModel{}).Select("created_at", "updated_at").Where("id = ?", 1),
			want: persistedTimestamps{
				CreatedAt: formatTime(createdAt),
				UpdatedAt: formatTime(updatedAt),
			},
		},
		{
			name:  "user",
			query: db.Model(&localUserModel{}).Select("created_at", "updated_at").Where("id = ?", 1),
			want: persistedTimestamps{
				CreatedAt: formatTime(createdAt),
				UpdatedAt: formatTime(createdAt),
			},
		},
		{
			name:  "session",
			query: db.Model(&sessionModel{}).Select("created_at", "updated_at", "expires_at").Where("token = ?", session.Token),
			want: persistedTimestamps{
				CreatedAt: formatTime(updatedAt),
				UpdatedAt: formatTime(updatedAt),
				ExpiresAt: formatTime(updatedAt.Add(90 * time.Minute)),
			},
		},
	} {
		t.Run(check.name, func(t *testing.T) {
			var persisted persistedTimestamps
			if err := check.query.Take(&persisted).Error; err != nil {
				t.Fatal(err)
			}
			if persisted != check.want {
				t.Fatalf("persisted timestamps = %#v, want %#v", persisted, check.want)
			}
		})
	}
}

func TestOpenSecuresDedicatedDatabaseDirectoryAndFiles(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "manager.db")
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	sqlDB, err := store.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if got := sqlDB.Stats().MaxOpenConnections; got != sqliteMaxOpenConnections {
		t.Fatalf("MaxOpenConnections = %d, want %d", got, sqliteMaxOpenConnections)
	}
	for _, check := range []struct {
		name string
		want int
	}{
		{name: "foreign_keys", want: 1},
		{name: "busy_timeout", want: 5000},
	} {
		var got int
		if err := store.db.Raw("PRAGMA " + check.name).Scan(&got).Error; err != nil {
			t.Fatalf("read PRAGMA %s: %v", check.name, err)
		}
		if got != check.want {
			t.Errorf("PRAGMA %s = %d, want %d", check.name, got, check.want)
		}
	}
	var journalMode string
	if err := store.db.Raw("PRAGMA journal_mode").Scan(&journalMode).Error; err != nil {
		t.Fatalf("read PRAGMA journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("PRAGMA journal_mode = %q, want wal", journalMode)
	}
	assertFileMode(t, directory, 0o700)
	assertFileMode(t, path, 0o600)
	for _, suffix := range []string{"-shm", "-wal"} {
		if _, err := os.Stat(path + suffix); err == nil {
			assertFileMode(t, path+suffix, 0o600)
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
}

func TestOpenDoesNotRestrictSharedParentDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "keep.txt"), []byte("shared"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatal(err)
	}

	store, err := Open(filepath.Join(directory, "manager.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	assertFileMode(t, directory, 0o755)
	assertFileMode(t, filepath.Join(directory, "manager.db"), 0o600)
}
