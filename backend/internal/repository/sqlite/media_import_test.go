package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
	"github.com/sevxn007/openlist2emby/backend/internal/task"
)

type importTestOpenList struct {
	tree  map[string][]medialibrary.RemoteEntry
	paths []string
}

func (c *importTestOpenList) ListDirectory(_ context.Context, directory string, _ bool) ([]medialibrary.RemoteEntry, error) {
	c.paths = append(c.paths, directory)
	return c.tree[directory], nil
}

func (*importTestOpenList) ResolveDownloadURL(context.Context, string, string) (string, error) {
	return "", nil
}

func TestMediaImportPreservesLibraryAndEpisodeIdentity(t *testing.T) {
	store := newMediaLibraryStore(t)
	if err := store.SaveInitialSettings(t.Context(), "https://openlist.example", "synthetic-admin-key"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetMediaLibraryConfiguration(t.Context(), true, domain.DefaultEmbyPort); err != nil {
		t.Fatal(err)
	}
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{Name: "Shows", ContentType: medialibrary.ContentTVShows, RootPaths: []string{"/TV", "/Archive"}})
	if err != nil {
		t.Fatal(err)
	}
	client := &importTestOpenList{tree: map[string][]medialibrary.RemoteEntry{
		"/TV":                {{Name: "Show", IsDir: true}},
		"/TV/Show":           {{Name: "Season 01", IsDir: true}},
		"/TV/Show/Season 01": {{Name: "Show.S01E01.mkv"}},
		"/Archive":           {{Name: "Other.S01E01.mkv"}},
	}}
	handler := medialibrary.NewTaskHandler(store, func(string, string) medialibrary.OpenList { return client }, nil)
	run := func(paths ...string) medialibrary.ScanResult {
		t.Helper()
		raw, _ := json.Marshal(medialibrary.ScanPayload{LibraryID: library.ID, Paths: paths})
		payload, err := handler.Prepare(t.Context(), raw)
		if err != nil {
			t.Fatal(err)
		}
		result, err := handler.Run(t.Context(), payload, task.Execution{})
		if err != nil {
			t.Fatal(err)
		}
		var summary medialibrary.ScanResult
		if err := json.Unmarshal(result, &summary); err != nil {
			t.Fatal(err)
		}
		return summary
	}
	if result := run("/TV/Show/Season 01/Show.S01E01.mkv"); result.Added != 3 || result.Removed != 0 {
		t.Fatalf("first import into empty library=%#v", result)
	}
	initialLibrary, _ := store.GetLibrary(t.Context(), library.ID)
	if initialLibrary.ScanGeneration != 0 || initialLibrary.LastScannedAt != nil {
		t.Fatal("initial import unexpectedly performed a full scan")
	}
	run()
	beforeLibrary, _ := store.GetLibrary(t.Context(), library.ID)
	before, err := store.ListMediaItems(t.Context(), medialibrary.ItemQuery{LibraryID: &library.ID, Limit: 100})
	if err != nil || before.Total != 6 {
		t.Fatalf("initial library=%#v error=%v", before, err)
	}
	var first medialibrary.Item
	for _, item := range before.Items {
		if item.Path == "/TV/Show/Season 01/Show.S01E01.mkv" {
			first = item
		}
	}
	if first.ID == 0 {
		t.Fatal("first episode missing")
	}
	if _, err := store.UpdateMediaUserData(t.Context(), first.ID, medialibrary.UserData{PlaybackPositionTicks: 12345, Favorite: true}); err != nil {
		t.Fatal(err)
	}
	client.tree["/TV/Show/Season 01"] = append(client.tree["/TV/Show/Season 01"], medialibrary.RemoteEntry{Name: "Show.S01E02.mkv"})
	client.paths = nil
	if result := run("/TV/Show/Season 01/Show.S01E02.mkv"); result.Added != 1 || result.Removed != 0 || result.Files != 1 {
		t.Fatalf("import result=%#v", result)
	}
	if !reflect.DeepEqual(client.paths, []string{"/TV/Show/Season 01"}) {
		t.Fatalf("import traversed unrelated paths: %v", client.paths)
	}
	if result := run("/TV/Show/Season 01/Show.S01E02.mkv"); result.Added != 0 || result.Removed != 0 {
		t.Fatalf("duplicate import=%#v", result)
	}
	after, err := store.ListMediaItems(t.Context(), medialibrary.ItemQuery{LibraryID: &library.ID, Limit: 100})
	if err != nil || after.Total != 7 {
		t.Fatalf("imported library=%#v error=%v", after, err)
	}
	for _, previous := range before.Items {
		current, err := store.GetMediaItem(t.Context(), previous.ID)
		if err != nil || current.Path != previous.Path {
			t.Fatalf("existing item changed: %#v %v", current, err)
		}
	}
	for _, item := range after.Items {
		if item.Path == "/TV/Show/Season 01/Show.S01E02.mkv" && (item.ParentID == nil || first.ParentID == nil || *item.ParentID != *first.ParentID) {
			t.Fatal("new episode did not reuse the existing season")
		}
	}
	preserved, _ := store.GetMediaItem(t.Context(), first.ID)
	if preserved.UserData.PlaybackPositionTicks != 12345 || !preserved.UserData.Favorite {
		t.Fatalf("playback state changed: %#v", preserved.UserData)
	}
	afterLibrary, _ := store.GetLibrary(t.Context(), library.ID)
	if afterLibrary.ScanGeneration != beforeLibrary.ScanGeneration || !afterLibrary.LastScannedAt.Equal(*beforeLibrary.LastScannedAt) {
		t.Fatal("import changed full-scan state")
	}
	// A later full reconciliation still owns removal of genuinely missing files.
	delete(client.tree, "/Archive")
	if result := run(); result.Removed != 3 {
		t.Fatalf("subsequent full scan=%#v", result)
	}
	if _, err := store.GetMediaItem(t.Context(), first.ID); err != nil {
		t.Fatal(err)
	}
	// Persisted tasks must be checked against current roots when executed.
	raw, _ := json.Marshal(medialibrary.ScanPayload{LibraryID: library.ID, Paths: []string{"/TV/Show"}})
	payload, err := handler.Prepare(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateLibrary(t.Context(), library.ID, medialibrary.LibraryInput{Name: "Shows", ContentType: medialibrary.ContentTVShows, RootPaths: []string{"/NewTV"}}); err != nil {
		t.Fatal(err)
	}
	client.paths = nil
	if _, err := handler.Run(t.Context(), payload, task.Execution{}); !errors.Is(err, medialibrary.ErrInvalidLibrary) || len(client.paths) != 0 {
		t.Fatalf("stale import: error=%v paths=%v", err, client.paths)
	}
}
