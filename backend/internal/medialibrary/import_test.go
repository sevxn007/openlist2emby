package medialibrary

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

type importOpenList struct {
	tree    map[string][]RemoteEntry
	paths   []string
	refresh []bool
	fail    string
}

func (c *importOpenList) ListDirectory(_ context.Context, directory string, refresh bool) ([]RemoteEntry, error) {
	c.paths = append(c.paths, directory)
	c.refresh = append(c.refresh, refresh)
	if directory == c.fail {
		return nil, errors.New("synthetic upstream failure")
	}
	return c.tree[directory], nil
}

func (*importOpenList) ResolveDownloadURL(context.Context, string, string) (string, error) {
	return "", nil
}

func TestDiscoverPathsReadsOnlySelectedFilesAndDirectories(t *testing.T) {
	library := Library{RootPaths: []string{"/TV", "/Archive"}, ContentType: ContentTVShows}
	client := &importOpenList{tree: map[string][]RemoteEntry{
		"/TV/Show/Season 01":       {{Name: "Show.S01E01.mkv"}, {Name: "Show.S01E02.mkv"}, {Name: "Show.S01E03.mkv"}},
		"/Archive":                 {{Name: "Other", IsDir: true}, {Name: "Unselected", IsDir: true}},
		"/Archive/Other":           {{Name: "Season 02", IsDir: true}},
		"/Archive/Other/Season 02": {{Name: "Other.S02E01.mkv"}},
	}}
	files, progress, err := DiscoverPaths(t.Context(), client, library, []string{
		"/TV/Show/Season 01/Show.S01E02.mkv", "/TV/Show/Season 01/Show.S01E03.mkv",
		"/Archive/Other/Season 02/Other.S02E01.mkv", "/Archive/Other", "/Archive/Other",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 || progress != (ScanProgress{Directories: 4, Files: 3}) {
		t.Fatalf("files=%#v progress=%#v", files, progress)
	}
	if files[0].ScanRoot != "/Archive" || files[0].Relative != "Other/Season 02/Other.S02E01.mkv" || files[1].ScanRoot != "/TV" || files[1].Relative != "Show/Season 01/Show.S01E02.mkv" {
		t.Fatalf("library-relative hierarchy lost: %#v", files)
	}
	if !reflect.DeepEqual(client.paths, []string{"/Archive", "/Archive/Other", "/Archive/Other/Season 02", "/TV/Show/Season 01"}) {
		t.Fatalf("unexpected directories: %v", client.paths)
	}
	for _, refresh := range client.refresh {
		if !refresh {
			t.Fatal("import must refresh listings")
		}
	}
}

func TestDiscoverPathsRejectsInvalidSelectionsBeforeIndexing(t *testing.T) {
	library := Library{RootPath: "/TV", ContentType: ContentTVShows}
	for _, selected := range []string{"relative.mkv", "/TV-Other/Show.mkv", "/TV/../Private/file.mkv", ""} {
		t.Run(selected, func(t *testing.T) {
			client := &importOpenList{}
			_, _, err := DiscoverPaths(t.Context(), client, library, []string{selected}, nil)
			if !errors.Is(err, ErrInvalidLibrary) || len(client.paths) != 0 {
				t.Fatalf("error=%v paths=%v", err, client.paths)
			}
		})
	}
	for _, test := range []struct {
		name    string
		entries []RemoteEntry
		fail    string
		want    error
	}{
		{"missing", nil, "", domain.ErrNotFound},
		{"unsupported", []RemoteEntry{{Name: "notes.txt"}}, "", ErrInvalidLibrary},
		{"unsafe upstream name", []RemoteEntry{{Name: "../Private"}}, "", nil},
		{"upstream failure", nil, "/TV", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &importOpenList{tree: map[string][]RemoteEntry{"/TV": test.entries}, fail: test.fail}
			files, _, err := DiscoverPaths(t.Context(), client, library, []string{"/TV/notes.txt"}, nil)
			if err == nil || files != nil || test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("files=%v error=%v", files, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	client := &importOpenList{}
	if _, _, err := DiscoverPaths(ctx, client, library, []string{"/TV"}, nil); !errors.Is(err, context.Canceled) || len(client.paths) != 0 {
		t.Fatalf("canceled discovery: %v, %v", err, client.paths)
	}
}

func TestDiscoverPathsAcceptsLibraryRootWithoutReadingItsParent(t *testing.T) {
	client := &importOpenList{tree: map[string][]RemoteEntry{"/TV": {{Name: "Show.S01E01.mkv"}}}}
	files, _, err := DiscoverPaths(t.Context(), client, Library{RootPath: "/TV", ContentType: ContentTVShows}, []string{"/TV/"}, nil)
	if err != nil || len(files) != 1 || !reflect.DeepEqual(client.paths, []string{"/TV"}) {
		t.Fatalf("files=%v paths=%v error=%v", files, client.paths, err)
	}
}
