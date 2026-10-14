package emby_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	glebarezsqlite "github.com/glebarez/sqlite"
	"github.com/sevxn007/openlist2emby/backend/internal/app"
	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/emby"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
	"github.com/sevxn007/openlist2emby/backend/internal/repository/sqlite"
	"github.com/sevxn007/openlist2emby/backend/internal/testidentity"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type fakeMediaOpenList struct {
	downloadURL    string
	userAgent      string
	resolveStarted chan struct{}
	resolveRelease chan struct{}
}

func (f *fakeMediaOpenList) ListDirectory(context.Context, string, bool) ([]medialibrary.RemoteEntry, error) {
	return []medialibrary.RemoteEntry{}, nil
}

func (f *fakeMediaOpenList) ResolveDownloadURL(_ context.Context, _, userAgent string) (string, error) {
	f.userAgent = userAgent
	if f.resolveStarted != nil {
		close(f.resolveStarted)
	}
	if f.resolveRelease != nil {
		<-f.resolveRelease
	}
	return f.downloadURL, nil
}

type fakeEmbyMetadataProvider struct {
	creditsFn              func(context.Context, string, domain.MediaCreditsQuery, string) ([]domain.MediaPerson, error)
	seasonEpisodeCreditsFn func(context.Context, string, domain.MediaCreditsQuery, string) (map[int][]domain.MediaPerson, error)

	profileFn func(context.Context, string, int64, string) (string, error)
}

func (f *fakeEmbyMetadataProvider) Search(context.Context, string, string, string) ([]domain.MediaSearchResult, error) {
	return []domain.MediaSearchResult{}, nil
}

func (f *fakeEmbyMetadataProvider) Detail(context.Context, string, string, int64, *int, string) (domain.MediaDetail, error) {
	return domain.MediaDetail{}, nil
}

func (f *fakeEmbyMetadataProvider) Credits(ctx context.Context, apiKey string, query domain.MediaCreditsQuery, language string) ([]domain.MediaPerson, error) {
	if f.creditsFn == nil {
		return []domain.MediaPerson{}, nil
	}
	return f.creditsFn(ctx, apiKey, query, language)
}

func (f *fakeEmbyMetadataProvider) SeasonEpisodeCredits(ctx context.Context, apiKey string, query domain.MediaCreditsQuery, language string) (map[int][]domain.MediaPerson, error) {
	if f.seasonEpisodeCreditsFn == nil {
		return map[int][]domain.MediaPerson{}, nil
	}
	return f.seasonEpisodeCreditsFn(ctx, apiKey, query, language)
}

func (f *fakeEmbyMetadataProvider) PersonProfile(ctx context.Context, apiKey string, personID int64, language string) (string, error) {
	if f.profileFn == nil {
		return "", nil
	}
	return f.profileFn(ctx, apiKey, personID, language)
}

func newEmbyTestServer(t *testing.T, configure ...func(*medialibrary.Service)) (*emby.Server, *sqlite.Store, *fakeMediaOpenList, medialibrary.Item, *fakeEmbyMetadataProvider) {
	t.Helper()
	return newEmbyTestServerWithClock(t, time.Now, configure...)
}

