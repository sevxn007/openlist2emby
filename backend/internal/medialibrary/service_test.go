package medialibrary

import "testing"

func boolPointer(value bool) *bool { return &value }

func intPointer(value int) *int { return &value }

func TestNormalizeLibrarySupportsMultipleRootPaths(t *testing.T) {
	normalized, err := normalizeLibrary(LibraryInput{
		Name:        " Movies ",
		ContentType: ContentMovies,
		RootPaths:   []string{" /Movies/ ", "/Archive"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.Name != "Movies" || normalized.RootPath != "/Movies" {
		t.Fatalf("normalized library = %#v", normalized)
	}
	if len(normalized.RootPaths) != 2 || normalized.RootPaths[0] != "/Movies" || normalized.RootPaths[1] != "/Archive" {
		t.Fatalf("normalized roots = %#v", normalized.RootPaths)
	}
}

func TestNormalizeLibraryVideoScreenshotDefaultsAndValidation(t *testing.T) {
	normalized, err := normalizeLibrary(LibraryInput{
		Name: "Movies", ContentType: ContentMovies, RootPaths: []string{"/Movies"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.VideoScreenshotEnabled == nil || *normalized.VideoScreenshotEnabled || normalized.VideoScreenshotPercent == nil || *normalized.VideoScreenshotPercent != 10 {
		t.Fatalf("screenshot defaults = %#v", normalized)
	}

	existing := Library{VideoScreenshotEnabled: true, VideoScreenshotPercent: 35}
	preserved, err := normalizeLibrary(LibraryInput{
		Name: "Movies", ContentType: ContentMovies, RootPaths: []string{"/Movies"},
	}, &existing)
	if err != nil {
		t.Fatal(err)
	}
	if !*preserved.VideoScreenshotEnabled || *preserved.VideoScreenshotPercent != 35 {
		t.Fatalf("preserved screenshot settings = %#v", preserved)
	}

	music, err := normalizeLibrary(LibraryInput{
		Name: "Music", ContentType: ContentMusic, RootPaths: []string{"/Music"},
		VideoScreenshotEnabled: boolPointer(true), VideoScreenshotPercent: intPointer(20),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if *music.VideoScreenshotEnabled {
		t.Fatal("music library retained video screenshots")
	}

	for _, percent := range []int{0, 100} {
		_, err := normalizeLibrary(LibraryInput{
			Name: "Movies", ContentType: ContentMovies, RootPaths: []string{"/Movies"},
			VideoScreenshotPercent: intPointer(percent),
		}, nil)
		if err == nil {
			t.Fatalf("normalizeLibrary accepted screenshot percent %d", percent)
		}
	}
}

func TestNormalizeLibraryPlaybackCompletionPercentDefaultsAndValidation(t *testing.T) {
	input := LibraryInput{Name: "Movies", ContentType: ContentMovies, RootPaths: []string{"/Movies"}}
	normalized, err := normalizeLibrary(input, nil)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.PlaybackCompletionPercent == nil || *normalized.PlaybackCompletionPercent != DefaultPlaybackCompletionPercent {
		t.Fatalf("completion threshold default = %#v", normalized.PlaybackCompletionPercent)
	}

	existing := Library{VideoScreenshotPercent: 10, PlaybackCompletionPercent: 95}
	preserved, err := normalizeLibrary(input, &existing)
	if err != nil {
		t.Fatal(err)
	}
	if preserved.PlaybackCompletionPercent == nil || *preserved.PlaybackCompletionPercent != 95 {
		t.Fatalf("preserved completion threshold = %#v", preserved.PlaybackCompletionPercent)
	}

	for _, percent := range []int{0, 101} {
		_, err := normalizeLibrary(LibraryInput{
			Name: "Movies", ContentType: ContentMovies, RootPaths: []string{"/Movies"}, PlaybackCompletionPercent: &percent,
		}, nil)
		if err == nil {
			t.Fatalf("normalizeLibrary accepted completion threshold %d", percent)
		}
	}
}

func TestNormalizeLibraryKeepsLegacyRootPathInput(t *testing.T) {
	normalized, err := normalizeLibrary(LibraryInput{
		Name: "Movies", ContentType: ContentMovies, RootPath: "/Movies",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(normalized.RootPaths) != 1 || normalized.RootPaths[0] != "/Movies" {
		t.Fatalf("legacy roots = %#v", normalized.RootPaths)
	}
}

func TestNormalizeLibraryRejectsInvalidRootSets(t *testing.T) {
	tests := []struct {
		name  string
		input LibraryInput
	}{
		{name: "empty", input: LibraryInput{Name: "Movies", ContentType: ContentMovies}},
		{name: "duplicate", input: LibraryInput{Name: "Movies", ContentType: ContentMovies, RootPaths: []string{"/Movies", "/Movies/"}}},
		{name: "nested", input: LibraryInput{Name: "Movies", ContentType: ContentMovies, RootPaths: []string{"/Media", "/Media/Movies"}}},
		{name: "legacy mismatch", input: LibraryInput{Name: "Movies", ContentType: ContentMovies, RootPath: "/Old", RootPaths: []string{"/Movies", "/Archive"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := normalizeLibrary(test.input, nil); err == nil {
				t.Fatal("normalizeLibrary accepted invalid root set")
			}
		})
	}
}
