package sqlite

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
)

func bindingTestItem(t *testing.T, store *Store) medialibrary.Item {
	t.Helper()
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{Name: "TV", ContentType: medialibrary.ContentTVShows, RootPath: "/TV"})
	if err != nil {
		t.Fatal(err)
	}
	tmdbID := int64(123)
	item, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{LibraryID: library.ID, Kind: medialibrary.KindEpisode, Path: "/TV/episode.mkv", Name: "Episode", TMDBID: &tmdbID})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func assertMetadataBinding(t *testing.T, store *Store, id int64, want *medialibrary.MetadataBinding) {
	t.Helper()
	got, err := store.GetMediaMetadataBinding(t.Context(), id)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("binding = %#v, %v; want %#v", got, err, want)
	}
}

func TestMetadataBindingAdditiveInitialization(t *testing.T) {
	store, db := openUninitializedTestStore(t, filepath.Join(t.TempDir(), "existing.db"))
	// Build the previous schema without ever dropping an existing table.
	for _, model := range schemaModels {
		if _, binding := model.(*metadataBindingModel); binding {
			continue
		}
		if err := db.AutoMigrate(model); err != nil {
			t.Fatal(err)
		}
	}
	now := newSQLiteTimestamp(store.now())
	library := mediaLibraryModel{Name: "Existing", ContentType: "movies", RootPath: "/Existing", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&library).Error; err != nil {
		t.Fatal(err)
	}
	item := mediaItemModel{LibraryID: library.ID, Kind: "movie", Path: "/Existing/movie.mkv", Name: "Preserved", SortName: "Preserved", GenresJSON: "[]", ArtistsJSON: "[]", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasTable(&metadataBindingModel{}) {
		t.Fatal("binding table exists before initialization")
	}
	for range 2 {
		if err := store.InitializeSchema(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.GetMediaItem(t.Context(), item.ID)
	if err != nil || got.Name != "Preserved" || got.MetadataBinding != nil {
		t.Fatalf("existing item = %#v, %v", got, err)
	}
	binding := &medialibrary.MetadataBinding{SourceType: medialibrary.MetadataSourceTMDBMovie, SourceID: 456}
	if err := store.db.Create(&metadataBindingModel{ItemID: item.ID, SourceType: binding.SourceType, SourceID: binding.SourceID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeSchema(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertMetadataBinding(t, store, item.ID, binding)
}

func TestHistoricalBindingsRemainReadOnly(t *testing.T) {
	store := newMediaLibraryStore(t)
	item := bindingTestItem(t, store)
	if err := store.db.Create(&metadataBindingModel{ItemID: item.ID, SourceType: "tmdb_movie", SourceID: 42}).Error; err != nil {
		t.Fatal(err)
	}
	err := store.UpdateMediaItemMetadata(t.Context(), item.ID, medialibrary.MetadataInput{Name: "Changed", MetadataBindingSet: true})
	if err == nil {
		t.Fatal("legacy binding write accepted")
	}
	assertMetadataBinding(t, store, item.ID, &medialibrary.MetadataBinding{SourceType: "tmdb_movie", SourceID: 42})
}
