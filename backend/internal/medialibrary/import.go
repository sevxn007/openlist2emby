package medialibrary

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

// normalizeImportPaths validates the library boundary and removes overlapping
// selections so a file is indexed at most once, relative to its original root.
func normalizeImportPaths(library Library, paths []string) ([]string, error) {
	if len(paths) > 1000 {
		return nil, fmt.Errorf("%w: at most 1000 import paths are allowed", ErrInvalidLibrary)
	}
	normalized := make([]string, 0, len(paths))
	for _, value := range paths {
		if !strings.HasPrefix(value, "/") || strings.ContainsRune(value, '\x00') {
			return nil, fmt.Errorf("%w: import paths must be absolute", ErrInvalidLibrary)
		}
		value = path.Clean(value)
		if importRoot(library, value) == "" {
			return nil, fmt.Errorf("%w: import paths must belong to the selected library", ErrInvalidLibrary)
		}
		normalized = append(normalized, value)
	}
	sort.Strings(normalized)
	result := make([]string, 0, len(normalized))
	for _, value := range normalized {
		covered := false
		for _, selected := range result {
			if pathWithinRoot(value, selected) {
				covered = true
				break
			}
		}
		if !covered {
			result = append(result, value)
		}
	}
	return result, nil
}

func pathWithinRoot(value, root string) bool {
	return value == root || strings.HasPrefix(value, strings.TrimSuffix(root, "/")+"/")
}

func importRoot(library Library, value string) string {
	roots := library.RootPaths
	if len(roots) == 0 && library.RootPath != "" {
		roots = []string{library.RootPath}
	}
	for _, root := range roots {
		if pathWithinRoot(value, root) {
			return root
		}
	}
	return ""
}

type importDirectoryReader struct {
	OpenList
	entries map[string][]RemoteEntry
	onList  func()
}

func (c *importDirectoryReader) ListDirectory(ctx context.Context, directory string, _ bool) ([]RemoteEntry, error) {
	if entries, found := c.entries[directory]; found {
		return entries, nil
	}
	entries, err := c.OpenList.ListDirectory(ctx, directory, true)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.Name == "" || entry.Name == "." || entry.Name == ".." || path.Base(entry.Name) != entry.Name {
			return nil, fmt.Errorf("invalid media directory entry")
		}
	}
	c.entries[directory] = entries
	c.onList()
	return entries, nil
}

// DiscoverPaths refreshes only selected directories or file parents. Returned
// paths retain their library-relative hierarchy for the existing indexers.
func DiscoverPaths(ctx context.Context, client OpenList, library Library, paths []string, reporter ScanReporter) ([]DiscoveredFile, ScanProgress, error) {
	paths, err := normalizeImportPaths(library, paths)
	if err != nil {
		return nil, ScanProgress{}, err
	}
	files := []DiscoveredFile{}
	progress := ScanProgress{}
	report := func() {
		if reporter != nil {
			reporter(progress)
		}
	}
	reader := &importDirectoryReader{OpenList: client, entries: map[string][]RemoteEntry{}, onList: func() {
		progress.Directories++
		report()
	}}
	for _, selected := range paths {
		if err := ctx.Err(); err != nil {
			return nil, progress, err
		}
		root := importRoot(library, selected)
		entry := RemoteEntry{IsDir: selected == root}
		if selected != root {
			entries, err := reader.ListDirectory(ctx, path.Dir(selected), true)
			if err != nil {
				return nil, progress, err
			}
			found := false
			for _, candidate := range entries {
				if candidate.Name == path.Base(selected) {
					entry, found = candidate, true
					break
				}
			}
			if !found {
				return nil, progress, domain.ErrNotFound
			}
		}
		selectedFiles := []DiscoveredFile{{Path: selected, Name: entry.Name, Size: entry.Size, ModifiedAt: entry.ModifiedAt, Thumbnail: entry.Thumbnail}}
		if entry.IsDir {
			var err error
			selectedFiles, _, err = Discover(ctx, reader, selected, func(current ScanProgress) {
				progress.Files = len(files) + current.Files
				report()
			})
			if err != nil {
				return nil, progress, err
			}
		} else if !IsSupportedFile(library.ContentType, entry.Name) {
			return nil, progress, fmt.Errorf("%w: selected file is not supported by this library", ErrInvalidLibrary)
		}
		for _, file := range selectedFiles {
			file.ScanRoot = root
			file.Relative = strings.TrimPrefix(strings.TrimPrefix(file.Path, root), "/")
			files = append(files, file)
		}
		progress.Files = len(files)
		report()
	}
	return files, progress, nil
}
