package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
	"github.com/sevxn007/openlist2emby/backend/internal/repository/sqlite"
	"github.com/sevxn007/openlist2emby/backend/internal/task"
	"github.com/sevxn007/openlist2emby/backend/internal/testidentity"
)

func TestLibraryEditorCannotUploadMediaLibraryItemImage(t *testing.T) {
	server, store, _, item := newAuthenticatedMediaLibraryServer(t)
	editor, err := store.CreateUser(t.Context(), domain.LocalUser{OpenListUserID: 101, InstanceID: "test-instance",
		Username: "editor", Role: domain.LocalUserRoleMedia, Enabled: true,
		LibraryGrants: []domain.LibraryGrant{{LibraryID: item.LibraryID, Access: domain.LibraryAccessEdit}},
	})
	if err != nil {
		t.Fatal(err)
	}
	editorSession, err := testidentity.Session(t.Context(), store, editor.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: sessionCookieName, Value: editorSession.Token}
	assets, err := medialibrary.NewAssetFiles(filepath.Join(t.TempDir(), "media"))
	if err != nil {
		t.Fatal(err)
	}
	server.mediaLibraries.SetAssetFiles(assets)

	var imageData bytes.Buffer
	if err := jpeg.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 32, 48)), nil); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", "poster.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(imageData.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPut,
		"/api/manager/media-library/items/"+strconv.FormatInt(item.ID, 10)+"/images/primary", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("upload status = %d", response.Code)
	}
	if _, err := store.GetMediaItemImage(t.Context(), item.ID, medialibrary.ImagePrimary); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unexpected stored image: %v", err)
	}

}

