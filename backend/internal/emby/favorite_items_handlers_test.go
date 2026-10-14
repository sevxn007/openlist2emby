package emby_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
)

type favoriteUserDataResponse struct {
	ItemID                string `json:"ItemId"`
	PlaybackPositionTicks int64  `json:"PlaybackPositionTicks"`
	PlayCount             int    `json:"PlayCount"`
	IsFavorite            bool   `json:"IsFavorite"`
	Played                bool   `json:"Played"`
	LastPlayedDate        string `json:"LastPlayedDate"`
}

func TestFavoriteItemEndpointsOnlyChangeFavoriteState(t *testing.T) {
	server, store, _, item, _ := newEmbyTestServer(t)
	lastPlayedAt := time.Date(2026, time.August, 7, 12, 34, 56, 0, time.UTC)
	if _, err := store.UpdateMediaUserDataForUser(t.Context(), 1, item.ID, medialibrary.UserData{
		PlaybackPositionTicks: 3_000_000,
		Played:                true,
		PlayCount:             4,
		LastPlayedAt:          &lastPlayedAt,
	}); err != nil {
		t.Fatal(err)
	}
	auth := authenticateEmby(t, server)
	itemID := strconv.FormatInt(item.ID, 10)

	request := func(method, suffix string) favoriteUserDataResponse {
		t.Helper()
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(method,
			"/emby/Users/1/FavoriteItems/"+itemID+suffix+"?api_key="+auth.AccessToken, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s %s status = %d body = %s", method, suffix, recorder.Code, recorder.Body.String())
		}
		var response favoriteUserDataResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		return response
	}

	favorited := request(http.MethodPost, "")
	if favorited.ItemID != itemID || !favorited.IsFavorite || favorited.PlaybackPositionTicks != 3_000_000 || !favorited.Played || favorited.PlayCount != 4 || favorited.LastPlayedDate == "" {
		t.Fatalf("favorite response = %#v", favorited)
	}
	unfavorited := request(http.MethodDelete, "")
	if unfavorited.IsFavorite || unfavorited.PlaybackPositionTicks != 3_000_000 || !unfavorited.Played || unfavorited.PlayCount != 4 || unfavorited.LastPlayedDate == "" {
		t.Fatalf("unfavorite response = %#v", unfavorited)
	}

	request(http.MethodPost, "")
	aliasResponse := request(http.MethodPost, "/Delete")
	if aliasResponse.IsFavorite {
		t.Fatalf("delete alias response = %#v", aliasResponse)
	}
	stored, err := store.GetMediaItemForUser(t.Context(), 1, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.UserData.Favorite || stored.UserData.PlaybackPositionTicks != 3_000_000 || !stored.UserData.Played || stored.UserData.PlayCount != 4 || stored.UserData.LastPlayedAt == nil || !stored.UserData.LastPlayedAt.Equal(lastPlayedAt) {
		t.Fatalf("stored user data = %#v", stored.UserData)
	}
}

func TestFavoriteItemEndpointsValidateUserAndItemAccess(t *testing.T) {
	server, store, _, item, _ := newEmbyTestServer(t)
	privateLibrary, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Private", ContentType: medialibrary.ContentMovies, RootPath: "/Private",
	})
	if err != nil {
		t.Fatal(err)
	}
	generation, err := store.BeginLibraryScan(t.Context(), privateLibrary.ID)
	if err != nil {
		t.Fatal(err)
	}
	privateItem, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: privateLibrary.ID, Kind: medialibrary.KindMovie, Path: "/Private/Secret.mkv",
		MediaPath: "/Private/Secret.mkv", Name: "Secret", SortName: "Secret", Container: "mkv",
		Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishLibraryScan(t.Context(), privateLibrary.ID, generation); err != nil {
		t.Fatal(err)
	}
	bob, err := store.CreateUser(t.Context(), domain.LocalUser{OpenListUserID: 101, InstanceID: "test-instance",
		Username: "bob", Role: domain.LocalUserRoleMedia, Enabled: true,
		LibraryGrants: []domain.LibraryGrant{{LibraryID: item.LibraryID, Access: domain.LibraryAccessView}},
	})
	if err != nil {
		t.Fatal(err)
	}
	auth := authenticateEmbyAs(t, server, "bob", "bob-password", "bob-favorites")

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{name: "invalid item ID", path: "/emby/Users/" + strconv.FormatInt(bob.ID, 10) + "/FavoriteItems/not-an-id", wantStatus: http.StatusBadRequest},
		{name: "other user path", path: "/emby/Users/1/FavoriteItems/" + strconv.FormatInt(item.ID, 10), wantStatus: http.StatusNotFound},
		{name: "unauthorized item", path: "/emby/Users/" + strconv.FormatInt(bob.ID, 10) + "/FavoriteItems/" + strconv.FormatInt(privateItem.ID, 10), wantStatus: http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, test.path+"?api_key="+auth.AccessToken, nil))
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d body = %s, want %d", recorder.Code, recorder.Body.String(), test.wantStatus)
			}
		})
	}
	stored, err := store.GetMediaItemForUser(t.Context(), bob.ID, privateItem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.UserData.Favorite {
		t.Fatal("unauthorized request changed favorite state")
	}
}

func TestFavoriteItemEndpointReturnsNotFoundWhenMediaLibraryIsDisabled(t *testing.T) {
	server, store, _, item, _ := newEmbyTestServer(t)
	auth := authenticateEmby(t, server)
	if err := store.SetMediaLibraryConfiguration(t.Context(), false, domain.DefaultEmbyPort); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost,
		"/emby/Users/1/FavoriteItems/"+strconv.FormatInt(item.ID, 10)+"?api_key="+auth.AccessToken, nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestFavoriteItemsFilterOnlyReturnsCurrentUsersFavorites(t *testing.T) {
	server, store, _, favoriteItem, _ := newEmbyTestServer(t)
	generation, err := store.BeginLibraryScan(t.Context(), favoriteItem.LibraryID)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: favoriteItem.LibraryID, Kind: medialibrary.KindMovie,
		Path: "/Movies/Arrival.mkv", MediaPath: "/Movies/Arrival.mkv",
		Name: "Arrival", SortName: "Arrival", Container: "mkv", ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateMediaUserDataForUser(t.Context(), 1, favoriteItem.ID, medialibrary.UserData{Favorite: true}); err != nil {
		t.Fatal(err)
	}
	auth := authenticateEmby(t, server)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet,
		"/emby/Users/1/Items?Filters=IsFavorite&api_key="+auth.AccessToken, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var result struct {
		Items []struct {
			ID       string `json:"Id"`
			UserData struct {
				IsFavorite bool `json:"IsFavorite"`
			} `json:"UserData"`
		} `json:"Items"`
		TotalRecordCount int `json:"TotalRecordCount"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.TotalRecordCount != 1 || len(result.Items) != 1 ||
		result.Items[0].ID != strconv.FormatInt(favoriteItem.ID, 10) || !result.Items[0].UserData.IsFavorite {
		t.Fatalf("favorite items = %#v body = %s", result, recorder.Body.String())
	}
}
