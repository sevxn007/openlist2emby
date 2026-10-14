package medialibrary

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/sevxn007/openlist2emby/backend/internal/metadata"
)

var (
	videoExtensions = map[string]struct{}{
		"avi": {}, "flv": {}, "m2ts": {}, "m4v": {}, "mkv": {}, "mov": {}, "mp4": {},
		"mpeg": {}, "mpg": {}, "mts": {}, "ts": {}, "webm": {}, "wmv": {},
	}
	audioExtensions = map[string]struct{}{
		"aac": {}, "aiff": {}, "alac": {}, "ape": {}, "flac": {}, "m4a": {}, "mp3": {},
		"ogg": {}, "opus": {}, "wav": {}, "wma": {},
	}
	episodePattern     = regexp.MustCompile(`(?i)^(.*?)[ ._\-]+s([0-9]{1,4})e([0-9]{1,3})(?:[^0-9].*)?$`)
	episodeAlt         = regexp.MustCompile(`(?i)^(.*?)[ ._\-]+([0-9]{1,4})x([0-9]{1,3})(?:[^0-9].*)?$`)
	episodeMarker      = regexp.MustCompile(`(?i)s[0-9]{1,4}e[0-9]{1,3}|[0-9]{1,4}x[0-9]{1,3}`)
	trackPattern       = regexp.MustCompile(`^(?:([0-9]{1,2})[-_.])?([0-9]{1,3})[ ._\-]+(.+)$`)
	noisePattern       = regexp.MustCompile(`(?i)\b(2160p|1080p|720p|480p|bluray|blu-ray|webrip|web-dl|hdtv|x264|x265|h264|h265|hevc|av1|aac|dts|remux)\b.*$`)
	episodeNoiseOnly   = regexp.MustCompile(`(?i)^(?:(?:19|20)[0-9]{2}[ ._-]+)?(?:2160p|1080p|720p|480p|4k|8k|bluray|blu-ray|webrip|web[- .]?dl|hdtv|x264|x265|h264|h265|hevc|av1|aac|dts|ddp|dd\+|remux)(?:[ ._-].*)?$`)
	seriesSeasonSuffix = regexp.MustCompile(`(?i)[ ._\-]*(?:season[ ._\-]*[0-9]{1,4}|s[0-9]{1,4}|第[零〇一二三四五六七八九十百两0-9]{1,4}季)[ ._\-]*$`)
)

type DiscoveredFile struct {
	Path       string
	Relative   string
	ScanRoot   string
	Name       string
	Size       int64
	ModifiedAt *time.Time
	Thumbnail  string
}

type ScanProgress struct {
	Directories int
	Files       int
}

type ScanReporter func(ScanProgress)

func Discover(ctx context.Context, client OpenList, root string, reporter ScanReporter) ([]DiscoveredFile, ScanProgress, error) {
	root = path.Clean(root)
	files := []DiscoveredFile{}
	progress := ScanProgress{}
	var walk func(string) error
	walk = func(directory string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, err := client.ListDirectory(ctx, directory, false)
		if err != nil {
			return fmt.Errorf("list media directory %q: %w", directory, err)
		}
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].IsDir != entries[j].IsDir {
				return entries[i].IsDir
			}
			return entries[i].Name < entries[j].Name
		})
		progress.Directories++
		for _, entry := range entries {
			if !entry.IsDir {
				progress.Files++
			}
		}
		if reporter != nil {
			reporter(progress)
		}
		for _, entry := range entries {
			objectPath := path.Join(directory, entry.Name)
			if entry.IsDir {
				if err := walk(objectPath); err != nil {
					return err
				}
				continue
			}
			relative := strings.TrimPrefix(strings.TrimPrefix(objectPath, root), "/")
			files = append(files, DiscoveredFile{
				Path: objectPath, Relative: relative, ScanRoot: root, Name: entry.Name, Size: entry.Size,
				ModifiedAt: entry.ModifiedAt, Thumbnail: entry.Thumbnail,
			})
		}
		return nil
	}
	if err := walk(root); err != nil {
		return nil, progress, err
	}
	return files, progress, nil
}

func DiscoverRoots(ctx context.Context, client OpenList, roots []string, reporter ScanReporter) ([]DiscoveredFile, ScanProgress, error) {
	files := []DiscoveredFile{}
	progress := ScanProgress{}
	for _, root := range roots {
		rootFiles, rootProgress, err := Discover(ctx, client, root, func(current ScanProgress) {
			if reporter == nil {
				return
			}
			reporter(ScanProgress{
				Directories: progress.Directories + current.Directories,
				Files:       progress.Files + current.Files,
			})
		})
		if err != nil {
			return nil, progress, err
		}
		files = append(files, rootFiles...)
		progress.Directories += rootProgress.Directories
		progress.Files += rootProgress.Files
	}
	return files, progress, nil
}

func IsSupportedFile(contentType ContentType, name string) bool {
	extension := strings.TrimPrefix(strings.ToLower(path.Ext(name)), ".")
	if contentType == ContentMusic {
		_, ok := audioExtensions[extension]
		return ok
	}
	_, ok := videoExtensions[extension]
	return ok
}

type ParsedTitle struct {
	Name string
	Year *int
}

