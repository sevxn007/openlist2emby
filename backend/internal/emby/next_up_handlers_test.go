package emby_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
)

func TestEmbyNextUpReturnsFirstUnplayedEpisodeForViewer(t *testing.T) {
	var media *medialibrary.Service
	server, store, _, inaccessibleMovie, _ := newEmbyTestServer(t, func(service *medialibrary.Service) {
		media = service
	})
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "TV", ContentType: medialibrary.ContentTVShows, RootPath: "/TV",
	})
	if err != nil {
		t.Fatal(err)
	}
	generation, err := store.BeginLibraryScan(t.Context(), library.ID)
	if err != nil {
		t.Fatal(err)
	}
	series, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, Kind: medialibrary.KindSeries, Path: "/TV/Severance",
		Name: "Severance", SortName: "Severance", Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	season2Number := 2
	season2, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, ParentID: &series.ID, Kind: medialibrary.KindSeason,
		Path: "/TV/Severance/Season 02", Name: "Season 2", SortName: "0002",
		SeasonNumber: &season2Number, Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	episode21Number := 1
	episode21, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, ParentID: &season2.ID, Kind: medialibrary.KindEpisode,
		Path: "/TV/Severance/Season 02/S02E01.mkv", MediaPath: "/TV/Severance/Season 02/S02E01.mkv",
		Name: "Episode 2.1", SortName: "0001", Container: "mkv", RuntimeTicks: 10_000_000,
		SeasonNumber: &season2Number, EpisodeNumber: &episode21Number, Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	season1Number := 1
	season1, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, ParentID: &series.ID, Kind: medialibrary.KindSeason,
		Path: "/TV/Severance/Season 01", Name: "Season 1", SortName: "0001",
		SeasonNumber: &season1Number, Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	episode12Number := 2
	episode12, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, ParentID: &season1.ID, Kind: medialibrary.KindEpisode,
		Path: "/TV/Severance/Season 01/S01E02.mkv", MediaPath: "/TV/Severance/Season 01/S01E02.mkv",
		Name: "Episode 1.2", SortName: "0002", Container: "mkv", RuntimeTicks: 10_000_000,
		SeasonNumber: &season1Number, EpisodeNumber: &episode12Number, Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	episode11Number := 1
	episode11, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, ParentID: &season1.ID, Kind: medialibrary.KindEpisode,
		Path: "/TV/Severance/Season 01/S01E01.mkv", MediaPath: "/TV/Severance/Season 01/S01E01.mkv",
		Name: "Episode 1.1", SortName: "0001", Container: "mkv", RuntimeTicks: 10_000_000,
		SeasonNumber: &season1Number, EpisodeNumber: &episode11Number, Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishLibraryScan(t.Context(), library.ID, generation); err != nil {
		t.Fatal(err)
	}

	assets, err := medialibrary.NewAssetFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	media.SetAssetFiles(assets)
	picture, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	cover, err := assets.StoreItemImage(t.Context(), series.ID, medialibrary.ImagePrimary, bytes.NewReader(picture))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertMediaItemImage(t.Context(), medialibrary.LocalImage{
		ItemID: series.ID, ImageType: medialibrary.ImagePrimary, SourceType: medialibrary.ImageSourceRemote,
		RelativePath: cover.RelativePath, MIMEType: cover.MIMEType, SizeBytes: cover.SizeBytes,
		Width: cover.Width, Height: cover.Height, SHA256: cover.SHA256, UpdatedAt: cover.UpdatedAt,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateMediaUserDataForUser(t.Context(), 1, episode11.ID, medialibrary.UserData{Played: true}); err != nil {
		t.Fatal(err)
	}

	bob, err := store.CreateUser(t.Context(), domain.LocalUser{OpenListUserID: 101, InstanceID: "test-instance",
		Username: "bob", Role: domain.LocalUserRoleMedia, Enabled: true,
		LibraryGrants: []domain.LibraryGrant{{LibraryID: library.ID, Access: domain.LibraryAccessView}},
	})
	if err != nil {
		t.Fatal(err)
	}

	type nextUpItem struct {
		ID                       string            `json:"Id"`
		Type                     string            `json:"Type"`
		IndexNumber              int               `json:"IndexNumber"`
		ParentIndexNumber        int               `json:"ParentIndexNumber"`
		SeriesID                 string            `json:"SeriesId"`
		SeriesName               string            `json:"SeriesName"`
		SeasonID                 string            `json:"SeasonId"`
		SeasonName               string            `json:"SeasonName"`
		SeriesPrimaryImageTag    string            `json:"SeriesPrimaryImageTag"`
		ImageTags                map[string]string `json:"ImageTags"`
		PrimaryImageItemID       string            `json:"PrimaryImageItemId"`
		PrimaryImageTag          string            `json:"PrimaryImageTag"`
		ParentPrimaryImageItemID string            `json:"ParentPrimaryImageItemId"`
		ParentPrimaryImageTag    string            `json:"ParentPrimaryImageTag"`
	}
	type nextUpResponse struct {
		Items            []nextUpItem `json:"Items"`
		TotalRecordCount int          `json:"TotalRecordCount"`
		StartIndex       int          `json:"StartIndex"`
	}
	requestNextUp := func(token string, seriesID int64) (*httptest.ResponseRecorder, nextUpResponse) {
		t.Helper()
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet,
			"/emby/Shows/NextUp?SeriesId="+strconv.FormatInt(seriesID, 10)+"&Limit=1&api_key="+token, nil))
		var response nextUpResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode NextUp response: %v body = %s", err, recorder.Body.String())
		}
		return recorder, response
	}

	aliceAuth := authenticateEmby(t, server)
	aliceRecorder, aliceNextUp := requestNextUp(aliceAuth.AccessToken, series.ID)
	if aliceRecorder.Code != http.StatusOK || aliceNextUp.TotalRecordCount != 1 || aliceNextUp.StartIndex != 0 || len(aliceNextUp.Items) != 1 {
		t.Fatalf("Alice NextUp status = %d response = %#v body = %s", aliceRecorder.Code, aliceNextUp, aliceRecorder.Body.String())
	}
	item := aliceNextUp.Items[0]
	if item.ID != strconv.FormatInt(episode12.ID, 10) || item.Type != "Episode" || item.ParentIndexNumber != 1 || item.IndexNumber != 2 ||
		item.SeriesID != strconv.FormatInt(series.ID, 10) || item.SeriesName != series.Name ||
		item.SeasonID != strconv.FormatInt(season1.ID, 10) || item.SeasonName != season1.Name ||
		item.SeriesPrimaryImageTag != cover.SHA256 || item.ImageTags["Primary"] != cover.SHA256 ||
		item.PrimaryImageItemID != strconv.FormatInt(series.ID, 10) || item.PrimaryImageTag != cover.SHA256 ||
		item.ParentPrimaryImageItemID != strconv.FormatInt(series.ID, 10) || item.ParentPrimaryImageTag != cover.SHA256 {
		t.Fatalf("Alice NextUp item = %#v body = %s", item, aliceRecorder.Body.String())
	}
	globalRecorder := httptest.NewRecorder()
	server.ServeHTTP(globalRecorder, httptest.NewRequest(http.MethodGet,
		"/emby/Shows/NextUp?UserId=1&Limit=24&api_key="+aliceAuth.AccessToken, nil))
	var globalNextUp nextUpResponse
	if err := json.Unmarshal(globalRecorder.Body.Bytes(), &globalNextUp); err != nil {
		t.Fatalf("decode global NextUp response: %v body = %s", err, globalRecorder.Body.String())
	}
	if globalRecorder.Code != http.StatusOK || globalNextUp.TotalRecordCount != 1 || len(globalNextUp.Items) != 1 || globalNextUp.Items[0].ID != strconv.FormatInt(episode12.ID, 10) {
		t.Fatalf("global NextUp status = %d response = %#v body = %s", globalRecorder.Code, globalNextUp, globalRecorder.Body.String())
	}
	continueWatching, err := media.ListContinueWatchingForUser(t.Context(), medialibrary.UserAccess{UserID: 1, IsAdmin: true}, 0, 10)
	if err != nil || len(continueWatching.Items) != 1 || continueWatching.Items[0].Source != medialibrary.ContinueWatchingNextUp || continueWatching.Items[0].Item.ID != episode12.ID {
		t.Fatalf("Manager continue watching = %#v, %v", continueWatching, err)
	}
	libraryViewID := strconv.FormatInt(8_000_000_000_000_000_000+library.ID, 10)
	libraryRecorder := httptest.NewRecorder()
	server.ServeHTTP(libraryRecorder, httptest.NewRequest(http.MethodGet,
		"/emby/Shows/NextUp?UserId=1&ParentId="+libraryViewID+"&api_key="+aliceAuth.AccessToken, nil))
	var libraryNextUp nextUpResponse
	if err := json.Unmarshal(libraryRecorder.Body.Bytes(), &libraryNextUp); err != nil {
		t.Fatalf("decode library NextUp response: %v body = %s", err, libraryRecorder.Body.String())
	}
	if libraryRecorder.Code != http.StatusOK || libraryNextUp.TotalRecordCount != 1 || len(libraryNextUp.Items) != 1 || libraryNextUp.Items[0].ID != strconv.FormatInt(continueWatching.Items[0].Item.ID, 10) {
		t.Fatalf("library NextUp status = %d response = %#v body = %s", libraryRecorder.Code, libraryNextUp, libraryRecorder.Body.String())
	}
	parentRecorder := httptest.NewRecorder()
	server.ServeHTTP(parentRecorder, httptest.NewRequest(http.MethodGet,
		"/emby/Shows/NextUp?UserId=1&ParentId="+strconv.FormatInt(season1.ID, 10)+"&EnableTotalRecordCount=false&api_key="+aliceAuth.AccessToken, nil))
	var parentNextUp nextUpResponse
	if err := json.Unmarshal(parentRecorder.Body.Bytes(), &parentNextUp); err != nil {
		t.Fatal(err)
	}
	if parentRecorder.Code != http.StatusOK || parentNextUp.TotalRecordCount != 0 || len(parentNextUp.Items) != 1 || parentNextUp.Items[0].ID != strconv.FormatInt(episode12.ID, 10) {
		t.Fatalf("parent next up status = %d response = %#v body = %s", parentRecorder.Code, parentNextUp, parentRecorder.Body.String())
	}
	cutoffRecorder := httptest.NewRecorder()
	server.ServeHTTP(cutoffRecorder, httptest.NewRequest(http.MethodGet,
		"/emby/Shows/NextUp?SeriesId="+strconv.FormatInt(series.ID, 10)+"&NextUpDateCutoff=2026-08-16T00:00:00Z&api_key="+aliceAuth.AccessToken, nil))
	var cutoffNextUp nextUpResponse
	if err := json.Unmarshal(cutoffRecorder.Body.Bytes(), &cutoffNextUp); err != nil {
		t.Fatal(err)
	}
	if cutoffRecorder.Code != http.StatusOK || cutoffNextUp.TotalRecordCount != 0 || len(cutoffNextUp.Items) != 0 {
		t.Fatalf("cutoff next up status = %d response = %#v", cutoffRecorder.Code, cutoffNextUp)
	}
	resumeAt := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	if _, err := store.UpdateMediaUserDataForUser(t.Context(), 1, episode12.ID, medialibrary.UserData{PlaybackPositionTicks: 100, LastPlayedAt: &resumeAt}); err != nil {
		t.Fatal(err)
	}
	resumableRecorder := httptest.NewRecorder()
	server.ServeHTTP(resumableRecorder, httptest.NewRequest(http.MethodGet,
		"/emby/Shows/NextUp?SeriesId="+strconv.FormatInt(series.ID, 10)+"&EnableResumable=false&api_key="+aliceAuth.AccessToken, nil))
	var resumableNextUp nextUpResponse
	if err := json.Unmarshal(resumableRecorder.Body.Bytes(), &resumableNextUp); err != nil {
		t.Fatal(err)
	}
	if resumableRecorder.Code != http.StatusOK || resumableNextUp.TotalRecordCount != 0 || len(resumableNextUp.Items) != 0 {
		t.Fatalf("resumable-disabled next up status = %d response = %#v", resumableRecorder.Code, resumableNextUp)
	}
	wrongUserRecorder := httptest.NewRecorder()
	server.ServeHTTP(wrongUserRecorder, httptest.NewRequest(http.MethodGet,
		"/emby/Shows/NextUp?UserId=2&SeriesId="+strconv.FormatInt(series.ID, 10)+"&api_key="+aliceAuth.AccessToken, nil))
	if wrongUserRecorder.Code != http.StatusNotFound {
		t.Fatalf("wrong-user NextUp status = %d body = %s", wrongUserRecorder.Code, wrongUserRecorder.Body.String())
	}

	bobAuth := authenticateEmbyAs(t, server, bob.Username, "bob-password", "bob-next-up")
	bobRecorder, bobNextUp := requestNextUp(bobAuth.AccessToken, series.ID)
	if bobRecorder.Code != http.StatusOK || len(bobNextUp.Items) != 0 || bobNextUp.TotalRecordCount != 0 {
		t.Fatalf("Bob NextUp status = %d response = %#v body = %s", bobRecorder.Code, bobNextUp, bobRecorder.Body.String())
	}
	bobGlobalRecorder := httptest.NewRecorder()
	server.ServeHTTP(bobGlobalRecorder, httptest.NewRequest(http.MethodGet,
		"/emby/Shows/NextUp?UserId="+strconv.FormatInt(bob.ID, 10)+"&api_key="+bobAuth.AccessToken, nil))
	var bobGlobalNextUp nextUpResponse
	if err := json.Unmarshal(bobGlobalRecorder.Body.Bytes(), &bobGlobalNextUp); err != nil {
		t.Fatalf("decode Bob global NextUp response: %v body = %s", err, bobGlobalRecorder.Body.String())
	}
	if bobGlobalRecorder.Code != http.StatusOK || bobGlobalNextUp.TotalRecordCount != 0 || len(bobGlobalNextUp.Items) != 0 {
		t.Fatalf("Bob global NextUp status = %d response = %#v body = %s", bobGlobalRecorder.Code, bobGlobalNextUp, bobGlobalRecorder.Body.String())
	}
	deniedLibraryViewID := strconv.FormatInt(8_000_000_000_000_000_000+inaccessibleMovie.LibraryID, 10)
	deniedLibraryRecorder := httptest.NewRecorder()
	server.ServeHTTP(deniedLibraryRecorder, httptest.NewRequest(http.MethodGet,
		"/emby/Shows/NextUp?ParentId="+deniedLibraryViewID+"&api_key="+bobAuth.AccessToken, nil))
	if deniedLibraryRecorder.Code != http.StatusNotFound {
		t.Fatalf("unauthorized library NextUp status = %d body = %s", deniedLibraryRecorder.Code, deniedLibraryRecorder.Body.String())
	}
	deniedRecorder, _ := requestNextUp(bobAuth.AccessToken, inaccessibleMovie.ID)
	if deniedRecorder.Code != http.StatusNotFound {
		t.Fatalf("unauthorized NextUp status = %d body = %s", deniedRecorder.Code, deniedRecorder.Body.String())
	}

	for _, episode := range []medialibrary.Item{episode12, episode21} {
		if _, err := store.UpdateMediaUserDataForUser(t.Context(), 1, episode.ID, medialibrary.UserData{Played: true}); err != nil {
			t.Fatal(err)
		}
	}
	emptyRecorder, emptyNextUp := requestNextUp(aliceAuth.AccessToken, series.ID)
	if emptyRecorder.Code != http.StatusOK || emptyNextUp.TotalRecordCount != 0 || emptyNextUp.StartIndex != 0 || len(emptyNextUp.Items) != 0 {
		t.Fatalf("empty NextUp status = %d response = %#v body = %s", emptyRecorder.Code, emptyNextUp, emptyRecorder.Body.String())
	}
	rewatchAt := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	if _, err := store.UpdateMediaUserDataForUser(t.Context(), 1, episode11.ID, medialibrary.UserData{Played: true, PlayCount: 2, LastPlayedAt: &rewatchAt}); err != nil {
		t.Fatal(err)
	}
	rewatchRecorder := httptest.NewRecorder()
	server.ServeHTTP(rewatchRecorder, httptest.NewRequest(http.MethodGet,
		"/emby/Shows/NextUp?SeriesId="+strconv.FormatInt(series.ID, 10)+"&EnableRewatching=true&api_key="+aliceAuth.AccessToken, nil))
	var rewatchNextUp nextUpResponse
	if err := json.Unmarshal(rewatchRecorder.Body.Bytes(), &rewatchNextUp); err != nil {
		t.Fatal(err)
	}
	if rewatchRecorder.Code != http.StatusOK || rewatchNextUp.TotalRecordCount != 1 || len(rewatchNextUp.Items) != 1 || rewatchNextUp.Items[0].ID != strconv.FormatInt(episode12.ID, 10) {
		t.Fatalf("rewatch next up status = %d response = %#v body = %s", rewatchRecorder.Code, rewatchNextUp, rewatchRecorder.Body.String())
	}
}
