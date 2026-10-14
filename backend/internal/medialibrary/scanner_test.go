package medialibrary

import (
	"context"
	"reflect"
	"testing"
)

type scanOpenList struct {
	tree          map[string][]RemoteEntry
	refreshValues []bool
}

func (f *scanOpenList) ListDirectory(_ context.Context, path string, refresh bool) ([]RemoteEntry, error) {
	f.refreshValues = append(f.refreshValues, refresh)
	return f.tree[path], nil
}

func (*scanOpenList) ResolveDownloadURL(context.Context, string, string) (string, error) {
	return "", nil
}

func TestDiscoverReportsDirectoriesAndFilesWithoutRefreshingListings(t *testing.T) {
	client := &scanOpenList{tree: map[string][]RemoteEntry{
		"/Media":       {{Name: "Shows", IsDir: true}, {Name: "movie.mkv", Size: 10}},
		"/Media/Shows": {{Name: "show.s01e01.mkv", Size: 20}, {Name: "notes.txt", Size: 1}},
	}}
	progress := []ScanProgress{}
	files, completed, err := Discover(t.Context(), client, "/Media", func(value ScanProgress) {
		progress = append(progress, value)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 || completed.Directories != 2 || completed.Files != 3 {
		t.Fatalf("files = %#v progress = %#v", files, completed)
	}
	if files[0].ScanRoot != "/Media" {
		t.Fatalf("scan root = %q", files[0].ScanRoot)
	}
	if want := []ScanProgress{{Directories: 1, Files: 1}, {Directories: 2, Files: 3}}; !reflect.DeepEqual(progress, want) {
		t.Fatalf("progress = %#v, want %#v", progress, want)
	}
	if want := []bool{false, false}; !reflect.DeepEqual(client.refreshValues, want) {
		t.Fatalf("refresh values = %#v, want %#v", client.refreshValues, want)
	}
}

func TestDiscoverRootsAggregatesProgressAcrossFolders(t *testing.T) {
	client := &scanOpenList{tree: map[string][]RemoteEntry{
		"/Movies": {{Name: "Dune.mkv", Size: 10}},
		"/Shows":  {{Name: "Severance.s01e01.mkv", Size: 20}},
	}}
	var progress []ScanProgress
	files, completed, err := DiscoverRoots(t.Context(), client, []string{"/Movies", "/Shows"}, func(value ScanProgress) {
		progress = append(progress, value)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || completed != (ScanProgress{Directories: 2, Files: 2}) {
		t.Fatalf("files = %#v progress = %#v", files, completed)
	}
	if files[0].ScanRoot != "/Movies" || files[1].ScanRoot != "/Shows" {
		t.Fatalf("scan roots = %q, %q", files[0].ScanRoot, files[1].ScanRoot)
	}
	if want := []ScanProgress{{Directories: 1, Files: 1}, {Directories: 2, Files: 2}}; !reflect.DeepEqual(progress, want) {
		t.Fatalf("progress = %#v, want %#v", progress, want)
	}
}

func TestMediaFilenameParsers(t *testing.T) {
	movie := ParseTitle("Dune.2021.2160p.WEB-DL.mkv")
	if movie.Name != "Dune" || movie.Year == nil || *movie.Year != 2021 {
		t.Fatalf("movie = %#v", movie)
	}
	episode, ok := ParseEpisode("Severance.S02E03.1080p.mkv")
	if !ok || episode.Series != "Severance" || episode.Season != 2 || episode.Episode != 3 {
		t.Fatalf("episode = %#v ok = %v", episode, ok)
	}
	for folder, want := range map[string]string{
		"庆余年 第一季":             "庆余年",
		"庆余年-第2季":             "庆余年",
		"Severance Season 02": "Severance",
		"Severance.S03":       "Severance",
		"Season 04":           "Severance",
		"Severance (2022)":    "Severance (2022)",
	} {
		if got := ParseSeriesFolder(folder, "Severance"); got != want {
			t.Fatalf("series folder %q = %q, want %q", folder, got, want)
		}
	}
	if title := ParseEpisodeTitle("庆余年 -S01E01-范闲脱胎换骨.mkv"); title != "范闲脱胎换骨" {
		t.Fatalf("episode title = %q, want 范闲脱胎换骨", title)
	}
	if title := ParseEpisodeTitle("怪奇物语.S01E01.2016.2160p.WEB-DL.DV.H.265.DDP 5.1.mkv"); title != "" {
		t.Fatalf("technical episode title = %q, want empty for TMDB fallback", title)
	}
	track := ParseTrack("Artist/Album/01 - First Song.flac")
	if track.Artist != "Artist" || track.Album != "Album" || track.Name != "First Song" || track.TrackNumber == nil || *track.TrackNumber != 1 {
		t.Fatalf("track = %#v", track)
	}
	flat := ParseTrack("G.E.M.邓紫棋 - A.I.N.Y.flac")
	if flat.Artist != "G.E.M.邓紫棋" || flat.Album != "Singles" || flat.Name != "A.I.N.Y" || len(flat.Artists) != 1 {
		t.Fatalf("flat track = %#v", flat)
	}
	collaboration := ParseTrack("G.E.M.邓紫棋,Faouzia – Still Breathing (Acoustic).flac")
	if collaboration.Artist != "G.E.M.邓紫棋" || collaboration.Name != "Still Breathing (Acoustic)" || len(collaboration.Artists) != 2 || collaboration.Artists[1] != "Faouzia" {
		t.Fatalf("collaboration track = %#v", collaboration)
	}
	underscored := ParseTrack("杨宗纬,宝石Gem,王宇宙Leto_若月亮没来_Live版.flac")
	if underscored.Artist != "杨宗纬" || underscored.Name != "若月亮没来 Live版" || len(underscored.Artists) != 3 || underscored.Artists[2] != "王宇宙Leto" {
		t.Fatalf("underscored track = %#v", underscored)
	}
}

func TestYearSeasonEpisodeParsing(t *testing.T) {
	for _, test := range []struct {
		name            string
		season, episode int
	}{
		{"Tom and Jerry - S1940E01 - 第 1 集.mkv", 1940, 1},
		{"Tom and Jerry - S1950E68 - Tot Watchers.mkv", 1950, 68},
		{"Tom and Jerry - S1960E47 - Purr-Chance to Dream.mkv", 1960, 47},
		{"Show.9999x123.Title.mkv", 9999, 123},
		{"Show.S01E02.mkv", 1, 2},
	} {
		got, ok := ParseEpisode(test.name)
		if !ok || got.Season != test.season || got.Episode != test.episode {
			t.Fatalf("%s: %+v %t", test.name, got, ok)
		}
	}
	for _, name := range []string{"Show.S19400E01.mkv", "Show.S1940E1000.mkv", "Show.S1940E.mkv", "Show.19400x01.mkv"} {
		if got, ok := ParseEpisode(name); ok {
			t.Fatalf("invalid filename accepted: %s %+v", name, got)
		}
	}
	if got := ParseEpisodeTitle("Tom and Jerry - S1940E01 - 第 1 集.mkv"); got != "第 1 集" {
		t.Fatal(got)
	}
	if got := ParseSeriesFolder("Tom and Jerry Season 1940", "fallback"); got != "Tom and Jerry" {
		t.Fatal(got)
	}
}

func TestParseTitleDirectoryYears(t *testing.T) {
	for _, folder := range []string{"逍遥（2025）", "逍遥[2025]", "逍遥【2025】", "逍遥.2025", "逍遥 (2025)", "逍遥.2025.1080p.mkv"} {
		got := ParseTitle(folder)
		if got.Name != "逍遥" || got.Year == nil || *got.Year != 2025 {
			t.Fatalf("%q: %#v", folder, got)
		}
	}
}