func ParseTitle(filename string) ParsedTitle {
	name := path.Base(filename)
	extension := strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
	_, video := videoExtensions[extension]
	_, audio := audioExtensions[extension]
	if video || audio {
		name = strings.TrimSuffix(name, path.Ext(name))
	}
	name, year := metadata.ParseQuery(name)
	name = noisePattern.ReplaceAllString(name, "")
	name = humanize(name)
	if name == "" {
		name = strings.TrimSuffix(path.Base(filename), path.Ext(filename))
	}
	return ParsedTitle{Name: name, Year: year}
}

type ParsedEpisode struct {
	Series  string
	Season  int
	Episode int
}

func ParseEpisode(filename string) (ParsedEpisode, bool) {
	name := strings.TrimSuffix(path.Base(filename), path.Ext(filename))
	match := episodePattern.FindStringSubmatch(name)
	if len(match) == 0 {
		match = episodeAlt.FindStringSubmatch(name)
	}
	if len(match) == 0 {
		return ParsedEpisode{}, false
	}
	season, _ := strconv.Atoi(match[2])
	episode, _ := strconv.Atoi(match[3])
	return ParsedEpisode{Series: humanize(match[1]), Season: season, Episode: episode}, true
}

// ParseSeriesFolder removes an explicit season suffix from a top-level TV
// folder so seasons stored as sibling folders still share one series item.
func ParseSeriesFolder(folder, filenameSeries string) string {
	series := strings.TrimSpace(seriesSeasonSuffix.ReplaceAllString(folder, ""))
	if series == "" {
		return filenameSeries
	}
	return series
}

// ParseEpisodeTitle removes the series name and season/episode marker from an
// episode filename, leaving only the human-readable episode title.
func ParseEpisodeTitle(filename string) string {
	name := strings.TrimSuffix(path.Base(filename), path.Ext(filename))
	marker := episodeMarker.FindStringIndex(name)
	if marker == nil {
		return ParseTitle(filename).Name
	}
	title := strings.Trim(name[marker[1]:], " ._-\t")
	if title == "" {
		return ParseTitle(filename).Name
	}
	if episodeNoiseOnly.MatchString(title) {
		return ""
	}
	return ParseTitle(title).Name
}

type ParsedTrack struct {
	Name        string
	Artist      string
	Artists     []string
	Album       string
	TrackNumber *int
	DiscNumber  *int
}

func ParseTrack(relativePath string) ParsedTrack {
	segments := strings.Split(relativePath, "/")
	artist := "Unknown Artist"
	artists := []string{"Unknown Artist"}
	album := "Singles"
	if len(segments) >= 2 {
		artist = humanize(segments[0])
		artists = []string{artist}
	}
	if len(segments) >= 3 {
		album = humanize(segments[1])
	}
	base := strings.TrimSuffix(path.Base(relativePath), path.Ext(relativePath))
	nameSource := base
	var track, disc *int
	if match := trackPattern.FindStringSubmatch(base); len(match) > 0 {
		trackValue, _ := strconv.Atoi(match[2])
		track = &trackValue
		if match[1] != "" {
			discValue, _ := strconv.Atoi(match[1])
			disc = &discValue
		}
		nameSource = match[3]
	}
	if filenameArtist, title, found := parseTrackArtistTitle(nameSource); found {
		if len(segments) == 1 {
			artists = splitTrackArtists(filenameArtist)
			artist = artists[0]
		}
		if len(segments) == 1 || normalizeTrackArtistKey(filenameArtist) == normalizeTrackArtistKey(artist) {
			nameSource = title
		}
	}
	return ParsedTrack{Name: cleanTrackText(nameSource), Artist: artist, Artists: artists, Album: album, TrackNumber: track, DiscNumber: disc}
}

func parseTrackArtistTitle(value string) (string, string, bool) {
	for _, separator := range []string{" - ", " – ", " — ", "_"} {
		if artist, title, found := strings.Cut(value, separator); found {
			artist, title = cleanTrackText(artist), cleanTrackText(title)
			if artist != "" && title != "" {
				return artist, title, true
			}
		}
	}
	return "", "", false
}

func splitTrackArtists(value string) []string {
	parts := strings.FieldsFunc(value, func(character rune) bool {
		switch character {
		case ',', '，', '、', '&', '＆':
			return true
		default:
			return false
		}
	})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = cleanTrackText(part); part != "" {
			result = append(result, part)
		}
	}
	if len(result) == 0 {
		return []string{"Unknown Artist"}
	}
	return result
}

func cleanTrackText(value string) string {
	value = strings.ReplaceAll(value, "_", " ")
	return strings.Join(strings.Fields(value), " ")
}

func normalizeTrackArtistKey(value string) string {
	value = strings.ToLower(cleanTrackText(value))
	return strings.Map(func(character rune) rune {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			return character
		}
		return -1
	}, value)
}

func humanize(value string) string {
	value = strings.Map(func(r rune) rune {
		if r == '.' || r == '_' {
			return ' '
		}
		return r
	}, value)
	return strings.Join(strings.FieldsFunc(value, func(r rune) bool {
		return unicode.IsSpace(r)
	}), " ")
}
