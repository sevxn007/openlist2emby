package medialibrary

import (
	"context"
	"testing"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

type standaloneRefreshStore struct {
	SettingsStore
	library Library
	item    Item
	updated int64
}

func (s *standaloneRefreshStore) GetSettings(context.Context) (domain.Settings, error) {
	return domain.Settings{MediaLibraryEnabled: true, TMDBAPIKey: "synthetic-tmdb-key"}, nil
}

func (s *standaloneRefreshStore) GetMediaItemForUser(_ context.Context, _, id int64) (Item, error) {
	if id != s.item.ID {
		return Item{}, domain.ErrNotFound
	}
	return s.item, nil
}

func (s *standaloneRefreshStore) GetLibrary(_ context.Context, id int64) (Library, error) {
	if id != s.library.ID {
		return Library{}, domain.ErrNotFound
	}
	return s.library, nil
}

func (s *standaloneRefreshStore) UpdateMediaItemMetadata(_ context.Context, id int64, _ MetadataInput) error {
	s.updated = id
	return nil
}

func TestRefreshStandaloneSeriesSkipsProviderEpisodeDetails(t *testing.T) {
	library := Library{ID: 7, RootPath: "/Shows", RootPaths: []string{"/Shows", "/Archive"}, ContentType: ContentTVShows}
	store := &standaloneRefreshStore{
		library: library,
		item: Item{ID: 12, LibraryID: library.ID, Kind: KindSeries,
			Path: standaloneSeriesPrefix(library.RootPath) + "Archive/One/Severance.mkv", Name: "Severance"},
	}
	provider := &tvMetadataProvider{}
	service := NewService(store, nil, provider)
	tmdbID := int64(100)
	if _, err := service.RefreshMetadataForUser(t.Context(), UserAccess{UserID: 1, IsAdmin: true}, store.item.ID, RefreshMetadataInput{TMDBID: &tmdbID, MetadataSource: "tmdb"}); err != nil {
		t.Fatal(err)
	}
	if store.updated != store.item.ID || len(provider.detailSeasons) != 1 || provider.detailSeasons[0] != -1 {
		t.Fatalf("updated = %d, provider seasons = %v", store.updated, provider.detailSeasons)
	}
}