func newAuthenticatedMediaLibraryServer(t *testing.T) (*Server, *sqlite.Store, *http.Cookie, medialibrary.Item) {
	t.Helper()
	server, store := newTestServer(t)
	if err := store.SaveInitialSettings(t.Context(), "http://openlist.local", "admin-key"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetMediaLibraryConfiguration(t.Context(), true, domain.DefaultEmbyPort); err != nil {
		t.Fatal(err)
	}
	session, err := createTestSession(t, store, t.Context(), "manager", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	media := medialibrary.NewService(store, func(string, string) medialibrary.OpenList { return nil }, nil)
	library, err := media.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Movies", ContentType: medialibrary.ContentMovies, RootPath: "/Movies",
	})
	if err != nil {
		t.Fatal(err)
	}
	generation, err := store.BeginLibraryScan(t.Context(), library.ID)
	if err != nil {
		t.Fatal(err)
	}
	item, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, Kind: medialibrary.KindMovie, Path: "/Movies/Dune.mp4",
		MediaPath: "/Movies/Dune.mp4", Name: "Dune", SortName: "Dune", Container: "mp4",
		RuntimeTicks: 10_000_000, Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	scheduleInput, err := newMediaScanTaskScheduleInput(library.ID, library.Name, defaultMediaScanCron, defaultMediaScanTimezone, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.tasks.CreateSchedule(t.Context(), "manager", scheduleInput); err != nil {
		t.Fatal(err)
	}
	server.mediaLibraries = media
	server.api = server.routes()
	return server, store, &http.Cookie{Name: sessionCookieName, Value: session.Token}, item
}

type fakeMediaMetadataProvider struct {
	detailFn  func(context.Context, string, string, int64, *int, string) (domain.MediaDetail, error)
	creditsFn func(context.Context, string, domain.MediaCreditsQuery, string) ([]domain.MediaPerson, error)
}

func (f *fakeMediaMetadataProvider) Search(context.Context, string, string, string) ([]domain.MediaSearchResult, error) {
	return nil, nil
}

func (f *fakeMediaMetadataProvider) Detail(ctx context.Context, apiKey, mediaType string, id int64, season *int, language string) (domain.MediaDetail, error) {
	if f.detailFn == nil {
		return domain.MediaDetail{}, nil
	}
	return f.detailFn(ctx, apiKey, mediaType, id, season, language)
}

func (f *fakeMediaMetadataProvider) Credits(ctx context.Context, apiKey string, query domain.MediaCreditsQuery, language string) ([]domain.MediaPerson, error) {
	if f.creditsFn == nil {
		return []domain.MediaPerson{}, nil
	}
	return f.creditsFn(ctx, apiKey, query, language)
}

func updatePlaybackRequest(itemID int64, cookie *http.Cookie, position int64, stopped bool) *http.Request {
	request := requestJSON(http.MethodPut,
		"/api/manager/media-library/items/"+strconv.FormatInt(itemID, 10)+"/playback",
		playbackUpdateRequest{PositionTicks: position, Stopped: stopped},
	)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	return request
}

func decodePlaybackUserData(t *testing.T, recorder *httptest.ResponseRecorder) medialibrary.UserData {
	t.Helper()
	var response struct {
		Data medialibrary.UserData `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v body = %s", err, recorder.Body.String())
	}
	return response.Data
}

func updateMetadataRequest(itemID int64, cookie *http.Cookie, input medialibrary.MetadataInput) *http.Request {
	request := requestJSON(http.MethodPut,
		"/api/manager/media-library/items/"+strconv.FormatInt(itemID, 10)+"/metadata",
		input,
	)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	return request
}

func refreshMetadataRequest(itemID int64, cookie *http.Cookie, tmdbID int64) *http.Request {
	request := requestJSON(http.MethodPost,
		"/api/manager/media-library/items/"+strconv.FormatInt(itemID, 10)+"/metadata/refresh",
		metadataRefreshRequest{TMDBID: &tmdbID},
	)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	return request
}

func decodeMediaItem(t *testing.T, recorder *httptest.ResponseRecorder) medialibrary.Item {
	t.Helper()
	var response struct {
		Data medialibrary.Item `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v body = %s", err, recorder.Body.String())
	}
	return response.Data
}

func TestMediaLibraryCRUDSupportsMultipleRootPaths(t *testing.T) {
	server, _, cookie, _ := newAuthenticatedMediaLibraryServer(t)
	create := requestJSON(http.MethodPost, "/api/manager/media-libraries", medialibrary.LibraryInput{
		Name: "Shows", ContentType: medialibrary.ContentTVShows, RootPaths: []string{"/Shows", "/Archive/Shows"},
	})
	create.AddCookie(cookie)
	created := httptest.NewRecorder()
	server.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d body = %s", created.Code, created.Body.String())
	}
	var response struct {
		Data medialibrary.Library `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.RootPath != "/Shows" || len(response.Data.RootPaths) != 2 || response.Data.RootPaths[1] != "/Archive/Shows" || response.Data.HidePeopleWithoutImages {
		t.Fatalf("created library = %#v", response.Data)
	}

	hidePeople := true
	update := requestJSON(http.MethodPut, "/api/manager/media-libraries/"+strconv.FormatInt(response.Data.ID, 10), medialibrary.LibraryInput{
		Name: "Shows", ContentType: medialibrary.ContentTVShows, RootPaths: []string{"/Shows/New", "/Archive/Shows"},
		HidePeopleWithoutImages: &hidePeople,
	})
	update.AddCookie(cookie)
	updated := httptest.NewRecorder()
	server.ServeHTTP(updated, update)
	if updated.Code != http.StatusOK {
		t.Fatalf("update status = %d body = %s", updated.Code, updated.Body.String())
	}
	if err := json.Unmarshal(updated.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.RootPath != "/Shows/New" || len(response.Data.RootPaths) != 2 || response.Data.RootPaths[0] != "/Shows/New" || !response.Data.HidePeopleWithoutImages {
		t.Fatalf("updated library = %#v", response.Data)
	}
}

func TestMediaLibraryScanScheduleLifecycle(t *testing.T) {
	server, store, cookie, _ := newAuthenticatedMediaLibraryServer(t)
	create := requestJSON(http.MethodPost, "/api/manager/media-libraries", medialibrary.LibraryInput{
		Name: "Shows", ContentType: medialibrary.ContentTVShows, RootPaths: []string{"/Shows"},
	})
	create.AddCookie(cookie)
	created := httptest.NewRecorder()
	server.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d body = %s", created.Code, created.Body.String())
	}
	var libraryResponse struct {
		Data medialibrary.Library `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &libraryResponse); err != nil {
		t.Fatal(err)
	}
	libraryID := libraryResponse.Data.ID

	get := httptest.NewRequest(http.MethodGet, "/api/manager/media-libraries/"+strconv.FormatInt(libraryID, 10)+"/scan-schedule", nil)
	get.AddCookie(cookie)
	got := httptest.NewRecorder()
	server.ServeHTTP(got, get)
	if got.Code != http.StatusOK {
		t.Fatalf("get schedule status = %d body = %s", got.Code, got.Body.String())
	}
	var scheduleResponse struct {
		Data task.PublicSchedule `json:"data"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &scheduleResponse); err != nil {
		t.Fatal(err)
	}
	schedule := scheduleResponse.Data
	if schedule.CronExpression != defaultMediaScanCron || schedule.Timezone != defaultMediaScanTimezone || !schedule.Enabled || schedule.NextRunAt == nil {
		t.Fatalf("default schedule = %#v", schedule)
	}
	scheduleID := schedule.ID
	queued, err := store.CreateRun(t.Context(), task.NewRun{
		ScheduleID: &scheduleID, Type: medialibrary.ScanTaskType, PayloadVersion: 1,
		Payload: json.RawMessage(`{"library_id":1}`), Trigger: task.TriggerSchedule,
		AvailableAt: time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	update := requestJSON(http.MethodPut, "/api/manager/media-libraries/"+strconv.FormatInt(libraryID, 10)+"/scan-schedule", mediaLibraryScanScheduleInput{
		CronExpression: "30 2 * * *", Timezone: "Asia/Shanghai", Enabled: false,
	})
	update.AddCookie(cookie)
	updated := httptest.NewRecorder()
	server.ServeHTTP(updated, update)
	if updated.Code != http.StatusOK {
		t.Fatalf("update schedule status = %d body = %s", updated.Code, updated.Body.String())
	}
	scheduleResponse = struct {
		Data task.PublicSchedule `json:"data"`
	}{}
	if err := json.Unmarshal(updated.Body.Bytes(), &scheduleResponse); err != nil {
		t.Fatal(err)
	}
	if scheduleResponse.Data.Enabled || scheduleResponse.Data.CronExpression != "30 2 * * *" || scheduleResponse.Data.Timezone != "Asia/Shanghai" || scheduleResponse.Data.NextRunAt != nil {
		t.Fatalf("updated schedule = %#v", scheduleResponse.Data)
	}

	rename := requestJSON(http.MethodPut, "/api/manager/media-libraries/"+strconv.FormatInt(libraryID, 10), medialibrary.LibraryInput{
		Name: "Television", ContentType: medialibrary.ContentTVShows, RootPaths: []string{"/Shows"},
	})
	rename.AddCookie(cookie)
	renamed := httptest.NewRecorder()
	server.ServeHTTP(renamed, rename)
	if renamed.Code != http.StatusOK {
		t.Fatalf("rename status = %d body = %s", renamed.Code, renamed.Body.String())
	}
	scheduleAfterRename, err := server.tasks.GetSchedule(t.Context(), schedule.ID)
	if err != nil || scheduleAfterRename.Name != "Television" {
		t.Fatalf("renamed schedule = %#v error = %v", scheduleAfterRename, err)
	}

	remove := httptest.NewRequest(http.MethodDelete, "/api/manager/media-libraries/"+strconv.FormatInt(libraryID, 10), nil)
	remove.AddCookie(cookie)
	removed := httptest.NewRecorder()
	server.ServeHTTP(removed, remove)
	if removed.Code != http.StatusOK {
		t.Fatalf("delete status = %d body = %s", removed.Code, removed.Body.String())
	}
	if _, err := server.tasks.GetSchedule(t.Context(), schedule.ID); !errors.Is(err, task.ErrNotFound) {
		t.Fatalf("deleted schedule error = %v", err)
	}
	canceled, err := store.GetRun(t.Context(), queued.ID)
	if err != nil {
		t.Fatal(err)
	}
	if canceled.Status != task.StatusCanceled {
		t.Fatalf("queued schedule run status = %q, want %q", canceled.Status, task.StatusCanceled)
	}
}

func TestMediaLibraryPlaybackCompletionPercentControlsPlayedState(t *testing.T) {
	server, store, cookie, item := newAuthenticatedMediaLibraryServer(t)
	library, err := store.GetLibrary(t.Context(), item.LibraryID)
	if err != nil {
		t.Fatal(err)
	}
	if library.PlaybackCompletionPercent != medialibrary.DefaultPlaybackCompletionPercent {
		t.Fatalf("default completion threshold = %d", library.PlaybackCompletionPercent)
	}
	completionPercent := 95
	update := requestJSON(http.MethodPut, "/api/manager/media-libraries/"+strconv.FormatInt(library.ID, 10), medialibrary.LibraryInput{
		Name: library.Name, ContentType: library.ContentType, RootPaths: library.RootPaths,
		PlaybackCompletionPercent: &completionPercent,
	})
	update.AddCookie(cookie)
	updated := httptest.NewRecorder()
	server.ServeHTTP(updated, update)
	if updated.Code != http.StatusOK {
		t.Fatalf("update status = %d body = %s", updated.Code, updated.Body.String())
	}
	var libraryResponse struct {
		Data medialibrary.Library `json:"data"`
	}
	if err := json.Unmarshal(updated.Body.Bytes(), &libraryResponse); err != nil {
		t.Fatal(err)
	}
	if libraryResponse.Data.PlaybackCompletionPercent != completionPercent {
		t.Fatalf("updated completion threshold = %#v", libraryResponse.Data)
	}

	belowThreshold := httptest.NewRecorder()
	server.ServeHTTP(belowThreshold, updatePlaybackRequest(item.ID, cookie, 9_499_999, true))
	if belowThreshold.Code != http.StatusOK {
		t.Fatalf("below threshold playback status = %d body = %s", belowThreshold.Code, belowThreshold.Body.String())
	}
	if data := decodePlaybackUserData(t, belowThreshold); data.Played || data.PlaybackPositionTicks != 9_499_999 {
		t.Fatalf("below threshold playback = %#v", data)
	}

	completed := httptest.NewRecorder()
	server.ServeHTTP(completed, updatePlaybackRequest(item.ID, cookie, 9_500_000, true))
	if completed.Code != http.StatusOK {
		t.Fatalf("completion playback status = %d body = %s", completed.Code, completed.Body.String())
	}
	if data := decodePlaybackUserData(t, completed); !data.Played || data.PlaybackPositionTicks != 0 || data.PlayCount != 1 {
		t.Fatalf("completion playback = %#v", data)
	}
}

func TestMediaLibraryOrderRequiresAdminAndPersists(t *testing.T) {
	server, store, cookie, _ := newAuthenticatedMediaLibraryServer(t)
	libraries, err := store.ListLibraries(t.Context())
	if err != nil || len(libraries) != 1 {
		t.Fatalf("initial libraries = %#v error = %v", libraries, err)
	}
	second, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Shows", ContentType: medialibrary.ContentTVShows, RootPath: "/Shows",
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := mediaLibraryOrderRequest{LibraryIDs: []int64{second.ID, libraries[0].ID}}

	unauthorized := httptest.NewRecorder()
	server.ServeHTTP(unauthorized, requestJSON(http.MethodPut, "/api/manager/media-libraries/order", payload))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized order status = %d body = %s", unauthorized.Code, unauthorized.Body.String())
	}

	request := requestJSON(http.MethodPut, "/api/manager/media-libraries/order", payload)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("order status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	ordered, err := store.ListLibraries(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(ordered) != 2 || ordered[0].ID != second.ID || ordered[1].ID != libraries[0].ID {
		t.Fatalf("ordered libraries = %#v", ordered)
	}
}

func TestMediaLibraryPlaybackRequiresAuthenticationAndValidInput(t *testing.T) {
	server, _, cookie, item := newAuthenticatedMediaLibraryServer(t)

	unauthorized := httptest.NewRecorder()
	server.ServeHTTP(unauthorized, updatePlaybackRequest(item.ID, nil, 1, false))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d body = %s", unauthorized.Code, unauthorized.Body.String())
	}

	invalidID := httptest.NewRecorder()
	request := requestJSON(http.MethodPut, "/api/manager/media-library/items/nope/playback", playbackUpdateRequest{})
	request.AddCookie(cookie)
	server.ServeHTTP(invalidID, request)
	if invalidID.Code != http.StatusBadRequest {
		t.Fatalf("invalid ID status = %d body = %s", invalidID.Code, invalidID.Body.String())
	}

	negative := httptest.NewRecorder()
	server.ServeHTTP(negative, updatePlaybackRequest(item.ID, cookie, -1, false))
	if negative.Code != http.StatusBadRequest {
		t.Fatalf("negative position status = %d body = %s", negative.Code, negative.Body.String())
	}

	missing := httptest.NewRecorder()
	server.ServeHTTP(missing, updatePlaybackRequest(99999, cookie, 1, false))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing item status = %d body = %s", missing.Code, missing.Body.String())
	}
}

func TestMediaLibraryPeopleAndPersonFilterUseTMDBOnDemand(t *testing.T) {
	server, store, cookie, item := newAuthenticatedMediaLibraryServer(t)
	if err := store.UpdateSettings(t.Context(), "http://openlist.local", "admin-key", "tmdb-key"); err != nil {
		t.Fatal(err)
	}
	movieID := int64(101)
	seriesID := int64(202)
	seasonNumber := 1
	episodeNumber := 2
	if err := store.UpdateMediaItemMetadata(t.Context(), item.ID, medialibrary.MetadataInput{
		Name: item.Name, SortName: item.SortName, TMDBID: &movieID, Genres: []string{}, Artists: []string{},
	}); err != nil {
		t.Fatal(err)
	}
	generation, err := store.BeginLibraryScan(t.Context(), item.LibraryID)
	if err != nil {
		t.Fatal(err)
	}
	series, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: item.LibraryID, Kind: medialibrary.KindSeries, Path: "/Movies/Show", Name: "Show", SortName: "Show",
		TMDBID: &seriesID, Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	season, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: item.LibraryID, ParentID: &series.ID, Kind: medialibrary.KindSeason, Path: "/Movies/Show/Season 1", Name: "Season 1", SortName: "0001",
		SeasonNumber: &seasonNumber, ImageURL: "https://image.example/old-season.jpg", Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	episode, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: item.LibraryID, ParentID: &season.ID, Kind: medialibrary.KindEpisode, Path: "/Movies/Show/S01E02.mp4", MediaPath: "/Movies/Show/S01E02.mp4", Name: "Episode 2", SortName: "0002",
		SeasonNumber: &seasonNumber, EpisodeNumber: &episodeNumber, Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	called := map[string]bool{}
	provider := &fakeMediaMetadataProvider{
		creditsFn: func(_ context.Context, apiKey string, query domain.MediaCreditsQuery, language string) ([]domain.MediaPerson, error) {
			if apiKey != "tmdb-key" || language != "zh-CN" {
				t.Fatalf("credits query = %#v key=%q language=%q", query, apiKey, language)
			}
			switch {
			case query.MediaType == "movie" && query.TMDBID == movieID:
				called["movie"] = true
			case query.MediaType == "tv" && query.TMDBID == seriesID && query.SeasonNumber == nil:
				called["series"] = true
			case query.MediaType == "tv" && query.TMDBID == seriesID && query.SeasonNumber != nil && *query.SeasonNumber == seasonNumber && query.EpisodeNumber == nil:
				called["season"] = true
			case query.MediaType == "tv" && query.TMDBID == seriesID && query.SeasonNumber != nil && *query.SeasonNumber == seasonNumber && query.EpisodeNumber != nil && *query.EpisodeNumber == episodeNumber:
				called["episode"] = true
			default:
				t.Fatalf("unexpected credits query = %#v", query)
			}
			return []domain.MediaPerson{{ID: 99, Name: "Timothee Chalamet", Type: "Actor", Role: "Paul", SortOrder: 0}}, nil
		},
	}
	server.mediaLibraries = medialibrary.NewService(store, func(string, string) medialibrary.OpenList { return nil }, provider)
	server.api = server.routes()

	peopleRequest := httptest.NewRequest(http.MethodGet, "/api/manager/media-library/items/"+strconv.FormatInt(item.ID, 10)+"/people?language=en", nil)
	peopleRequest.AddCookie(cookie)
	peopleResponse := httptest.NewRecorder()
	server.ServeHTTP(peopleResponse, peopleRequest)
	var peopleBody struct {
		Data struct {
			Items []domain.MediaPerson `json:"items"`
		} `json:"data"`
	}
	if peopleResponse.Code != http.StatusOK || json.Unmarshal(peopleResponse.Body.Bytes(), &peopleBody) != nil || len(peopleBody.Data.Items) != 1 || peopleBody.Data.Items[0].ID != 99 {
		t.Fatalf("people response status = %d body = %s", peopleResponse.Code, peopleResponse.Body.String())
	}
	for _, mediaItem := range []medialibrary.Item{series, season, episode} {
		request := httptest.NewRequest(http.MethodGet, "/api/manager/media-library/items/"+strconv.FormatInt(mediaItem.ID, 10)+"/people?language=en", nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("people status for %s = %d body = %s", mediaItem.Kind, response.Code, response.Body.String())
		}
	}
	for _, key := range []string{"movie", "series", "season", "episode"} {
		if !called[key] {
			t.Fatalf("credits source %s was not requested", key)
		}
	}

	for _, local := range []medialibrary.Item{item, season} {
		if err := store.ReplaceMediaItemPeople(t.Context(), local.ID, []medialibrary.LocalPerson{{Person: domain.MediaPerson{ID: 99, Name: "Timothee Chalamet", Type: "Actor"}}}); err != nil {
			t.Fatal(err)
		}
	}
	provider.creditsFn = func(context.Context, string, domain.MediaCreditsQuery, string) ([]domain.MediaPerson, error) {
		t.Fatal("person works must not fetch remote credits")
		return nil, nil
	}
	if err := store.ReplaceMediaItemPeople(t.Context(), episode.ID, []medialibrary.LocalPerson{{Person: domain.MediaPerson{ID: 100, Name: "Guest", Type: "Actor"}}}); err != nil {
		t.Fatal(err)
	}
	guestRequest := httptest.NewRequest(http.MethodGet, "/api/manager/media-library/items?person_id=100&kinds=movie,series,episode", nil)
	guestRequest.AddCookie(cookie)
	guestResponse := httptest.NewRecorder()
	server.ServeHTTP(guestResponse, guestRequest)
	var guestBody struct {
		Data medialibrary.ItemPage `json:"data"`
	}
	if guestResponse.Code != http.StatusOK || json.Unmarshal(guestResponse.Body.Bytes(), &guestBody) != nil || guestBody.Data.Total != 1 || len(guestBody.Data.Items) != 1 {
		t.Fatalf("guest works: %d %s", guestResponse.Code, guestResponse.Body.String())
	}
	guestItem := guestBody.Data.Items[0]
	if guestItem.ID != episode.ID || guestItem.Kind != medialibrary.KindEpisode || guestItem.SeriesID == nil || *guestItem.SeriesID != series.ID || guestItem.SeriesName != series.Name || guestItem.Name != episode.Name {
		t.Fatalf("guest episode metadata: %#v", guestItem)
	}
	worksRequest := httptest.NewRequest(http.MethodGet, "/api/manager/media-library/items?person_id=99&kinds=movie,series", nil)
	worksRequest.AddCookie(cookie)
	worksResponse := httptest.NewRecorder()
	server.ServeHTTP(worksResponse, worksRequest)
	var worksBody struct {
		Data medialibrary.ItemPage `json:"data"`
	}
	if worksResponse.Code != http.StatusOK || json.Unmarshal(worksResponse.Body.Bytes(), &worksBody) != nil || len(worksBody.Data.Items) != 2 || worksBody.Data.Items[0].ID != item.ID || worksBody.Data.Items[1].ID != series.ID {
		t.Fatalf("person works response status = %d body = %s", worksResponse.Code, worksResponse.Body.String())
	}
}

func TestMediaLibraryPeopleVisibilitySettingFiltersMissingImages(t *testing.T) {
	server, store, cookie, item := newAuthenticatedMediaLibraryServer(t)
	if err := store.UpdateSettings(t.Context(), "http://openlist.local", "admin-key", "tmdb-key"); err != nil {
		t.Fatal(err)
	}
	tmdbID := int64(123)
	if err := store.UpdateMediaItemMetadata(t.Context(), item.ID, medialibrary.MetadataInput{
		Name: item.Name, SortName: item.SortName, TMDBID: &tmdbID, Genres: []string{}, Artists: []string{},
	}); err != nil {
		t.Fatal(err)
	}
	hide := true
	library, err := store.GetLibrary(t.Context(), item.LibraryID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateLibrary(t.Context(), library.ID, medialibrary.LibraryInput{
		Name: library.Name, ContentType: library.ContentType, RootPaths: library.RootPaths, HidePeopleWithoutImages: &hide,
	}); err != nil {
		t.Fatal(err)
	}
	server.mediaLibraries = medialibrary.NewService(store, func(string, string) medialibrary.OpenList { return nil }, &fakeMediaMetadataProvider{
		creditsFn: func(_ context.Context, _ string, _ domain.MediaCreditsQuery, _ string) ([]domain.MediaPerson, error) {
			return []domain.MediaPerson{
				{ID: 1, Name: "With image", ProfileURL: "https://image.example/person.jpg", Type: "Actor"},
				{ID: 2, Name: "Without image", Type: "Actor"},
			}, nil
		},
	})
	server.api = server.routes()
	request := httptest.NewRequest(http.MethodGet, "/api/manager/media-library/items/"+strconv.FormatInt(item.ID, 10)+"/people", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	var body struct {
		Data struct {
			Items []domain.MediaPerson `json:"items"`
		} `json:"data"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &body) != nil || len(body.Data.Items) != 1 || body.Data.Items[0].ID != 1 {
		t.Fatalf("filtered people response status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestMediaLibraryPlaybackPersistsProgressAndCompletion(t *testing.T) {
	server, store, cookie, item := newAuthenticatedMediaLibraryServer(t)
	favorite := medialibrary.UserData{Favorite: true, PlayCount: 2}
	if _, err := store.UpdateMediaUserData(t.Context(), item.ID, favorite); err != nil {
		t.Fatal(err)
	}

	progress := httptest.NewRecorder()
	server.ServeHTTP(progress, updatePlaybackRequest(item.ID, cookie, 4_000_000, false))
	if progress.Code != http.StatusOK {
		t.Fatalf("progress status = %d body = %s", progress.Code, progress.Body.String())
	}
	data := decodePlaybackUserData(t, progress)
	if data.PlaybackPositionTicks != 4_000_000 || data.Played || data.PlayCount != 2 || !data.Favorite || data.LastPlayedAt == nil {
		t.Fatalf("progress user data = %#v", data)
	}

	completed := httptest.NewRecorder()
	server.ServeHTTP(completed, updatePlaybackRequest(item.ID, cookie, 9_000_000, true))
	if completed.Code != http.StatusOK {
		t.Fatalf("completion status = %d body = %s", completed.Code, completed.Body.String())
	}
	data = decodePlaybackUserData(t, completed)
	if data.PlaybackPositionTicks != 0 || !data.Played || data.PlayCount != 3 || !data.Favorite {
		t.Fatalf("completion user data = %#v", data)
	}

	repeated := httptest.NewRecorder()
	server.ServeHTTP(repeated, updatePlaybackRequest(item.ID, cookie, 9_500_000, true))
	data = decodePlaybackUserData(t, repeated)
	if repeated.Code != http.StatusOK || data.PlayCount != 3 {
		t.Fatalf("repeated completion status = %d user data = %#v", repeated.Code, data)
	}
}

func TestMediaLibraryPlaybackDisappearsWhenDisabled(t *testing.T) {
	server, store, cookie, item := newAuthenticatedMediaLibraryServer(t)
	if err := store.SetMediaLibraryConfiguration(t.Context(), false, domain.DefaultEmbyPort); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, updatePlaybackRequest(item.ID, cookie, 1, false))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("disabled status = %d body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestMediaLibraryMetadataEditingRequiresAdminAndValidatesInput(t *testing.T) {
	server, store, cookie, item := newAuthenticatedMediaLibraryServer(t)
	year := 2021
	season := 1
	tmdbID := int64(438631)
	metadata := medialibrary.MetadataInput{
		Name: "Dune", SortName: "Dune (2021)", TMDBID: &tmdbID, IMDbID: "tt1160419",
		Genres: []string{"Science Fiction", "Adventure"}, ProductionYear: &year, PremiereDate: "2021-10-22",
		Overview: "A mythic and emotional hero's journey.", CommunityRating: 8.2, RuntimeTicks: 9_300_000_000,
		ImageURL: "https://image.example/dune.jpg", BackdropURL: "https://image.example/dune-backdrop.jpg",
		SeasonNumber: &season, Artists: []string{"Hans Zimmer"},
	}
	updated := httptest.NewRecorder()
	server.ServeHTTP(updated, updateMetadataRequest(item.ID, cookie, metadata))
	if updated.Code != http.StatusOK {
		t.Fatalf("metadata status = %d body = %s", updated.Code, updated.Body.String())
	}
	stored := decodeMediaItem(t, updated)
	if stored.Name != metadata.Name || stored.SortName != metadata.SortName || stored.TMDBID == nil || *stored.TMDBID != 438631 || stored.Overview != metadata.Overview || stored.ImageURL != metadata.ImageURL || len(stored.Genres) != 2 {
		t.Fatalf("updated metadata = %#v", stored)
	}

	invalid := httptest.NewRecorder()
	server.ServeHTTP(invalid, updateMetadataRequest(item.ID, cookie, medialibrary.MetadataInput{Name: "", Artists: []string{}}))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid metadata status = %d body = %s", invalid.Code, invalid.Body.String())
	}

	viewer, err := store.CreateUser(t.Context(), domain.LocalUser{OpenListUserID: 102, InstanceID: "test-instance",
		Username: "viewer", Role: domain.LocalUserRoleMedia, Enabled: true,
		LibraryGrants: []domain.LibraryGrant{{LibraryID: item.LibraryID, Access: domain.LibraryAccessView}},
	})
	if err != nil {
		t.Fatal(err)
	}
	viewerSession, err := testidentity.Session(t.Context(), store, viewer.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := httptest.NewRecorder()
	server.ServeHTTP(forbidden, updateMetadataRequest(item.ID, &http.Cookie{Name: sessionCookieName, Value: viewerSession.Token}, metadata))
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("media user metadata status = %d body = %s", forbidden.Code, forbidden.Body.String())
	}

	editor, err := store.CreateUser(t.Context(), domain.LocalUser{OpenListUserID: 103, InstanceID: "test-instance",
		Username: "editor", Role: domain.LocalUserRoleMedia, Enabled: true,
		LibraryGrants: []domain.LibraryGrant{{LibraryID: item.LibraryID, Access: domain.LibraryAccessEdit}},
	})
	if err != nil {
		t.Fatal(err)
	}
	editorSession, err := testidentity.Session(t.Context(), store, editor.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	edited := httptest.NewRecorder()
	server.ServeHTTP(edited, updateMetadataRequest(item.ID, &http.Cookie{Name: sessionCookieName, Value: editorSession.Token}, metadata))
	if edited.Code != http.StatusForbidden {
		t.Fatalf("editor metadata status = %d body = %s", edited.Code, edited.Body.String())
	}

	if err := store.SetMediaLibraryConfiguration(t.Context(), false, domain.DefaultEmbyPort); err != nil {
		t.Fatal(err)
	}
	disabled := httptest.NewRecorder()
	server.ServeHTTP(disabled, updateMetadataRequest(item.ID, cookie, metadata))
	if disabled.Code != http.StatusNotFound {
		t.Fatalf("disabled metadata status = %d body = %s", disabled.Code, disabled.Body.String())
	}
}

func TestMediaLibraryMetadataRefreshUsesTMDBIDAndReplacesPresentationMetadata(t *testing.T) {
	server, store, cookie, item := newAuthenticatedMediaLibraryServer(t)
	if err := store.UpdateSettings(t.Context(), "http://openlist.local", "admin-key", "tmdb-key"); err != nil {
		t.Fatal(err)
	}
	provider := &fakeMediaMetadataProvider{
		detailFn: func(_ context.Context, apiKey, mediaType string, id int64, season *int, language string) (domain.MediaDetail, error) {
			if apiKey != "tmdb-key" || mediaType != "movie" || id != 693134 || season != nil || language != "zh-CN" {
				t.Fatalf("TMDB request = key=%q type=%q id=%d season=%v language=%q", apiKey, mediaType, id, season, language)
			}
			return domain.MediaDetail{
				ID: 693134, MediaType: "movie", Title: "Dune: Part Two", IMDbID: "tt15239678",
				Overview: "Paul Atreides unites with Chani.", Genres: []string{"Science Fiction", "Adventure"},
				ReleaseDate: "2024-02-27", Runtime: 167, VoteAverage: 8.26,
				PosterURL: "https://image.example/dune-two.jpg", BackdropURL: "https://image.example/dune-two-backdrop.jpg",
			}, nil
		},
	}
	server.mediaLibraries = medialibrary.NewService(store, func(string, string) medialibrary.OpenList { return nil }, provider)
	server.api = server.routes()

	updated := httptest.NewRecorder()
	server.ServeHTTP(updated, refreshMetadataRequest(item.ID, cookie, 693134))
	if updated.Code != http.StatusOK {
		t.Fatalf("metadata refresh status = %d body = %s", updated.Code, updated.Body.String())
	}
	stored := decodeMediaItem(t, updated)
	if stored.TMDBID == nil || *stored.TMDBID != 693134 || stored.Name != "Dune: Part Two" || stored.SortName != "Dune: Part Two" || stored.IMDbID != "tt15239678" || stored.Overview != "Paul Atreides unites with Chani." || stored.ProductionYear == nil || *stored.ProductionYear != 2024 || stored.RuntimeTicks != 167*60*10_000_000 || stored.CommunityRating != 8.3 || len(stored.Genres) != 2 || stored.ImageURL != "https://image.example/dune-two.jpg" {
		t.Fatalf("refreshed metadata = %#v", stored)
	}

	invalid := httptest.NewRecorder()
	server.ServeHTTP(invalid, refreshMetadataRequest(item.ID, cookie, 0))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid refresh status = %d body = %s", invalid.Code, invalid.Body.String())
	}
}

func TestMediaLibraryMetadataRefreshUpdatesSeriesSeasonsAndEpisodes(t *testing.T) {
	server, store, cookie, baseItem := newAuthenticatedMediaLibraryServer(t)
	if err := store.UpdateSettings(t.Context(), "http://openlist.local", "admin-key", "tmdb-key"); err != nil {
		t.Fatal(err)
	}
	generation, err := store.BeginLibraryScan(t.Context(), baseItem.LibraryID)
	if err != nil {
		t.Fatal(err)
	}
	series, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: baseItem.LibraryID, Kind: medialibrary.KindSeries, Path: "/Shows/@series/Severance",
		Name: "Old series", SortName: "Old series", ImageURL: "https://image.example/old-series.jpg", BackdropURL: "https://image.example/old-backdrop.jpg", Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	seasonNumber := 1
	season, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: baseItem.LibraryID, ParentID: &series.ID, Kind: medialibrary.KindSeason,
		Path: "/Shows/@series/Severance/@season/1", Name: "Old season", SortName: "0001",
		SeasonNumber: &seasonNumber, Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	episodeNumber := 1
	episode, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: baseItem.LibraryID, ParentID: &season.ID, Kind: medialibrary.KindEpisode,
		Path: "/Shows/Severance/S01E01.mkv", MediaPath: "/Shows/Severance/S01E01.mkv",
		Name: "Old episode", SortName: "0001", SeasonNumber: &seasonNumber, EpisodeNumber: &episodeNumber,
		RuntimeTicks: 10, ImageURL: "https://image.example/series-backdrop.jpg", Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateMediaUserData(t.Context(), episode.ID, medialibrary.UserData{Favorite: true, PlayCount: 2}); err != nil {
		t.Fatal(err)
	}
	provider := &fakeMediaMetadataProvider{
		detailFn: func(_ context.Context, apiKey, mediaType string, id int64, season *int, language string) (domain.MediaDetail, error) {
			if apiKey != "tmdb-key" || mediaType != "tv" || id != 1399 || language != "zh-CN" {
				t.Fatalf("TV request = key=%q type=%q id=%d language=%q", apiKey, mediaType, id, language)
			}
			if season == nil {
				return domain.MediaDetail{ID: 1399, MediaType: "tv", Name: "Severance", Title: "Severance", Overview: "Updated series", PosterURL: "https://image.example/series.jpg", BackdropURL: "https://image.example/backdrop.jpg"}, nil
			}
			if *season != 1 {
				t.Fatalf("season = %d, want 1", *season)
			}
			return domain.MediaDetail{
				ID: 1401, MediaType: "tv", Name: "Season 1", Overview: "Updated season", FirstAirDate: "2022-02-18",
				PosterURL: "https://image.example/season.jpg", VoteAverage: 8.5,
				Episodes: []domain.MediaEpisode{{
					ID: 140101, EpisodeNumber: 1, SeasonNumber: 1, Name: "Updated episode", Overview: "Updated episode overview",
					AirDate: "2022-02-18", Runtime: 57, StillURL: "https://image.example/episode.jpg", VoteAverage: 8.6,
				}},
			}, nil
		},
	}
	server.mediaLibraries = medialibrary.NewService(store, func(string, string) medialibrary.OpenList { return nil }, provider)
	server.api = server.routes()

	updated := httptest.NewRecorder()
	server.ServeHTTP(updated, refreshMetadataRequest(series.ID, cookie, 1399))
	if updated.Code != http.StatusOK {
		t.Fatalf("series metadata refresh status = %d body = %s", updated.Code, updated.Body.String())
	}
	updatedSeries, err := store.GetMediaItem(t.Context(), series.ID)
	if err != nil {
		t.Fatal(err)
	}
	updatedSeason, err := store.GetMediaItem(t.Context(), season.ID)
	if err != nil {
		t.Fatal(err)
	}
	updatedEpisode, err := store.GetMediaItem(t.Context(), episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedSeries.Name != "Severance" || updatedSeries.Overview != "Updated series" || updatedSeries.ImageURL != "https://image.example/series.jpg" || updatedSeries.BackdropURL != "https://image.example/backdrop.jpg" {
		t.Fatalf("series metadata = %#v", updatedSeries)
	}
	if updatedSeason.TMDBID == nil || *updatedSeason.TMDBID != 1401 || updatedSeason.Name != "Season 1" || updatedSeason.Overview != "Updated season" || updatedSeason.ImageURL != "https://image.example/season.jpg" {
		t.Fatalf("season metadata = %#v", updatedSeason)
	}
	if updatedEpisode.TMDBID == nil || *updatedEpisode.TMDBID != 140101 || updatedEpisode.Name != "Updated episode" || updatedEpisode.Overview != "Updated episode overview" || updatedEpisode.RuntimeTicks != 57*60*10_000_000 || updatedEpisode.ImageURL != "https://image.example/episode.jpg" || !updatedEpisode.UserData.Favorite || updatedEpisode.UserData.PlayCount != 2 {
		t.Fatalf("episode metadata or user data = %#v", updatedEpisode)
	}
}

func TestMediaLibraryMetadataRefreshPersistsEpisodesBeforeLocalizingAssets(t *testing.T) {
	server, store, cookie, baseItem := newAuthenticatedMediaLibraryServer(t)
	if err := store.UpdateSettings(t.Context(), "http://openlist.local", "admin-key", "tmdb-key"); err != nil {
		t.Fatal(err)
	}
	generation, err := store.BeginLibraryScan(t.Context(), baseItem.LibraryID)
	if err != nil {
		t.Fatal(err)
	}
	series, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: baseItem.LibraryID, Kind: medialibrary.KindSeries, Path: "/Shows/@series/Example",
		Name: "Old series", SortName: "Old series", Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	seasonNumber := 1
	season, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: baseItem.LibraryID, ParentID: &series.ID, Kind: medialibrary.KindSeason,
		Path: "/Shows/@series/Example/@season/1", Name: "Old season", SortName: "0001",
		SeasonNumber: &seasonNumber, Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	episodeNumber := 1
	episode, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: baseItem.LibraryID, ParentID: &season.ID, Kind: medialibrary.KindEpisode,
		Path: "/Shows/Example/S01E01.mkv", MediaPath: "/Shows/Example/S01E01.mkv",
		Name: "Old episode", SortName: "0001", SeasonNumber: &seasonNumber, EpisodeNumber: &episodeNumber,
		Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	provider := &fakeMediaMetadataProvider{detailFn: func(_ context.Context, _, _ string, _ int64, season *int, _ string) (domain.MediaDetail, error) {
		if season == nil {
			return domain.MediaDetail{ID: 42, MediaType: "tv", Name: "Updated series"}, nil
		}
		return domain.MediaDetail{
			ID: 43, MediaType: "tv", Name: "Updated season", PosterURL: "http://127.0.0.1/season.jpg",
			Episodes: []domain.MediaEpisode{{ID: 44, SeasonNumber: 1, EpisodeNumber: 1, Name: "Updated episode"}},
		}, nil
	}}
	service := medialibrary.NewService(store, func(string, string) medialibrary.OpenList { return nil }, provider)
	assets, err := medialibrary.NewAssetFiles(filepath.Join(t.TempDir(), "media"))
	if err != nil {
		t.Fatal(err)
	}
	service.SetAssetFiles(assets)
	server.mediaLibraries = service
	server.api = server.routes()

	response := httptest.NewRecorder()
	server.ServeHTTP(response, refreshMetadataRequest(series.ID, cookie, 42))
	if response.Code == http.StatusOK {
		t.Fatalf("metadata refresh unexpectedly succeeded despite invalid season image")
	}
	updatedEpisode, err := store.GetMediaItem(t.Context(), episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedEpisode.TMDBID == nil || *updatedEpisode.TMDBID != 44 || updatedEpisode.Name != "Updated episode" {
		t.Fatalf("episode metadata = %#v", updatedEpisode)
	}
}

func TestMediaUserOnlySeesGrantedLibrariesAndNoSourcePaths(t *testing.T) {
	server, store, _, allowedItem := newAuthenticatedMediaLibraryServer(t)
	privateLibrary, err := server.mediaLibraries.CreateLibrary(t.Context(), medialibrary.LibraryInput{
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
		LibraryID: privateLibrary.ID, Kind: medialibrary.KindMovie, Path: "/Private/Secret.mp4",
		MediaPath: "/Private/Secret.mp4", Name: "Secret", SortName: "Secret", Container: "mp4",
		RuntimeTicks: 10_000_000, Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := store.CreateUser(t.Context(), domain.LocalUser{OpenListUserID: 104, InstanceID: "test-instance",
		Username: "viewer", Role: domain.LocalUserRoleMedia, Enabled: true,
		LibraryGrants: []domain.LibraryGrant{{LibraryID: allowedItem.LibraryID, Access: domain.LibraryAccessView}},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := testidentity.Session(t.Context(), store, viewer.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: sessionCookieName, Value: session.Token}

	libraries := httptest.NewRecorder()
	librariesRequest := httptest.NewRequest(http.MethodGet, "/api/manager/media-libraries", nil)
	librariesRequest.AddCookie(cookie)
	server.ServeHTTP(libraries, librariesRequest)
	if libraries.Code != http.StatusOK {
		t.Fatalf("library list status = %d body = %s", libraries.Code, libraries.Body.String())
	}
	var libraryResponse struct {
		Data struct {
			Items []medialibrary.Library `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(libraries.Body.Bytes(), &libraryResponse); err != nil {
		t.Fatal(err)
	}
	if len(libraryResponse.Data.Items) != 1 || libraryResponse.Data.Items[0].ID != allowedItem.LibraryID || libraryResponse.Data.Items[0].RootPath != "" || len(libraryResponse.Data.Items[0].RootPaths) != 0 {
		t.Fatalf("media library projection = %#v", libraryResponse.Data.Items)
	}
	allLibraries := httptest.NewRecorder()
	allLibrariesRequest := httptest.NewRequest(http.MethodGet, "/api/manager/media-libraries?include_hidden=true", nil)
	allLibrariesRequest.AddCookie(cookie)
	server.ServeHTTP(allLibraries, allLibrariesRequest)
	if allLibraries.Code != http.StatusForbidden {
		t.Fatalf("media user include hidden status = %d body = %s", allLibraries.Code, allLibraries.Body.String())
	}

	forbidden := httptest.NewRecorder()
	forbiddenRequest := httptest.NewRequest(http.MethodGet, "/api/manager/media-library/items/"+strconv.FormatInt(privateItem.ID, 10), nil)
	forbiddenRequest.AddCookie(cookie)
	server.ServeHTTP(forbidden, forbiddenRequest)
	if forbidden.Code != http.StatusNotFound {
		t.Fatalf("private item status = %d body = %s", forbidden.Code, forbidden.Body.String())
	}

	privatePeople := httptest.NewRecorder()
	privatePeopleRequest := httptest.NewRequest(http.MethodGet, "/api/manager/media-library/items/"+strconv.FormatInt(privateItem.ID, 10)+"/people", nil)
	privatePeopleRequest.AddCookie(cookie)
	server.ServeHTTP(privatePeople, privatePeopleRequest)
	if privatePeople.Code != http.StatusNotFound {
		t.Fatalf("private people status = %d body = %s", privatePeople.Code, privatePeople.Body.String())
	}

	allowed := httptest.NewRecorder()
	allowedRequest := httptest.NewRequest(http.MethodGet, "/api/manager/media-library/items/"+strconv.FormatInt(allowedItem.ID, 10), nil)
	allowedRequest.AddCookie(cookie)
	server.ServeHTTP(allowed, allowedRequest)
	if allowed.Code != http.StatusOK {
		t.Fatalf("allowed item status = %d body = %s", allowed.Code, allowed.Body.String())
	}
	var itemResponse struct {
		Data medialibrary.Item `json:"data"`
	}
	if err := json.Unmarshal(allowed.Body.Bytes(), &itemResponse); err != nil {
		t.Fatal(err)
	}
	if itemResponse.Data.Path != "" || itemResponse.Data.MediaPath != "" {
		t.Fatalf("media item source paths leaked: %#v", itemResponse.Data)
	}
}

func TestAdministratorLibrarySelectionFiltersBrowsingButKeepsDirectAccess(t *testing.T) {
	server, store, cookie, visibleItem := newAuthenticatedMediaLibraryServer(t)
	hiddenLibrary, err := server.mediaLibraries.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Hidden", ContentType: medialibrary.ContentMovies, RootPath: "/Hidden",
	})
	if err != nil {
		t.Fatal(err)
	}
	generation, err := store.BeginLibraryScan(t.Context(), hiddenLibrary.ID)
	if err != nil {
		t.Fatal(err)
	}
	hiddenItem, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: hiddenLibrary.ID, Kind: medialibrary.KindMovie, Path: "/Hidden/Secret.mp4",
		MediaPath: "/Hidden/Secret.mp4", Name: "Secret", SortName: "Secret", Container: "mp4",
		RuntimeTicks: 10_000_000, Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishLibraryScan(t.Context(), hiddenLibrary.ID, generation); err != nil {
		t.Fatal(err)
	}
	admin, err := store.GetLocalUserByUsername(t.Context(), "manager")
	if err != nil {
		t.Fatal(err)
	}
	admin.LibraryGrantsConfigured = true
	admin.LibraryGrants = []domain.LibraryGrant{{LibraryID: visibleItem.LibraryID, Access: domain.LibraryAccessEdit}}
	admin, err = store.UpdateUser(t.Context(), admin)
	if err != nil {
		t.Fatal(err)
	}
	for _, itemID := range []int64{visibleItem.ID, hiddenItem.ID} {
		if _, err := store.UpdateMediaUserDataForUser(t.Context(), admin.ID, itemID, medialibrary.UserData{PlaybackPositionTicks: 1_000}); err != nil {
			t.Fatal(err)
		}
	}

	list := func(path string) []medialibrary.Library {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d body = %s", path, response.Code, response.Body.String())
		}
		var envelope struct {
			Data struct {
				Items []medialibrary.Library `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope.Data.Items
	}
	visibleLibraries := list("/api/manager/media-libraries")
	if len(visibleLibraries) != 1 || visibleLibraries[0].ID != visibleItem.LibraryID {
		t.Fatalf("administrator visible libraries = %#v", visibleLibraries)
	}
	allLibraries := list("/api/manager/media-libraries?include_hidden=true")
	if len(allLibraries) != 2 {
		t.Fatalf("administrator all libraries = %#v", allLibraries)
	}

	itemsRequest := httptest.NewRequest(http.MethodGet, "/api/manager/media-library/items?limit=10", nil)
	itemsRequest.AddCookie(cookie)
	itemsResponse := httptest.NewRecorder()
	server.ServeHTTP(itemsResponse, itemsRequest)
	var itemsEnvelope struct {
		Data medialibrary.ItemPage `json:"data"`
	}
	if itemsResponse.Code != http.StatusOK || json.Unmarshal(itemsResponse.Body.Bytes(), &itemsEnvelope) != nil || len(itemsEnvelope.Data.Items) != 1 || itemsEnvelope.Data.Items[0].ID != visibleItem.ID {
		t.Fatalf("administrator aggregate items status = %d body = %s", itemsResponse.Code, itemsResponse.Body.String())
	}

	continueRequest := httptest.NewRequest(http.MethodGet, "/api/manager/media-library/continue-watching?limit=10", nil)
	continueRequest.AddCookie(cookie)
	continueResponse := httptest.NewRecorder()
	server.ServeHTTP(continueResponse, continueRequest)
	var continueEnvelope struct {
		Data medialibrary.ContinueWatchingPage `json:"data"`
	}
	if continueResponse.Code != http.StatusOK || json.Unmarshal(continueResponse.Body.Bytes(), &continueEnvelope) != nil || len(continueEnvelope.Data.Items) != 1 || continueEnvelope.Data.Items[0].Item.ID != visibleItem.ID {
		t.Fatalf("administrator continue watching status = %d body = %s", continueResponse.Code, continueResponse.Body.String())
	}

	directRequest := httptest.NewRequest(http.MethodGet, "/api/manager/media-library/items/"+strconv.FormatInt(hiddenItem.ID, 10), nil)
	directRequest.AddCookie(cookie)
	directResponse := httptest.NewRecorder()
	server.ServeHTTP(directResponse, directRequest)
	if directResponse.Code != http.StatusOK {
		t.Fatalf("administrator hidden item status = %d body = %s", directResponse.Code, directResponse.Body.String())
	}
}

func TestMediaLibraryResumeVisibilityPreservesProgress(t *testing.T) {
	server, store, cookie, item := newAuthenticatedMediaLibraryServer(t)
	original := medialibrary.UserData{PlaybackPositionTicks: 42, Favorite: true, PlayCount: 2}
	if _, err := store.UpdateMediaUserDataForUser(t.Context(), 1, item.ID, original); err != nil {
		t.Fatal(err)
	}
	endpoint := "/api/manager/media-library/items/" + strconv.FormatInt(item.ID, 10) + "/resume-visibility"
	for _, test := range []struct {
		body   string
		auth   bool
		status int
		hidden bool
	}{
		{`{"hidden":true}`, false, 401, false},
		{`{}`, true, 400, false},
		{`{"hidden":null}`, true, 400, false},
		{`{"hidden":true}`, true, 200, true},
		{`{"hidden":false}`, true, 200, false},
	} {
		request := httptest.NewRequest(http.MethodPut, endpoint, bytes.NewBufferString(test.body))
		if test.auth {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("%s status=%d body=%s", test.body, response.Code, response.Body.String())
		}
		stored, err := store.GetMediaItemForUser(t.Context(), 1, item.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.UserData.HideFromResume != test.hidden || stored.UserData.PlaybackPositionTicks != 42 || !stored.UserData.Favorite || stored.UserData.PlayCount != 2 {
			t.Fatalf("state=%#v", stored.UserData)
		}
	}
}
