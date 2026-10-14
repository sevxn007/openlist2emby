package httpapi

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
	"github.com/sevxn007/openlist2emby/backend/internal/testidentity"
)

type captureOpenList struct {
	resolvedPath string
}

func (*captureOpenList) ListDirectory(context.Context, string, bool) ([]medialibrary.RemoteEntry, error) {
	return nil, nil
}

func (c *captureOpenList) ResolveDownloadURL(_ context.Context, objectPath, _ string) (string, error) {
	c.resolvedPath = objectPath
	return "https://signed.example/video", nil
}

type captureFrameExtractor struct {
	frame   []byte
	percent int
}

func (*captureFrameExtractor) Available() error { return nil }

func (f *captureFrameExtractor) Capture(_ context.Context, _ string, percent int) ([]byte, error) {
	f.percent = percent
	return f.frame, nil
}

func TestLibraryEditorCannotCaptureMediaLibraryItemImage(t *testing.T) {
	server, store, _, _ := newAuthenticatedMediaServer(t, "")
	if err := store.SetMediaLibraryConfiguration(t.Context(), true, domain.DefaultEmbyPort); err != nil {
		t.Fatal(err)
	}
	percent := 30
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Movies", ContentType: medialibrary.ContentMovies, RootPaths: []string{"/Movies"}, VideoScreenshotPercent: &percent,
	})
	if err != nil {
		t.Fatal(err)
	}
	item, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, Kind: medialibrary.KindMovie, Path: "/Movies/Film.mkv",
		MediaPath: "/Movies/Film.mkv", Name: "Film", SortName: "Film", ScanGeneration: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	editor, err := store.CreateUser(t.Context(), domain.LocalUser{OpenListUserID: 101, InstanceID: "test-instance",
		Username: "editor", Role: domain.LocalUserRoleMedia, Enabled: true,
		LibraryGrants: []domain.LibraryGrant{{LibraryID: library.ID, Access: domain.LibraryAccessEdit}},
	})
	if err != nil {
		t.Fatal(err)
	}
	editorSession, err := testidentity.Session(t.Context(), store, editor.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: sessionCookieName, Value: editorSession.Token}
	assets, err := medialibrary.NewAssetFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	openList := &captureOpenList{}
	frames := &captureFrameExtractor{frame: captureTestJPEG(t)}
	service := medialibrary.NewService(store, func(string, string) medialibrary.OpenList { return openList }, nil)
	service.SetAssetFiles(assets)
	service.SetVideoFrameExtractor(frames)
	server.mediaLibraries = service

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/manager/media-library/items/"+strconv.FormatInt(item.ID, 10)+"/images/primary/capture", nil)
	request.AddCookie(cookie)
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("capture status = %d", recorder.Code)
	}
	if openList.resolvedPath != "" {
		t.Fatal("unauthorized capture resolved video")
	}

}

func TestCaptureMediaLibraryItemImageReportsUnavailableDependency(t *testing.T) {
	server, store, cookie, _ := newAuthenticatedMediaServer(t, "")
	if err := store.SetMediaLibraryConfiguration(t.Context(), true, domain.DefaultEmbyPort); err != nil {
		t.Fatal(err)
	}
	service := medialibrary.NewService(store, nil, nil)
	server.mediaLibraries = service

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/manager/media-library/items/1/images/primary/capture", nil)
	request.AddCookie(cookie)
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable || !bytes.Contains(recorder.Body.Bytes(), []byte("ffmpeg and ffprobe")) {
		t.Fatalf("unavailable status = %d body = %s", recorder.Code, recorder.Body.String())
	}
}

func captureTestJPEG(t *testing.T) []byte {
	t.Helper()
	frame := image.NewRGBA(image.Rect(0, 0, 8, 4))
	for y := range 4 {
		for x := range 8 {
			frame.Set(x, y, color.RGBA{R: 30, G: uint8(40 + x), B: uint8(50 + y), A: 255})
		}
	}
	var output bytes.Buffer
	if err := jpeg.Encode(&output, frame, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