func newEmbyTestServerWithClock(t *testing.T, clock func() time.Time, configure ...func(*medialibrary.Service)) (*emby.Server, *sqlite.Store, *fakeMediaOpenList, medialibrary.Item, *fakeEmbyMetadataProvider) {
	t.Helper()
	db, err := gorm.Open(glebarezsqlite.Open(filepath.Join(t.TempDir(), "emby.db")), &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Silent),
		TranslateError: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	store := sqlite.NewStoreWithClock(db, clock)
	if err := store.InitializeSchema(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SaveIdentitySetup(t.Context(), "http://openlist.local", "admin-key", domain.OpenListIdentity{ID: 1, Username: "alice", Role: 2}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetMediaLibraryConfiguration(t.Context(), true, domain.DefaultEmbyPort); err != nil {
		t.Fatal(err)
	}
	remote := &fakeMediaOpenList{downloadURL: "https://download.example/Dune.mkv?sign=fresh"}
	metadata := &fakeEmbyMetadataProvider{}
	media := medialibrary.NewService(store, func(string, string) medialibrary.OpenList { return remote }, metadata)
	for _, apply := range configure {
		apply(media)
	}
	library, err := media.CreateLibrary(t.Context(), medialibrary.LibraryInput{Name: "Movies", ContentType: medialibrary.ContentMovies, RootPath: "/Movies"})
	if err != nil {
		t.Fatal(err)
	}
	generation, err := store.BeginLibraryScan(t.Context(), library.ID)
	if err != nil {
		t.Fatal(err)
	}
	item, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, Kind: medialibrary.KindMovie, Path: "/Movies/Dune.mkv",
		MediaPath: "/Movies/Dune.mkv", Name: "Dune", SortName: "Dune", Container: "mkv",
		SizeBytes: 1024, RuntimeTicks: 10_000_000, Genres: []string{"Science Fiction"},
		Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishLibraryScan(t.Context(), library.ID, generation); err != nil {
		t.Fatal(err)
	}
	application := app.NewService(store, nil, nil, nil, nil)
	application.SetIdentityClient(&testidentity.Client{Store: store})
	embyStore, err := emby.NewSQLiteStore(t.Context(), store.DB())
	if err != nil {
		t.Fatal(err)
	}
	server, err := emby.NewServer(media, embyStore, store, application)
	if err != nil {
		t.Fatal(err)
	}
	return server, store, remote, item, metadata
}

type embyAuthResult struct {
	AccessToken string `json:"AccessToken"`
	User        struct {
		Id     string `json:"Id"`
		Policy struct {
			IsAdministrator bool `json:"IsAdministrator"`
		} `json:"Policy"`
	} `json:"User"`
}

func authenticateEmby(t *testing.T, server *emby.Server) embyAuthResult {
	return authenticateEmbyAs(t, server, "alice", "password-123", "device-1")
}

func authenticateEmbyAs(t *testing.T, server *emby.Server, username, password, deviceID string) embyAuthResult {
	t.Helper()
	authBody, err := json.Marshal(map[string]string{"Username": username, "Pw": password})
	if err != nil {
		t.Fatal(err)
	}
	authRequest := httptest.NewRequest(http.MethodPost, "/emby/Users/AuthenticateByName", bytes.NewReader(authBody))
	authRequest.Header.Set("X-Emby-Authorization", `MediaBrowser Client="Test", Device="Phone", DeviceId="`+deviceID+`", Version="1.0"`)
	authRecorder := httptest.NewRecorder()
	server.ServeHTTP(authRecorder, authRequest)
	if authRecorder.Code != http.StatusOK {
		t.Fatalf("authenticate status = %d body = %s", authRecorder.Code, authRecorder.Body.String())
	}
	var result embyAuthResult
	if err := json.Unmarshal(authRecorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestEmbyAuthenticationBrowsePlaybackAndRedirect(t *testing.T) {
	server, _, remote, item, _ := newEmbyTestServer(t)

	public := httptest.NewRecorder()
	server.ServeHTTP(public, httptest.NewRequest(http.MethodGet, "/emby/System/Info/Public", nil))
	if public.Code != http.StatusOK || strings.Contains(public.Body.String(), `"code"`) || !strings.Contains(public.Body.String(), `"ProductName":"openlist2emby"`) {
		t.Fatalf("public response status = %d body = %s", public.Code, public.Body.String())
	}

	authResult := authenticateEmby(t, server)
	if authResult.AccessToken == "" || authResult.User.Id != "1" || !authResult.User.Policy.IsAdministrator {
		t.Fatalf("authentication result = %#v", authResult)
	}

	views := httptest.NewRecorder()
	server.ServeHTTP(views, httptest.NewRequest(http.MethodGet, "/emby/Users/1/Views?api_key="+authResult.AccessToken, nil))
	if views.Code != http.StatusOK || !strings.Contains(views.Body.String(), `"CollectionType":"movies"`) {
		t.Fatalf("views status = %d body = %s", views.Code, views.Body.String())
	}

	playback := httptest.NewRecorder()
	server.ServeHTTP(playback, httptest.NewRequest(http.MethodGet,
		"/emby/Items/"+strconv.FormatInt(item.ID, 10)+"/PlaybackInfo?api_key="+authResult.AccessToken, nil))
	if playback.Code != http.StatusOK || !strings.Contains(playback.Body.String(), `"SupportsDirectPlay":true`) || !strings.Contains(playback.Body.String(), `"SupportsDirectStream":true`) || !strings.Contains(playback.Body.String(), `"SupportsTranscoding":false`) || !strings.Contains(playback.Body.String(), `"DirectStreamUrl":"/Videos/`) || strings.Contains(playback.Body.String(), `"DirectStreamUrl":"/emby/`) {
		t.Fatalf("playback status = %d body = %s", playback.Code, playback.Body.String())
	}

	segments := httptest.NewRecorder()
	server.ServeHTTP(segments, httptest.NewRequest(http.MethodGet,
		"/emby/MediaSegments/"+strconv.FormatInt(item.ID, 10)+"?api_key="+authResult.AccessToken, nil))
	if segments.Code != http.StatusOK || strings.TrimSpace(segments.Body.String()) != "[]" {
		t.Fatalf("media segments status = %d body = %s", segments.Code, segments.Body.String())
	}
	introSegments := httptest.NewRecorder()
	server.ServeHTTP(introSegments, httptest.NewRequest(http.MethodGet,
		"/emby/MediaSegments/"+strconv.FormatInt(item.ID, 10)+"/Intro?api_key="+authResult.AccessToken, nil))
	if introSegments.Code != http.StatusOK || strings.TrimSpace(introSegments.Body.String()) != "[]" {
		t.Fatalf("intro media segments status = %d body = %s", introSegments.Code, introSegments.Body.String())
	}

	stream := httptest.NewRecorder()
	streamRequest := httptest.NewRequest(http.MethodGet,
		"/emby/Videos/"+strconv.FormatInt(item.ID, 10)+"/stream.mkv?api_key="+authResult.AccessToken, nil)
	streamRequest.Header.Set("User-Agent", "SyntheticPlayer/1.0")
	server.ServeHTTP(stream, streamRequest)
	if stream.Code != http.StatusTemporaryRedirect || stream.Header().Get("Location") != remote.downloadURL {
		t.Fatalf("stream status = %d location = %q", stream.Code, stream.Header().Get("Location"))
	}
	if remote.userAgent != "SyntheticPlayer/1.0" {
		t.Fatalf("playback User-Agent = %q", remote.userAgent)
	}
}

func TestEmbyAdministratorLibrarySelectionFiltersViewsAndItems(t *testing.T) {
	server, store, _, visibleItem, _ := newEmbyTestServer(t)
	hiddenLibrary, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
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
		LibraryID: hiddenLibrary.ID, Kind: medialibrary.KindMovie, Path: "/Hidden/Secret.mkv",
		MediaPath: "/Hidden/Secret.mkv", Name: "Secret", SortName: "Secret", Container: "mkv",
		RuntimeTicks: 10_000_000, Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishLibraryScan(t.Context(), hiddenLibrary.ID, generation); err != nil {
		t.Fatal(err)
	}
	admin, err := store.GetLocalUserByUsername(t.Context(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	admin.LibraryGrantsConfigured = true
	admin.LibraryGrants = []domain.LibraryGrant{{LibraryID: visibleItem.LibraryID, Access: domain.LibraryAccessEdit}}
	if _, err := store.UpdateUser(t.Context(), admin); err != nil {
		t.Fatal(err)
	}
	auth := authenticateEmby(t, server)

	viewsResponse := httptest.NewRecorder()
	server.ServeHTTP(viewsResponse, httptest.NewRequest(http.MethodGet, "/emby/Users/1/Views?api_key="+auth.AccessToken, nil))
	if viewsResponse.Code != http.StatusOK || strings.Contains(viewsResponse.Body.String(), `"Name":"Hidden"`) || !strings.Contains(viewsResponse.Body.String(), `"Name":"Movies"`) {
		t.Fatalf("administrator views status = %d body = %s", viewsResponse.Code, viewsResponse.Body.String())
	}

	itemsResponse := httptest.NewRecorder()
	server.ServeHTTP(itemsResponse, httptest.NewRequest(http.MethodGet, "/emby/Items?UserId=1&Recursive=true&api_key="+auth.AccessToken, nil))
	if itemsResponse.Code != http.StatusOK || strings.Contains(itemsResponse.Body.String(), `"Name":"Secret"`) || !strings.Contains(itemsResponse.Body.String(), `"Name":"Dune"`) {
		t.Fatalf("administrator items status = %d body = %s", itemsResponse.Code, itemsResponse.Body.String())
	}

	directResponse := httptest.NewRecorder()
	server.ServeHTTP(directResponse, httptest.NewRequest(http.MethodGet,
		"/emby/Items/"+strconv.FormatInt(hiddenItem.ID, 10)+"?api_key="+auth.AccessToken, nil))
	if directResponse.Code != http.StatusOK || !strings.Contains(directResponse.Body.String(), `"Name":"Secret"`) {
		t.Fatalf("administrator direct hidden item status = %d body = %s", directResponse.Code, directResponse.Body.String())
	}
}

func TestEmbyMusicLibraryDefaultsToSongsAndReturnsLocalLyrics(t *testing.T) {
	var media *medialibrary.Service
	server, store, _, _, _ := newEmbyTestServer(t, func(service *medialibrary.Service) { media = service })
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{Name: "Music", ContentType: medialibrary.ContentMusic, RootPath: "/Music"})
	if err != nil {
		t.Fatal(err)
	}
	generation, err := store.BeginLibraryScan(t.Context(), library.ID)
	if err != nil {
		t.Fatal(err)
	}
	artist, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{LibraryID: library.ID, Kind: medialibrary.KindMusicArtist, Path: "/Music/@artist/Artist", Name: "Artist", SortName: "Artist", Artists: []string{"Artist"}, ScanGeneration: generation})
	if err != nil {
		t.Fatal(err)
	}
	album, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{LibraryID: library.ID, ParentID: &artist.ID, Kind: medialibrary.KindMusicAlbum, Path: "/Music/@artist/Artist/@album/Album", Name: "Album", SortName: "Album", Album: "Album", AlbumArtist: "Artist", Artists: []string{"Artist"}, ScanGeneration: generation})
	if err != nil {
		t.Fatal(err)
	}
	audio, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, ParentID: &album.ID, Kind: medialibrary.KindAudio,
		Path: "/Music/Artist - Song.flac", MediaPath: "/Music/Artist - Song.flac",
		Name: "Song", SortName: "Song", Container: "flac", Album: "Album", AlbumArtist: "Artist", Artists: []string{"Artist"},
		RuntimeTicks: 269_000 * 10_000, Lyrics: "[00:01.50]First line\n[00:03.00]Second line", ScanGeneration: generation,
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
	cover, err := assets.StoreItemImage(t.Context(), album.ID, medialibrary.ImagePrimary, bytes.NewReader(picture))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertMediaItemImage(t.Context(), medialibrary.LocalImage{
		ItemID: album.ID, ImageType: medialibrary.ImagePrimary, SourceType: medialibrary.ImageSourceRemote,
		RelativePath: cover.RelativePath, MIMEType: cover.MIMEType, SizeBytes: cover.SizeBytes,
		Width: cover.Width, Height: cover.Height, SHA256: cover.SHA256, UpdatedAt: cover.UpdatedAt,
	}); err != nil {
		t.Fatal(err)
	}
	auth := authenticateEmby(t, server)
	libraryID := strconv.FormatInt(8_000_000_000_000_000_000+library.ID, 10)
	items := httptest.NewRecorder()
	server.ServeHTTP(items, httptest.NewRequest(http.MethodGet, "/emby/Users/1/Items?ParentId="+libraryID+"&api_key="+auth.AccessToken, nil))
	if items.Code != http.StatusOK || !strings.Contains(items.Body.String(), `"Type":"Audio"`) || strings.Contains(items.Body.String(), `"Type":"MusicArtist"`) {
		t.Fatalf("music root status = %d body = %s", items.Code, items.Body.String())
	}
	listed := httptest.NewRecorder()
	server.ServeHTTP(listed, httptest.NewRequest(http.MethodGet, "/emby/Users/1/Items?ParentId="+libraryID+"&Recursive=true&IncludeItemTypes=Audio&Fields=MediaSources&api_key="+auth.AccessToken, nil))
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"RunTimeTicks":2690000000`) ||
		!strings.Contains(listed.Body.String(), `"MediaSources":[{`) ||
		!strings.Contains(listed.Body.String(), `"AlbumId":"`+strconv.FormatInt(album.ID, 10)+`"`) ||
		!strings.Contains(listed.Body.String(), `"ArtistItems":[{"Name":"Artist","Id":"`+strconv.FormatInt(artist.ID, 10)+`"}]`) {
		t.Fatalf("audio list status = %d body = %s", listed.Code, listed.Body.String())
	}
	for _, endpoint := range []string{"/emby/Artists", "/emby/Artists/AlbumArtists"} {
		artists := httptest.NewRecorder()
		server.ServeHTTP(artists, httptest.NewRequest(http.MethodGet, endpoint+"?UserId=1&api_key="+auth.AccessToken, nil))
		if artists.Code != http.StatusOK || !strings.Contains(artists.Body.String(), `"Type":"MusicArtist"`) || !strings.Contains(artists.Body.String(), `"Name":"Artist"`) ||
			!strings.Contains(artists.Body.String(), `"AlbumCount":1`) || !strings.Contains(artists.Body.String(), `"SongCount":1`) || !strings.Contains(artists.Body.String(), `"ChildCount":1`) {
			t.Fatalf("artists endpoint %s status = %d body = %s", endpoint, artists.Code, artists.Body.String())
		}
	}
	otherArtist, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{LibraryID: library.ID, Kind: medialibrary.KindMusicArtist, Path: "/Music/@artist/Other", Name: "Other", SortName: "Other", Artists: []string{"Other"}, ScanGeneration: generation})
	if err != nil {
		t.Fatal(err)
	}
	otherAlbum, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{LibraryID: library.ID, ParentID: &otherArtist.ID, Kind: medialibrary.KindMusicAlbum, Path: "/Music/@artist/Other/@album/Other Album", Name: "Other Album", SortName: "Other Album", Album: "Other Album", AlbumArtist: "Other", Artists: []string{"Other"}, ScanGeneration: generation})
	if err != nil {
		t.Fatal(err)
	}
	otherAudio, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{LibraryID: library.ID, ParentID: &otherAlbum.ID, Kind: medialibrary.KindAudio, Path: "/Music/Other - Track.flac", MediaPath: "/Music/Other - Track.flac", Name: "Other Track", SortName: "Other Track", Album: "Other Album", AlbumArtist: "Other", Artists: []string{"Other"}, ScanGeneration: generation})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		itemType  string
		parameter string
		included  int64
		excluded  int64
	}{
		{name: "albums by artist", itemType: "MusicAlbum", parameter: "ArtistIds", included: album.ID, excluded: otherAlbum.ID},
		{name: "songs by artist", itemType: "Audio", parameter: "ArtistIds", included: audio.ID, excluded: otherAudio.ID},
		{name: "albums by album artist", itemType: "MusicAlbum", parameter: "AlbumArtistIds", included: album.ID, excluded: otherAlbum.ID},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			requestURL := "/emby/Users/1/Items?Recursive=true&IncludeItemTypes=" + test.itemType + "&" + test.parameter + "=" + strconv.FormatInt(artist.ID, 10) + "&api_key=" + auth.AccessToken
			server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, requestURL, nil))
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"Id":"`+strconv.FormatInt(test.included, 10)+`"`) || strings.Contains(response.Body.String(), `"Id":"`+strconv.FormatInt(test.excluded, 10)+`"`) {
				t.Fatalf("artist-filtered items status = %d body = %s", response.Code, response.Body.String())
			}
		})
	}
	detail := httptest.NewRecorder()
	server.ServeHTTP(detail, httptest.NewRequest(http.MethodGet, "/emby/Items/"+strconv.FormatInt(audio.ID, 10)+"?api_key="+auth.AccessToken, nil))
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"HasLyrics":true`) ||
		!strings.Contains(detail.Body.String(), `"AlbumId":"`+strconv.FormatInt(album.ID, 10)+`"`) ||
		!strings.Contains(detail.Body.String(), `"AlbumPrimaryImageTag":"`+cover.SHA256+`"`) ||
		!strings.Contains(detail.Body.String(), `"PrimaryImageItemId":"`+strconv.FormatInt(album.ID, 10)+`"`) ||
		!strings.Contains(detail.Body.String(), `"PrimaryImageTag":"`+cover.SHA256+`"`) ||
		!strings.Contains(detail.Body.String(), `"ArtistItems":[{"Name":"Artist","Id":"`+strconv.FormatInt(artist.ID, 10)+`"}]`) {
		t.Fatalf("audio detail status = %d body = %s", detail.Code, detail.Body.String())
	}
	image := httptest.NewRecorder()
	server.ServeHTTP(image, httptest.NewRequest(http.MethodGet, "/emby/Items/"+strconv.FormatInt(album.ID, 10)+"/Images/Primary?tag="+cover.SHA256+"&api_key="+auth.AccessToken, nil))
	if image.Code != http.StatusOK || image.Header().Get("Content-Type") != "image/png" || image.Body.Len() == 0 {
		t.Fatalf("album image status = %d type = %q body bytes = %d", image.Code, image.Header().Get("Content-Type"), image.Body.Len())
	}
	publicImage := httptest.NewRecorder()
	server.ServeHTTP(publicImage, httptest.NewRequest(http.MethodGet, "/Items/"+strconv.FormatInt(album.ID, 10)+"/Images/Primary?Tag="+cover.SHA256+"&MaxWidth=600&Quality=90", nil))
	if publicImage.Code != http.StatusOK || publicImage.Header().Get("Content-Type") != "image/jpeg" || publicImage.Body.Len() == 0 {
		t.Fatalf("public album image status = %d type = %q body bytes = %d", publicImage.Code, publicImage.Header().Get("Content-Type"), publicImage.Body.Len())
	}
	wrongTagImage := httptest.NewRecorder()
	server.ServeHTTP(wrongTagImage, httptest.NewRequest(http.MethodGet, "/Items/"+strconv.FormatInt(album.ID, 10)+"/Images/Primary?tag=wrong", nil))
	if wrongTagImage.Code != http.StatusNotFound {
		t.Fatalf("wrong image tag status = %d body = %s", wrongTagImage.Code, wrongTagImage.Body.String())
	}
	untaggedImage := httptest.NewRecorder()
	server.ServeHTTP(untaggedImage, httptest.NewRequest(http.MethodGet, "/Items/"+strconv.FormatInt(album.ID, 10)+"/Images/Primary", nil))
	if untaggedImage.Code != http.StatusOK || untaggedImage.Header().Get("Content-Type") != "image/png" || untaggedImage.Body.Len() == 0 {
		t.Fatalf("untagged image status = %d type = %q body bytes = %d", untaggedImage.Code, untaggedImage.Header().Get("Content-Type"), untaggedImage.Body.Len())
	}
	headImage := httptest.NewRecorder()
	server.ServeHTTP(headImage, httptest.NewRequest(http.MethodHead, "/Items/"+strconv.FormatInt(album.ID, 10)+"/Images/Primary", nil))
	if headImage.Code != http.StatusOK || headImage.Header().Get("Content-Type") != "image/png" || headImage.Body.Len() != 0 {
		t.Fatalf("head image status = %d type = %q body bytes = %d", headImage.Code, headImage.Header().Get("Content-Type"), headImage.Body.Len())
	}
	indexedImage := httptest.NewRecorder()
	server.ServeHTTP(indexedImage, httptest.NewRequest(http.MethodGet, "/Items/"+strconv.FormatInt(album.ID, 10)+"/Images/Primary/0", nil))
	if indexedImage.Code != http.StatusOK || indexedImage.Header().Get("Content-Type") != "image/png" || indexedImage.Body.Len() == 0 {
		t.Fatalf("indexed image status = %d type = %q body bytes = %d", indexedImage.Code, indexedImage.Header().Get("Content-Type"), indexedImage.Body.Len())
	}
	unsupportedImageIndex := httptest.NewRecorder()
	server.ServeHTTP(unsupportedImageIndex, httptest.NewRequest(http.MethodGet, "/Items/"+strconv.FormatInt(album.ID, 10)+"/Images/Primary/1", nil))
	if unsupportedImageIndex.Code != http.StatusNotFound {
		t.Fatalf("unsupported anonymous image index status = %d body = %s", unsupportedImageIndex.Code, unsupportedImageIndex.Body.String())
	}
	nonImage := httptest.NewRecorder()
	server.ServeHTTP(nonImage, httptest.NewRequest(http.MethodGet, "/Items/"+strconv.FormatInt(album.ID, 10), nil))
	if nonImage.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated item status = %d body = %s", nonImage.Code, nonImage.Body.String())
	}
	unauthenticatedImageInfo := httptest.NewRecorder()
	server.ServeHTTP(unauthenticatedImageInfo, httptest.NewRequest(http.MethodGet, "/Items/"+strconv.FormatInt(album.ID, 10)+"/Images", nil))
	if unauthenticatedImageInfo.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated image info status = %d body = %s", unauthenticatedImageInfo.Code, unauthenticatedImageInfo.Body.String())
	}
	unauthenticatedImageWrite := httptest.NewRecorder()
	server.ServeHTTP(unauthenticatedImageWrite, httptest.NewRequest(http.MethodPost, "/Items/"+strconv.FormatInt(album.ID, 10)+"/Images/Primary", nil))
	if unauthenticatedImageWrite.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated image write status = %d body = %s", unauthenticatedImageWrite.Code, unauthenticatedImageWrite.Body.String())
	}
	imageInfo := httptest.NewRecorder()
	server.ServeHTTP(imageInfo, httptest.NewRequest(http.MethodGet, "/emby/Items/"+strconv.FormatInt(album.ID, 10)+"/Images?api_key="+auth.AccessToken, nil))
	if imageInfo.Code != http.StatusOK || !strings.Contains(imageInfo.Body.String(), `"ImageType":"Primary"`) ||
		!strings.Contains(imageInfo.Body.String(), `"Height":1`) || !strings.Contains(imageInfo.Body.String(), `"Width":1`) {
		t.Fatalf("album image info status = %d body = %s", imageInfo.Code, imageInfo.Body.String())
	}
	lyrics := httptest.NewRecorder()
	server.ServeHTTP(lyrics, httptest.NewRequest(http.MethodGet, "/emby/Audio/"+strconv.FormatInt(audio.ID, 10)+"/Lyrics?api_key="+auth.AccessToken, nil))
	if lyrics.Code != http.StatusOK || !strings.Contains(lyrics.Body.String(), `"IsSynced":true`) || !strings.Contains(lyrics.Body.String(), `"Text":"First line"`) {
		t.Fatalf("lyrics status = %d body = %s", lyrics.Code, lyrics.Body.String())
	}
}

func TestEmbyLyricsAreAvailableBeforeStreamResolutionCompletes(t *testing.T) {
	server, store, remote, _, _ := newEmbyTestServer(t)
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{Name: "Lyrics", ContentType: medialibrary.ContentMusic, RootPath: "/Lyrics"})
	if err != nil {
		t.Fatal(err)
	}
	generation, err := store.BeginLibraryScan(t.Context(), library.ID)
	if err != nil {
		t.Fatal(err)
	}
	audio, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, Kind: medialibrary.KindAudio, Path: "/Lyrics/song.flac", MediaPath: "/Lyrics/song.flac",
		Name: "Song", SortName: "Song", Container: "flac", RuntimeTicks: 180 * 10_000_000,
		Lyrics: "[00:01.00]Immediate", ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishLibraryScan(t.Context(), library.ID, generation); err != nil {
		t.Fatal(err)
	}
	auth := authenticateEmby(t, server)
	remote.resolveStarted = make(chan struct{})
	remote.resolveRelease = make(chan struct{})
	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		stream := httptest.NewRecorder()
		server.ServeHTTP(stream, httptest.NewRequest(http.MethodGet, "/emby/Audio/"+strconv.FormatInt(audio.ID, 10)+"/stream.flac?api_key="+auth.AccessToken, nil))
	}()
	select {
	case <-remote.resolveStarted:
	case <-time.After(time.Second):
		t.Fatal("stream resolution did not start")
	}
	lyrics := httptest.NewRecorder()
	server.ServeHTTP(lyrics, httptest.NewRequest(http.MethodGet, "/emby/Audio/"+strconv.FormatInt(audio.ID, 10)+"/Lyrics?api_key="+auth.AccessToken, nil))
	if lyrics.Code != http.StatusOK || !strings.Contains(lyrics.Body.String(), `"Text":"Immediate"`) {
		t.Fatalf("lyrics status = %d body = %s", lyrics.Code, lyrics.Body.String())
	}
	close(remote.resolveRelease)
	select {
	case <-streamDone:
	case <-time.After(time.Second):
		t.Fatal("stream request did not finish")
	}
}

func TestEmbyPeopleAndPersonIDsFollowCurrentUserAccess(t *testing.T) {
	server, store, _, item, metadata := newEmbyTestServer(t)
	if err := store.UpdateSettings(t.Context(), "http://openlist.local", "admin-key", "tmdb-key"); err != nil {
		t.Fatal(err)
	}
	movieID := int64(101)
	if err := store.UpdateMediaItemMetadata(t.Context(), item.ID, medialibrary.MetadataInput{
		Name: item.Name, SortName: item.SortName, TMDBID: &movieID, Genres: []string{}, Artists: []string{},
	}); err != nil {
		t.Fatal(err)
	}
	metadata.creditsFn = func(_ context.Context, apiKey string, query domain.MediaCreditsQuery, language string) ([]domain.MediaPerson, error) {
		if apiKey != "tmdb-key" || query.MediaType != "movie" || query.TMDBID != movieID {
			t.Fatalf("credits query = %#v key=%q language=%q", query, apiKey, language)
		}
		if language != "zh-CN" {
			t.Fatalf("unexpected credits language = %q", language)
		}
		return []domain.MediaPerson{{ID: 99, Name: "丽贝卡·弗格森", ProfileURL: "https://image.tmdb.org/t/p/w500/rebecca.jpg", Type: "Actor", Role: "Lady Jessica", DisplayRole: "演员", SortOrder: 0}}, nil
	}
	if err := store.ReplaceMediaItemPeople(t.Context(), item.ID, []medialibrary.LocalPerson{{Person: domain.MediaPerson{ID: 99, Name: "Rebecca Ferguson", Type: "Actor"}}}); err != nil {
		t.Fatal(err)
	}
	metadata.profileFn = func(_ context.Context, apiKey string, personID int64, language string) (string, error) {
		if apiKey != "tmdb-key" || personID != 99 || language != "zh-CN" {
			t.Fatalf("person profile = person=%d key=%q language=%q", personID, apiKey, language)
		}
		return "https://image.tmdb.org/t/p/w500/rebecca.jpg", nil
	}
	auth := authenticateEmby(t, server)

	peopleRecorder := httptest.NewRecorder()
	server.ServeHTTP(peopleRecorder, httptest.NewRequest(http.MethodGet,
		"/emby/Items/"+strconv.FormatInt(item.ID, 10)+"/People?Language=en&api_key="+auth.AccessToken, nil))
	var people []struct {
		ID              string `json:"Id"`
		Name            string `json:"Name"`
		Role            string `json:"Role"`
		Type            string `json:"Type"`
		PrimaryImageTag string `json:"PrimaryImageTag"`
	}
	if peopleRecorder.Code != http.StatusOK || json.Unmarshal(peopleRecorder.Body.Bytes(), &people) != nil || len(people) != 1 || people[0].ID != "99" || people[0].Role != "演员" || people[0].Type != "Actor" || people[0].PrimaryImageTag != "person:99" {
		t.Fatalf("people response status = %d body = %s", peopleRecorder.Code, peopleRecorder.Body.String())
	}

	chineseDetailRecorder := httptest.NewRecorder()
	chineseDetailRequest := httptest.NewRequest(http.MethodGet,
		"/emby/Items/"+strconv.FormatInt(item.ID, 10)+"?Fields=People&api_key="+auth.AccessToken, nil)
	chineseDetailRequest.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	server.ServeHTTP(chineseDetailRecorder, chineseDetailRequest)
	var chineseDetail struct {
		People []struct {
			Name string `json:"Name"`
			Role string `json:"Role"`
		} `json:"People"`
	}
	if chineseDetailRecorder.Code != http.StatusOK || json.Unmarshal(chineseDetailRecorder.Body.Bytes(), &chineseDetail) != nil || len(chineseDetail.People) != 1 || chineseDetail.People[0].Name != "丽贝卡·弗格森" || chineseDetail.People[0].Role != "演员" {
		t.Fatalf("Chinese item people status = %d body = %s", chineseDetailRecorder.Code, chineseDetailRecorder.Body.String())
	}

	for _, imagePath := range []string{
		"/emby/Persons/99/Images/Primary?Language=zh-CN&api_key=" + auth.AccessToken,
		"/emby/Items/99/Images/Primary?tag=person%3A99&Language=zh-CN&api_key=" + auth.AccessToken,
	} {
		imageRecorder := httptest.NewRecorder()
		server.ServeHTTP(imageRecorder, httptest.NewRequest(http.MethodGet, imagePath, nil))
		if imageRecorder.Code != http.StatusFound || imageRecorder.Header().Get("Location") != "https://image.tmdb.org/t/p/w500/rebecca.jpg" {
			t.Fatalf("person image %s status = %d location = %q", imagePath, imageRecorder.Code, imageRecorder.Header().Get("Location"))
		}
	}
	publicPersonImage := httptest.NewRecorder()
	server.ServeHTTP(publicPersonImage, httptest.NewRequest(http.MethodGet, "/Persons/99/Images/Primary?Language=zh-CN", nil))
	if publicPersonImage.Code != http.StatusNotFound {
		t.Fatalf("uncached public person image status = %d body = %s", publicPersonImage.Code, publicPersonImage.Body.String())
	}

	detailRecorder := httptest.NewRecorder()
	server.ServeHTTP(detailRecorder, httptest.NewRequest(http.MethodGet,
		"/emby/Users/1/Items/"+strconv.FormatInt(item.ID, 10)+"?Fields=People&Language=en&api_key="+auth.AccessToken, nil))
	var detail struct {
		People []struct {
			ID   string `json:"Id"`
			Name string `json:"Name"`
		} `json:"People"`
	}
	if detailRecorder.Code != http.StatusOK || json.Unmarshal(detailRecorder.Body.Bytes(), &detail) != nil || len(detail.People) != 1 || detail.People[0].ID != "99" || detail.People[0].Name != "丽贝卡·弗格森" {
		t.Fatalf("item detail people status = %d body = %s", detailRecorder.Code, detailRecorder.Body.String())
	}

	itemsRecorder := httptest.NewRecorder()
	server.ServeHTTP(itemsRecorder, httptest.NewRequest(http.MethodGet,
		"/emby/Users/1/Items?PersonIds=99&IncludeItemTypes=Movie&Fields=People&Language=en&api_key="+auth.AccessToken, nil))
	var items struct {
		Items []struct {
			ID     string `json:"Id"`
			People []struct {
				ID string `json:"Id"`
			} `json:"People"`
		} `json:"Items"`
	}
	if itemsRecorder.Code != http.StatusOK || json.Unmarshal(itemsRecorder.Body.Bytes(), &items) != nil || len(items.Items) != 1 || items.Items[0].ID != strconv.FormatInt(item.ID, 10) || len(items.Items[0].People) != 1 {
		t.Fatalf("person items status = %d body = %s", itemsRecorder.Code, itemsRecorder.Body.String())
	}

	wrongUserRecorder := httptest.NewRecorder()
	server.ServeHTTP(wrongUserRecorder, httptest.NewRequest(http.MethodGet,
		"/emby/Users/2/Items?PersonIds=99&api_key="+auth.AccessToken, nil))
	if wrongUserRecorder.Code != http.StatusNotFound {
		t.Fatalf("cross-user person query status = %d body = %s", wrongUserRecorder.Code, wrongUserRecorder.Body.String())
	}
}

func TestEmbyPeopleVisibilitySettingFiltersMissingImages(t *testing.T) {
	server, store, _, item, metadata := newEmbyTestServer(t)
	if err := store.UpdateSettings(t.Context(), "http://openlist.local", "admin-key", "tmdb-key"); err != nil {
		t.Fatal(err)
	}
	library, err := store.GetLibrary(t.Context(), item.LibraryID)
	if err != nil {
		t.Fatal(err)
	}
	hide := true
	if _, err := store.UpdateLibrary(t.Context(), library.ID, medialibrary.LibraryInput{
		Name: library.Name, ContentType: library.ContentType, RootPaths: library.RootPaths,
		HidePeopleWithoutImages: &hide,
	}); err != nil {
		t.Fatal(err)
	}
	tmdbID := int64(101)
	if err := store.UpdateMediaItemMetadata(t.Context(), item.ID, medialibrary.MetadataInput{
		Name: item.Name, SortName: item.SortName, TMDBID: &tmdbID, Genres: []string{}, Artists: []string{},
	}); err != nil {
		t.Fatal(err)
	}
	metadata.creditsFn = func(_ context.Context, apiKey string, query domain.MediaCreditsQuery, _ string) ([]domain.MediaPerson, error) {
		if apiKey != "tmdb-key" || query.MediaType != "movie" || query.TMDBID != tmdbID {
			t.Fatalf("credits query = %#v key=%q", query, apiKey)
		}
		return []domain.MediaPerson{
			{ID: 1, Name: "With image", ProfileURL: "https://image.example/with.jpg", Type: "Actor"},
			{ID: 2, Name: "Without image", Type: "Actor"},
		}, nil
	}
	auth := authenticateEmby(t, server)
	assertPeople := func(path string) {
		t.Helper()
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("people status = %d body = %s", recorder.Code, recorder.Body.String())
		}
		var response struct {
			People []struct {
				ID string `json:"Id"`
			} `json:"People"`
			Items []struct {
				People []struct {
					ID string `json:"Id"`
				} `json:"People"`
			} `json:"Items"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode people response: %v body = %s", err, recorder.Body.String())
		}
		people := response.People
		if len(people) == 0 && len(response.Items) == 1 {
			people = response.Items[0].People
		}
		if len(people) != 1 || people[0].ID != "1" {
			t.Fatalf("filtered people = %#v body = %s", people, recorder.Body.String())
		}
	}
	assertPeople("/emby/Items/" + strconv.FormatInt(item.ID, 10) + "?Fields=People&api_key=" + auth.AccessToken)
	assertPeople("/emby/Users/1/Items?IncludeItemTypes=Movie&Fields=People&api_key=" + auth.AccessToken)

	peopleRecorder := httptest.NewRecorder()
	server.ServeHTTP(peopleRecorder, httptest.NewRequest(http.MethodGet,
		"/emby/Items/"+strconv.FormatInt(item.ID, 10)+"/People?api_key="+auth.AccessToken, nil))
	var people []struct {
		ID string `json:"Id"`
	}
	if peopleRecorder.Code != http.StatusOK || json.Unmarshal(peopleRecorder.Body.Bytes(), &people) != nil || len(people) != 1 || people[0].ID != "1" {
		t.Fatalf("filtered people endpoint status = %d body = %s", peopleRecorder.Code, peopleRecorder.Body.String())
	}
}

