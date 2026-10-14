package emby

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
)

type playedItemsStore struct {
	medialibrary.SettingsStore
	item    medialibrary.Item
	allowed bool
}

func (s *playedItemsStore) GetSettings(context.Context) (domain.Settings, error) {
	return domain.Settings{MediaLibraryEnabled: true}, nil
}

func (s *playedItemsStore) GetMediaItemForUser(_ context.Context, _ int64, itemID int64) (medialibrary.Item, error) {
	if itemID != s.item.ID {
		return medialibrary.Item{}, domain.ErrNotFound
	}
	return s.item, nil
}

func (s *playedItemsStore) UserCanAccessLibrary(context.Context, int64, int64) (bool, error) {
	return s.allowed, nil
}

func (s *playedItemsStore) UserCanEditLibrary(context.Context, int64, int64) (bool, error) {
	return false, nil
}

func (s *playedItemsStore) UpdateMediaUserDataForUser(_ context.Context, _ int64, itemID int64, data medialibrary.UserData) (medialibrary.UserData, error) {
	if itemID != s.item.ID {
		return medialibrary.UserData{}, domain.ErrNotFound
	}
	s.item.UserData = data
	return data, nil
}

func newPlayedItemsHandler(store *playedItemsStore) (*Server, viewer) {
	media := medialibrary.NewService(store, nil, nil)
	user := domain.LocalUser{ID: 3, Role: domain.LocalUserRoleMedia, Enabled: true}
	return &Server{media: media}, viewer{user: user}
}

func TestHandlePlayedItemsMarksAndUnmarksItem(t *testing.T) {
	lastPlayed := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	store := &playedItemsStore{
		item: medialibrary.Item{
			ID: 7, LibraryID: 4,
			UserData: medialibrary.UserData{
				PlaybackPositionTicks: 300, PlayCount: 2, Favorite: true, LastPlayedAt: &lastPlayed,
			},
		},
		allowed: true,
	}
	server, currentViewer := newPlayedItemsHandler(store)

	mark := httptest.NewRecorder()
	server.handlePlayedItems(mark, httptest.NewRequest(http.MethodPost, "/Users/3/PlayedItems/7", nil), []string{"users", "3", "playeditems", "7"}, "", currentViewer)
	marked := decodePlayedItemData(t, mark)
	if mark.Code != http.StatusOK || marked.ItemId != "7" || !marked.Played || marked.PlayCount != 3 || marked.PlaybackPositionTicks != 0 || !marked.IsFavorite || marked.LastPlayedDate == "" {
		t.Fatalf("mark response status = %d data = %#v", mark.Code, marked)
	}

	repeat := httptest.NewRecorder()
	server.handlePlayedItems(repeat, httptest.NewRequest(http.MethodPost, "/Users/3/PlayedItems/7", nil), []string{"users", "3", "playeditems", "7"}, "", currentViewer)
	repeated := decodePlayedItemData(t, repeat)
	if repeat.Code != http.StatusOK || repeated.PlayCount != 3 {
		t.Fatalf("repeat response status = %d data = %#v", repeat.Code, repeated)
	}

	unmark := httptest.NewRecorder()
	server.handlePlayedItems(unmark, httptest.NewRequest(http.MethodDelete, "/Users/3/PlayedItems/7", nil), []string{"users", "3", "playeditems", "7"}, "", currentViewer)
	unmarked := decodePlayedItemData(t, unmark)
	if unmark.Code != http.StatusOK || unmarked.Played || unmarked.PlayCount != 3 || unmarked.PlaybackPositionTicks != 0 || !unmarked.IsFavorite || unmarked.LastPlayedDate == "" {
		t.Fatalf("unmark response status = %d data = %#v", unmark.Code, unmarked)
	}

	alias := httptest.NewRecorder()
	server.handlePlayedItems(alias, httptest.NewRequest(http.MethodPost, "/Users/3/PlayedItems/7/Delete", nil), []string{"users", "3", "playeditems", "7", "delete"}, "", currentViewer)
	aliased := decodePlayedItemData(t, alias)
	if alias.Code != http.StatusOK || aliased.Played || aliased.PlayCount != 3 || !aliased.IsFavorite {
		t.Fatalf("delete alias response status = %d data = %#v", alias.Code, aliased)
	}
}

func TestHandlePlayedItemsValidatesUserItemAndAccess(t *testing.T) {
	store := &playedItemsStore{item: medialibrary.Item{ID: 7, LibraryID: 4}, allowed: false}
	server, currentViewer := newPlayedItemsHandler(store)

	tests := []struct {
		name string
		path []string
		want int
	}{
		{name: "different user", path: []string{"users", "2", "playeditems", "7"}, want: http.StatusNotFound},
		{name: "invalid item", path: []string{"users", "3", "playeditems", "zero"}, want: http.StatusBadRequest},
		{name: "ungranted library", path: []string{"users", "3", "playeditems", "7"}, want: http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			server.handlePlayedItems(recorder, httptest.NewRequest(http.MethodPost, "/", nil), test.path, "", currentViewer)
			if recorder.Code != test.want {
				t.Fatalf("status = %d body = %s, want %d", recorder.Code, recorder.Body.String(), test.want)
			}
		})
	}
}

func decodePlayedItemData(t *testing.T, recorder *httptest.ResponseRecorder) userItemDataDTO {
	t.Helper()
	var data userItemDataDTO
	if err := json.Unmarshal(recorder.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	return data
}
