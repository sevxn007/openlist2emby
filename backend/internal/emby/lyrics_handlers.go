package emby

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
)

type lyricDTO struct {
	Metadata lyricMetadata `json:"Metadata"`
	Lyrics   []lyricLine   `json:"Lyrics"`
}

type lyricMetadata struct {
	Artist   string `json:"Artist,omitempty"`
	Album    string `json:"Album,omitempty"`
	Title    string `json:"Title,omitempty"`
	Length   int64  `json:"Length,omitempty"`
	IsSynced bool   `json:"IsSynced"`
}

type lyricLine struct {
	Text  string `json:"Text"`
	Start *int64 `json:"Start,omitempty"`
}

func (s *Server) handleAudioLyrics(w http.ResponseWriter, r *http.Request, path []string, viewer viewer) {
	id, err := parsePositiveID(path[1])
	if err != nil {
		writeEmbyError(w, http.StatusBadRequest, "InvalidRequest", "invalid audio ID")
		return
	}
	item, err := s.media.GetItemForUser(r.Context(), viewer.mediaAccess(), id)
	if err != nil || item.Kind != medialibrary.KindAudio {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "audio item not found")
		return
	}
	lyrics := strings.TrimSpace(item.Lyrics)
	if lyrics == "" {
		lyrics = strings.TrimSpace(item.TranslatedLyrics)
	}
	if lyrics == "" {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "lyrics not found")
		return
	}
	lines, synced := parseLyrics(lyrics)
	writeEmbyJSON(w, http.StatusOK, lyricDTO{
		Metadata: lyricMetadata{
			Artist: firstMusicArtistName(item), Album: item.Album, Title: item.Name,
			Length: item.RuntimeTicks, IsSynced: synced,
		},
		Lyrics: lines,
	})
}

func firstMusicArtistName(item medialibrary.Item) string {
	if len(item.Artists) > 0 {
		return item.Artists[0]
	}
	return item.AlbumArtist
}

func parseLyrics(value string) ([]lyricLine, bool) {
	lines := make([]lyricLine, 0)
	synced := false
	for _, raw := range strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n") {
		text := strings.TrimSpace(raw)
		if text == "" {
			continue
		}
		starts := make([]int64, 0, 1)
		for strings.HasPrefix(text, "[") {
			end := strings.IndexByte(text, ']')
			if end < 0 {
				break
			}
			tag := text[1:end]
			if ticks, ok := lyricTimestampTicks(tag); ok {
				starts = append(starts, ticks)
			}
			text = strings.TrimSpace(text[end+1:])
		}
		if text == "" {
			continue
		}
		if len(starts) == 0 {
			lines = append(lines, lyricLine{Text: text})
			continue
		}
		synced = true
		for _, start := range starts {
			start := start
			lines = append(lines, lyricLine{Text: text, Start: &start})
		}
	}
	return lines, synced
}

func lyricTimestampTicks(value string) (int64, bool) {
	minutesText, secondsText, found := strings.Cut(strings.TrimSpace(value), ":")
	if !found {
		return 0, false
	}
	minutes, err := strconv.Atoi(minutesText)
	if err != nil || minutes < 0 {
		return 0, false
	}
	seconds, err := strconv.ParseFloat(secondsText, 64)
	if err != nil || seconds < 0 || seconds >= 60 {
		return 0, false
	}
	duration := time.Duration(float64(time.Minute)*float64(minutes) + float64(time.Second)*seconds)
	return duration.Nanoseconds() / 100, true
}