func TestEmbyEpisodeListsAppendEpisodeGuestsToSeasonPeople(t *testing.T) {
	server, store, _, _, metadata := newEmbyTestServer(t)
	if err := store.UpdateSettings(t.Context(), "http://openlist.local", "admin-key", "tmdb-key"); err != nil {
		t.Fatal(err)
	}
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
	tmdbID := int64(95396)
	series, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, Kind: medialibrary.KindSeries, Path: "/TV/Severance",
		Name: "Severance", SortName: "Severance", TMDBID: &tmdbID,
		Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	seasonNumber := 1
	season, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, ParentID: &series.ID, Kind: medialibrary.KindSeason,
		Path: "/TV/Severance/Season 01", Name: "Season 1", SortName: "0001",
		SeasonNumber: &seasonNumber, Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	episodes := make([]medialibrary.Item, 0, 2)
	for number := 1; number <= 2; number++ {
		episodeNumber := number
		episode, _, createErr := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
			LibraryID: library.ID, ParentID: &season.ID, Kind: medialibrary.KindEpisode,
			Path:      "/TV/Severance/Season 01/S01E0" + strconv.Itoa(number) + ".mkv",
			MediaPath: "/TV/Severance/Season 01/S01E0" + strconv.Itoa(number) + ".mkv",
			Name:      "Episode " + strconv.Itoa(number), SortName: "000" + strconv.Itoa(number), Container: "mkv",
			SeasonNumber: &seasonNumber, EpisodeNumber: &episodeNumber,
			Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		episodes = append(episodes, episode)
	}
	if _, err := store.FinishLibraryScan(t.Context(), library.ID, generation); err != nil {
		t.Fatal(err)
	}

	seasonCalls, episodeCalls, batchCalls := 0, 0, 0
	metadata.creditsFn = func(_ context.Context, apiKey string, query domain.MediaCreditsQuery, language string) ([]domain.MediaPerson, error) {
		if apiKey != "tmdb-key" || query.MediaType != "tv" || query.TMDBID != tmdbID || query.SeasonNumber == nil || *query.SeasonNumber != seasonNumber || language != "zh-CN" {
			t.Fatalf("credits query = %#v key=%q language=%q", query, apiKey, language)
		}
		if query.EpisodeNumber == nil {
			seasonCalls++
			return []domain.MediaPerson{{ID: 100, Name: "Season Actor", Type: "Actor", Role: "Regular", SortOrder: 0}}, nil
		}
		episodeCalls++
		guestID := int64(100 + *query.EpisodeNumber)
		return []domain.MediaPerson{{ID: guestID, Name: "Guest", Type: "Actor", Role: "Guest", SortOrder: 1}}, nil
	}
	metadata.seasonEpisodeCreditsFn = func(_ context.Context, apiKey string, query domain.MediaCreditsQuery, language string) (map[int][]domain.MediaPerson, error) {
		if apiKey != "tmdb-key" || query.MediaType != "tv" || query.TMDBID != tmdbID || query.SeasonNumber == nil || *query.SeasonNumber != seasonNumber || query.EpisodeNumber != nil || language != "zh-CN" {
			t.Fatalf("batch credits query = %#v key=%q language=%q", query, apiKey, language)
		}
		batchCalls++
		return map[int][]domain.MediaPerson{
			1: {{ID: 101, Name: "Guest 1", Type: "Actor", Role: "Guest", SortOrder: 1}},
			2: {{ID: 102, Name: "Guest 2", Type: "Actor", Role: "Guest", SortOrder: 1}},
		}, nil
	}

	auth := authenticateEmby(t, server)
	recorder := httptest.NewRecorder()
	requestPath := "/emby/Shows/" + strconv.FormatInt(series.ID, 10) + "/Episodes?SeasonId=" +
		strconv.FormatInt(season.ID, 10) + "&Fields=People&Language=en&api_key=" + auth.AccessToken
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, requestPath, nil))
	var response struct {
		Items []struct {
			ID     string `json:"Id"`
			People []struct {
				ID string `json:"Id"`
			} `json:"People"`
		} `json:"Items"`
	}
	if recorder.Code != http.StatusOK || json.Unmarshal(recorder.Body.Bytes(), &response) != nil || len(response.Items) != 2 {
		t.Fatalf("episode people status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	peopleByEpisode := make(map[string]map[string]bool, len(response.Items))
	for _, item := range response.Items {
		ids := make(map[string]bool, len(item.People))
		for _, person := range item.People {
			ids[person.ID] = true
		}
		peopleByEpisode[item.ID] = ids
	}
	for index, episode := range episodes {
		people := peopleByEpisode[strconv.FormatInt(episode.ID, 10)]
		guestID := strconv.Itoa(101 + index)
		if !people["100"] || !people[guestID] || len(people) != 2 {
			t.Fatalf("episode %d people = %#v, want season actor and guest %s", episode.ID, people, guestID)
		}
	}
	if seasonCalls != 1 || batchCalls != 1 || episodeCalls != 0 {
		t.Fatalf("credits calls = season %d batch %d episode %d, want 1, 1 and 0", seasonCalls, batchCalls, episodeCalls)
	}
}

func TestEmbyScopesTokensUsersLibrariesAndPlaybackState(t *testing.T) {
	server, store, _, item, _ := newEmbyTestServer(t)
	bob, err := store.CreateUser(t.Context(), domain.LocalUser{OpenListUserID: 101, InstanceID: "test-instance",
		Username: "bob", Role: domain.LocalUserRoleMedia, Enabled: true,
		LibraryGrants: []domain.LibraryGrant{{LibraryID: item.LibraryID, Access: domain.LibraryAccessView}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateUser(t.Context(), domain.LocalUser{OpenListUserID: 102, InstanceID: "test-instance",
		Username: "disabled", Role: domain.LocalUserRoleMedia, Enabled: false,
		LibraryGrants: []domain.LibraryGrant{{LibraryID: item.LibraryID, Access: domain.LibraryAccessView}},
	}); err != nil {
		t.Fatal(err)
	}

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
		RuntimeTicks: 10_000_000, Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishLibraryScan(t.Context(), privateLibrary.ID, generation); err != nil {
		t.Fatal(err)
	}

	public := httptest.NewRecorder()
	server.ServeHTTP(public, httptest.NewRequest(http.MethodGet, "/emby/Users/Public", nil))
	var publicUsers []struct {
		ID   string `json:"Id"`
		Name string `json:"Name"`
	}
	if public.Code != http.StatusOK || json.Unmarshal(public.Body.Bytes(), &publicUsers) != nil {
		t.Fatalf("public users status = %d body = %s", public.Code, public.Body.String())
	}
	if len(publicUsers) != 2 || publicUsers[0].Name != "alice" || publicUsers[1].ID != strconv.FormatInt(bob.ID, 10) || publicUsers[1].Name != "bob" {
		t.Fatalf("public users = %#v", publicUsers)
	}

	bobAuth := authenticateEmbyAs(t, server, "bob", "bob-password", "bob-device")
	if bobAuth.AccessToken == "" || bobAuth.User.Id != strconv.FormatInt(bob.ID, 10) || bobAuth.User.Policy.IsAdministrator {
		t.Fatalf("Bob authentication result = %#v", bobAuth)
	}

	wrongUserPath := httptest.NewRecorder()
	server.ServeHTTP(wrongUserPath, httptest.NewRequest(http.MethodGet, "/emby/Users/1/Views?api_key="+bobAuth.AccessToken, nil))
	if wrongUserPath.Code != http.StatusNotFound {
		t.Fatalf("cross-user view status = %d body = %s", wrongUserPath.Code, wrongUserPath.Body.String())
	}

	bobViews := httptest.NewRecorder()
	server.ServeHTTP(bobViews, httptest.NewRequest(http.MethodGet, "/emby/Users/"+bobAuth.User.Id+"/Views?api_key="+bobAuth.AccessToken, nil))
	var views struct {
		Items []struct {
			Name string `json:"Name"`
		} `json:"Items"`
	}
	if bobViews.Code != http.StatusOK || json.Unmarshal(bobViews.Body.Bytes(), &views) != nil || len(views.Items) != 1 || views.Items[0].Name != "Movies" {
		t.Fatalf("Bob views status = %d response = %s", bobViews.Code, bobViews.Body.String())
	}

	privateItemResponse := httptest.NewRecorder()
	server.ServeHTTP(privateItemResponse, httptest.NewRequest(http.MethodGet,
		"/emby/Items/"+strconv.FormatInt(privateItem.ID, 10)+"?api_key="+bobAuth.AccessToken, nil))
	if privateItemResponse.Code != http.StatusNotFound {
		t.Fatalf("unauthorized item status = %d body = %s", privateItemResponse.Code, privateItemResponse.Body.String())
	}

	progress := httptest.NewRecorder()
	server.ServeHTTP(progress, httptest.NewRequest(http.MethodPost,
		"/emby/Sessions/Playing/Progress?api_key="+bobAuth.AccessToken,
		bytes.NewBufferString(`{"ItemId":"`+strconv.FormatInt(item.ID, 10)+`","PositionTicks":4200000}`)))
	if progress.Code != http.StatusNoContent {
		t.Fatalf("Bob playback update status = %d body = %s", progress.Code, progress.Body.String())
	}

	readPosition := func(token string) int64 {
		t.Helper()
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet,
			"/emby/Items/"+strconv.FormatInt(item.ID, 10)+"?api_key="+token, nil))
		var response struct {
			UserData struct {
				Position int64 `json:"PlaybackPositionTicks"`
			} `json:"UserData"`
		}
		if recorder.Code != http.StatusOK || json.Unmarshal(recorder.Body.Bytes(), &response) != nil {
			t.Fatalf("read playback position status = %d body = %s", recorder.Code, recorder.Body.String())
		}
		return response.UserData.Position
	}
	aliceAuth := authenticateEmby(t, server)
	if position := readPosition(aliceAuth.AccessToken); position != 0 {
		t.Fatalf("Alice playback position = %d, want 0", position)
	}
	if position := readPosition(bobAuth.AccessToken); position != 4_200_000 {
		t.Fatalf("Bob playback position = %d, want 4200000", position)
	}

	bob.Enabled = false
	if _, err := store.UpdateUser(t.Context(), bob); err != nil {
		t.Fatal(err)
	}
	disabledToken := httptest.NewRecorder()
	server.ServeHTTP(disabledToken, httptest.NewRequest(http.MethodGet,
		"/emby/Items/"+strconv.FormatInt(item.ID, 10)+"?api_key="+bobAuth.AccessToken, nil))
	if disabledToken.Code != http.StatusUnauthorized {
		t.Fatalf("disabled token status = %d body = %s", disabledToken.Code, disabledToken.Body.String())
	}
}

func TestEmbyBrowsePreservesTVHierarchyWhenClientRequestsRecursiveItems(t *testing.T) {
	var media *medialibrary.Service
	server, store, _, _, _ := newEmbyTestServer(t, func(service *medialibrary.Service) { media = service })
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
	seasonNumber := 1
	season, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, ParentID: &series.ID, Kind: medialibrary.KindSeason,
		Path: "/TV/Severance/Season 01", Name: "Season 1", SortName: "0001",
		SeasonNumber: &seasonNumber, Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	episodeNumber := 1
	episode, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, ParentID: &season.ID, Kind: medialibrary.KindEpisode,
		Path: "/TV/Severance/Season 01/S01E01.mkv", MediaPath: "/TV/Severance/Season 01/S01E01.mkv",
		Name: "Good News About Hell", SortName: "0001", Container: "mkv",
		SeasonNumber: &seasonNumber, EpisodeNumber: &episodeNumber, Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
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
	if _, err := store.UpdateMediaUserData(t.Context(), episode.ID, medialibrary.UserData{
		PlaybackPositionTicks: 4_200_000,
	}); err != nil {
		t.Fatal(err)
	}

	auth := authenticateEmby(t, server)
	viewsRecorder := httptest.NewRecorder()
	server.ServeHTTP(viewsRecorder, httptest.NewRequest(http.MethodGet, "/emby/Users/1/Views?api_key="+auth.AccessToken, nil))
	var views struct {
		Items []struct {
			Id             string `json:"Id"`
			CollectionType string `json:"CollectionType"`
		} `json:"Items"`
	}
	if err := json.Unmarshal(viewsRecorder.Body.Bytes(), &views); err != nil {
		t.Fatal(err)
	}
	viewID := ""
	for _, item := range views.Items {
		if item.CollectionType == "tvshows" {
			viewID = item.Id
			break
		}
	}
	if viewID == "" {
		t.Fatalf("TV view not found: %s", viewsRecorder.Body.String())
	}

	assertBrowseTypes := func(endpoint, parentID, includeTypes string, want string) {
		t.Helper()
		path := endpoint + "?Recursive=true&ParentId=" + parentID +
			"&IncludeItemTypes=" + includeTypes + "&api_key=" + auth.AccessToken
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("browse %s status = %d body = %s", parentID, recorder.Code, recorder.Body.String())
		}
		if strings.HasSuffix(endpoint, "/Latest") {
			var items []struct {
				Type string `json:"Type"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &items); err != nil {
				t.Fatal(err)
			}
			if len(items) != 1 || items[0].Type != want {
				t.Fatalf("browse %s latest items = %#v body = %s", parentID, items, recorder.Body.String())
			}
			return
		}
		var result struct {
			Items []struct {
				Type string `json:"Type"`
			} `json:"Items"`
			TotalRecordCount int `json:"TotalRecordCount"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.TotalRecordCount != 1 || len(result.Items) != 1 || result.Items[0].Type != want {
			t.Fatalf("browse %s result = %#v body = %s", parentID, result, recorder.Body.String())
		}
	}

	assertBrowseTypes("/emby/Users/1/Items", viewID, "Series,Season,Episode", "Series")
	assertBrowseTypes("/emby/Users/1/Items/Latest", viewID, "Episode", "Series")
	assertBrowseTypes("/emby/Users/1/Items", strconv.FormatInt(series.ID, 10), "Season,Episode", "Season")
	assertBrowseTypes("/emby/Users/1/Items", strconv.FormatInt(season.ID, 10), "Episode", "Episode")

	libraryChildrenRecorder := httptest.NewRecorder()
	server.ServeHTTP(libraryChildrenRecorder, httptest.NewRequest(http.MethodGet,
		"/emby/Users/1/Items/"+viewID+"/Children?IncludeItemTypes=Series,Season,Episode&api_key="+auth.AccessToken, nil))
	if libraryChildrenRecorder.Code != http.StatusOK || !strings.Contains(libraryChildrenRecorder.Body.String(), `"Type":"Series"`) {
		t.Fatalf("user library children status = %d body = %s", libraryChildrenRecorder.Code, libraryChildrenRecorder.Body.String())
	}
	libraryDetailRecorder := httptest.NewRecorder()
	server.ServeHTTP(libraryDetailRecorder, httptest.NewRequest(http.MethodGet,
		"/emby/Users/1/Items/"+viewID+"?api_key="+auth.AccessToken, nil))
	if libraryDetailRecorder.Code != http.StatusOK || !strings.Contains(libraryDetailRecorder.Body.String(), `"Type":"CollectionFolder"`) {
		t.Fatalf("user library detail status = %d body = %s", libraryDetailRecorder.Code, libraryDetailRecorder.Body.String())
	}
	libraryImageRecorder := httptest.NewRecorder()
	server.ServeHTTP(libraryImageRecorder, httptest.NewRequest(http.MethodGet,
		"/emby/Users/1/Items/"+viewID+"/Images/Primary?api_key="+auth.AccessToken, nil))
	if libraryImageRecorder.Code != http.StatusOK || libraryImageRecorder.Body.Len() == 0 {
		t.Fatalf("user library image status = %d bytes = %d", libraryImageRecorder.Code, libraryImageRecorder.Body.Len())
	}
	publicLibraryDetailRecorder := httptest.NewRecorder()
	server.ServeHTTP(publicLibraryDetailRecorder, httptest.NewRequest(http.MethodGet,
		"/Items/"+viewID+"?api_key="+auth.AccessToken, nil))
	if publicLibraryDetailRecorder.Code != http.StatusOK || !strings.Contains(publicLibraryDetailRecorder.Body.String(), `"Type":"CollectionFolder"`) {
		t.Fatalf("public library detail status = %d body = %s", publicLibraryDetailRecorder.Code, publicLibraryDetailRecorder.Body.String())
	}

	childrenRecorder := httptest.NewRecorder()
	server.ServeHTTP(childrenRecorder, httptest.NewRequest(http.MethodGet,
		"/emby/Users/1/Items/"+strconv.FormatInt(series.ID, 10)+"/Children?IncludeItemTypes=Season,Episode&api_key="+auth.AccessToken, nil))
	if childrenRecorder.Code != http.StatusOK || !strings.Contains(childrenRecorder.Body.String(), `"Type":"Season"`) {
		t.Fatalf("user item children status = %d body = %s", childrenRecorder.Code, childrenRecorder.Body.String())
	}
	publicChildrenRecorder := httptest.NewRecorder()
	server.ServeHTTP(publicChildrenRecorder, httptest.NewRequest(http.MethodGet,
		"/emby/Items/"+strconv.FormatInt(series.ID, 10)+"/Children?IncludeItemTypes=Season,Episode&api_key="+auth.AccessToken, nil))
	if publicChildrenRecorder.Code != http.StatusOK || !strings.Contains(publicChildrenRecorder.Body.String(), `"Type":"Season"`) {
		t.Fatalf("public item children status = %d body = %s", publicChildrenRecorder.Code, publicChildrenRecorder.Body.String())
	}

	resumeRecorder := httptest.NewRecorder()
	server.ServeHTTP(resumeRecorder, httptest.NewRequest(http.MethodGet,
		"/emby/Users/1/Items/Resume?IncludeItemTypes=Episode&api_key="+auth.AccessToken, nil))
	var resume struct {
		Items []struct {
			Id                 string            `json:"Id"`
			ParentId           string            `json:"ParentId"`
			SeriesId           string            `json:"SeriesId"`
			SeriesName         string            `json:"SeriesName"`
			SeasonId           string            `json:"SeasonId"`
			SeasonName         string            `json:"SeasonName"`
			Path               string            `json:"Path"`
			MediaSourceCount   int               `json:"MediaSourceCount"`
			ImageTags          map[string]string `json:"ImageTags"`
			PrimaryImageItemId string            `json:"PrimaryImageItemId"`
			PrimaryImageTag    string            `json:"PrimaryImageTag"`
		} `json:"Items"`
	}
	if err := json.Unmarshal(resumeRecorder.Body.Bytes(), &resume); err != nil {
		t.Fatal(err)
	}
	if resumeRecorder.Code != http.StatusOK || len(resume.Items) != 1 ||
		resume.Items[0].Id != strconv.FormatInt(episode.ID, 10) ||
		resume.Items[0].ParentId != strconv.FormatInt(season.ID, 10) ||
		resume.Items[0].SeriesId != strconv.FormatInt(series.ID, 10) ||
		resume.Items[0].SeriesName != series.Name ||
		resume.Items[0].SeasonId != strconv.FormatInt(season.ID, 10) ||
		resume.Items[0].SeasonName != season.Name ||
		resume.Items[0].Path != episode.MediaPath ||
		resume.Items[0].MediaSourceCount != 1 ||
		resume.Items[0].ImageTags["Primary"] != cover.SHA256 ||
		resume.Items[0].PrimaryImageItemId != strconv.FormatInt(series.ID, 10) ||
		resume.Items[0].PrimaryImageTag != cover.SHA256 {
		t.Fatalf("resume items = %#v body = %s", resume.Items, resumeRecorder.Body.String())
	}
	resumeImage := httptest.NewRecorder()
	server.ServeHTTP(resumeImage, httptest.NewRequest(http.MethodGet,
		"/Items/"+resume.Items[0].PrimaryImageItemId+"/Images/Primary?tag="+resume.Items[0].PrimaryImageTag, nil))
	if resumeImage.Code != http.StatusOK || resumeImage.Body.Len() == 0 {
		t.Fatalf("resume image status = %d bytes = %d", resumeImage.Code, resumeImage.Body.Len())
	}
	backdropFallback := httptest.NewRecorder()
	server.ServeHTTP(backdropFallback, httptest.NewRequest(http.MethodGet,
		"/Items/"+strconv.FormatInt(episode.ID, 10)+"/Images/Backdrop?maxWidth=1280&quality=70", nil))
	if backdropFallback.Code != http.StatusOK || backdropFallback.Header().Get("Content-Type") != "image/jpeg" || backdropFallback.Body.Len() == 0 {
		t.Fatalf("episode backdrop fallback status = %d type = %q bytes = %d", backdropFallback.Code, backdropFallback.Header().Get("Content-Type"), backdropFallback.Body.Len())
	}
	primaryFallback := httptest.NewRecorder()
	server.ServeHTTP(primaryFallback, httptest.NewRequest(http.MethodGet,
		"/Items/"+strconv.FormatInt(episode.ID, 10)+"/Images/Primary", nil))
	if primaryFallback.Code != http.StatusOK || primaryFallback.Header().Get("Content-Type") != "image/png" || primaryFallback.Body.Len() == 0 {
		t.Fatalf("episode primary fallback status = %d type = %q bytes = %d", primaryFallback.Code, primaryFallback.Header().Get("Content-Type"), primaryFallback.Body.Len())
	}
	userResumeImage := httptest.NewRecorder()
	server.ServeHTTP(userResumeImage, httptest.NewRequest(http.MethodGet,
		"/emby/Users/1/Items/"+resume.Items[0].PrimaryImageItemId+"/Images/Primary?tag="+resume.Items[0].PrimaryImageTag+"&api_key="+auth.AccessToken, nil))
	if userResumeImage.Code != http.StatusOK || userResumeImage.Body.Len() == 0 {
		t.Fatalf("user resume image status = %d bytes = %d", userResumeImage.Code, userResumeImage.Body.Len())
	}

	playbackRecorder := httptest.NewRecorder()
	server.ServeHTTP(playbackRecorder, httptest.NewRequest(http.MethodGet,
		"/emby/Users/1/Items/"+strconv.FormatInt(episode.ID, 10)+"/PlaybackInfo?api_key="+auth.AccessToken, nil))
	var playbackInfo struct {
		MediaSources  []json.RawMessage `json:"MediaSources"`
		PlaySessionId string            `json:"PlaySessionId"`
	}
	if err := json.Unmarshal(playbackRecorder.Body.Bytes(), &playbackInfo); err != nil {
		t.Fatal(err)
	}
	if playbackRecorder.Code != http.StatusOK || playbackInfo.PlaySessionId == "" || len(playbackInfo.MediaSources) != 1 {
		t.Fatalf("user playback info status = %d body = %s", playbackRecorder.Code, playbackRecorder.Body.String())
	}

	playbackPostRecorder := httptest.NewRecorder()
	server.ServeHTTP(playbackPostRecorder, httptest.NewRequest(http.MethodPost,
		"/emby/Users/1/Items/"+strconv.FormatInt(episode.ID, 10)+"/PlaybackInfo?api_key="+auth.AccessToken, nil))
	if playbackPostRecorder.Code != http.StatusOK || !strings.Contains(playbackPostRecorder.Body.String(), `"PlaySessionId"`) {
		t.Fatalf("user playback info post status = %d body = %s", playbackPostRecorder.Code, playbackPostRecorder.Body.String())
	}
}

func TestEmbyRootItemsReturnLibraryViewsInsteadOfTopLevelSeries(t *testing.T) {
	var media *medialibrary.Service
	server, store, _, _, _ := newEmbyTestServer(t, func(service *medialibrary.Service) { media = service })
	tvLibrary, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "TV", ContentType: medialibrary.ContentTVShows, RootPath: "/TV",
	})
	if err != nil {
		t.Fatal(err)
	}
	generation, err := store.BeginLibraryScan(t.Context(), tvLibrary.ID)
	if err != nil {
		t.Fatal(err)
	}
	series, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: tvLibrary.ID, Kind: medialibrary.KindSeries, Path: "/TV/Severance",
		Name: "Severance", SortName: "Severance", Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishLibraryScan(t.Context(), tvLibrary.ID, generation); err != nil {
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

	auth := authenticateEmby(t, server)
	tvCoverTag := ""
	for _, endpoint := range []string{
		"/emby/Users/1/Items?api_key=" + auth.AccessToken,
		"/emby/Items?UserId=1&ParentId=0&api_key=" + auth.AccessToken,
	} {
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, endpoint, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("root browse status = %d body = %s", recorder.Code, recorder.Body.String())
		}
		var result struct {
			Items []struct {
				Id                 string            `json:"Id"`
				Type               string            `json:"Type"`
				CollectionType     string            `json:"CollectionType"`
				ImageTags          map[string]string `json:"ImageTags"`
				BackdropImageTags  []string          `json:"BackdropImageTags"`
				PrimaryImageItemId string            `json:"PrimaryImageItemId"`
				PrimaryImageTag    string            `json:"PrimaryImageTag"`
			} `json:"Items"`
			TotalRecordCount int `json:"TotalRecordCount"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.TotalRecordCount != 2 || len(result.Items) != 2 {
			t.Fatalf("root browse libraries = %#v body = %s", result, recorder.Body.String())
		}
		gotTypes := map[string]string{}
		for _, item := range result.Items {
			if item.Type != "CollectionFolder" || item.Id == strconv.FormatInt(series.ID, 10) {
				t.Fatalf("root browse item = %#v body = %s", item, recorder.Body.String())
			}
			gotTypes[item.CollectionType] = item.Type
			if item.CollectionType == "tvshows" && (item.PrimaryImageItemId != strconv.FormatInt(8_000_000_000_000_000_000+tvLibrary.ID, 10) ||
				item.PrimaryImageTag == "" || item.PrimaryImageTag == cover.SHA256 || item.ImageTags["Primary"] != item.PrimaryImageTag ||
				len(item.BackdropImageTags) != 1 || item.BackdropImageTags[0] != item.PrimaryImageTag) {
				t.Fatalf("TV library cover = %#v body = %s", item, recorder.Body.String())
			}
			if item.CollectionType == "tvshows" {
				tvCoverTag = item.PrimaryImageTag
			}
		}
		if gotTypes["tvshows"] != "CollectionFolder" || gotTypes["movies"] != "CollectionFolder" {
			t.Fatalf("root browse collection types = %#v body = %s", gotTypes, recorder.Body.String())
		}
	}
	viewID := strconv.FormatInt(8_000_000_000_000_000_000+tvLibrary.ID, 10)
	publicCover := httptest.NewRecorder()
	server.ServeHTTP(publicCover, httptest.NewRequest(http.MethodGet,
		"/Items/"+viewID+"/Images/Primary?tag="+tvCoverTag, nil))
	if publicCover.Code != http.StatusOK || publicCover.Header().Get("Content-Type") != "image/jpeg" || publicCover.Body.Len() == 0 {
		t.Fatalf("library inherited cover status = %d type = %q bytes = %d", publicCover.Code, publicCover.Header().Get("Content-Type"), publicCover.Body.Len())
	}

	children := httptest.NewRecorder()
	server.ServeHTTP(children, httptest.NewRequest(http.MethodGet,
		"/emby/Users/1/Items?ParentId="+viewID+"&api_key="+auth.AccessToken, nil))
	if children.Code != http.StatusOK || !strings.Contains(children.Body.String(), `"Type":"Series"`) {
		t.Fatalf("library children status = %d body = %s", children.Code, children.Body.String())
	}
}

func TestEmbyRoutesDisappearWhenMediaLibraryIsDisabled(t *testing.T) {
	server, store, _, _, _ := newEmbyTestServer(t)
	if err := store.SetMediaLibraryConfiguration(t.Context(), false, domain.DefaultEmbyPort); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/emby/System/Info/Public", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("disabled status = %d body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestWrapRoutesEmbyPathsAndDelegatesOtherRequests(t *testing.T) {
	server, _, _, _, _ := newEmbyTestServer(t)
	delegated := false
	handler := server.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		delegated = true
		w.WriteHeader(http.StatusNoContent)
	}))

	managerResponse := httptest.NewRecorder()
	handler.ServeHTTP(managerResponse, httptest.NewRequest(http.MethodGet, "/api/manager/status", nil))
	if !delegated || managerResponse.Code != http.StatusNoContent {
		t.Fatalf("manager request delegated = %v status = %d", delegated, managerResponse.Code)
	}

	delegated = false
	embyResponse := httptest.NewRecorder()
	handler.ServeHTTP(embyResponse, httptest.NewRequest(http.MethodGet, "/emby/System/Info/Public", nil))
	if delegated || embyResponse.Code != http.StatusOK {
		t.Fatalf("Emby request delegated = %v status = %d body = %s", delegated, embyResponse.Code, embyResponse.Body.String())
	}
}

func TestEmbyPersonWorksPreservesEpisodeTypes(t *testing.T) {
	server, store, _, movie, metadata := newEmbyTestServer(t)
	metadata.creditsFn = func(context.Context, string, domain.MediaCreditsQuery, string) ([]domain.MediaPerson, error) {
		t.Fatal("person works must not fetch remote credits")
		return nil, nil
	}
	add := func(kind medialibrary.ItemKind, name string, parent *int64) medialibrary.Item {
		t.Helper()
		item, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{LibraryID: movie.LibraryID, Kind: kind, ParentID: parent, Path: "/TV/" + name, Name: name, SortName: name, SeasonNumber: new(1), EpisodeNumber: new(2), ScanGeneration: 1})
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	series := add(medialibrary.KindSeries, "Guest show", nil)
	season := add(medialibrary.KindSeason, "Season", &series.ID)
	episode := add(medialibrary.KindEpisode, "Guest episode", &season.ID)
	people := []medialibrary.LocalPerson{{Person: domain.MediaPerson{ID: 99, Name: "Guest", Type: "Actor"}}}
	if err := store.ReplaceMediaItemPeople(t.Context(), episode.ID, people); err != nil {
		t.Fatal(err)
	}
	auth := authenticateEmby(t, server)
	assert := func(kind string, wantID int64, wantType string) {
		t.Helper()
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/emby/Users/1/Items?PersonIds=99&Recursive=true&IncludeItemTypes="+kind+"&api_key="+auth.AccessToken, nil))
		var response struct {
			Items []struct {
				ID          string `json:"Id"`
				Type        string `json:"Type"`
				IndexNumber int    `json:"IndexNumber"`
			} `json:"Items"`
			Total int `json:"TotalRecordCount"`
		}
		if recorder.Code != http.StatusOK || json.Unmarshal(recorder.Body.Bytes(), &response) != nil {
			t.Fatalf("response: %d %s", recorder.Code, recorder.Body.String())
		}
		if wantID == 0 {
			if response.Total != 0 || len(response.Items) != 0 {
				t.Fatalf("expected empty: %s", recorder.Body.String())
			}
			return
		}
		if response.Total != 1 || len(response.Items) != 1 || response.Items[0].ID != strconv.FormatInt(wantID, 10) || response.Items[0].Type != wantType {
			t.Fatalf("unexpected works: %s", recorder.Body.String())
		}
		if wantType == "Episode" && response.Items[0].IndexNumber != 2 {
			t.Fatalf("missing episode number: %s", recorder.Body.String())
		}
	}
	assert("Series", 0, "")
	assert("Episode", episode.ID, "Episode")
	assert("Movie,Series,Episode", episode.ID, "Episode")
	if err := store.ReplaceMediaItemPeople(t.Context(), season.ID, people); err != nil {
		t.Fatal(err)
	}
	assert("Episode", 0, "")
	assert("Series", series.ID, "Series")
	assert("Movie,Series,Episode", series.ID, "Series")
}
