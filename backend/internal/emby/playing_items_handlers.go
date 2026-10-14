package emby

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
)

const maxPlayingItemRequestSize = 256 << 10

// handlePlayingItems implements Emby's legacy, user-scoped playback reporting
// endpoints. Newer clients normally use /Sessions/Playing, but some clients
// still send progress exclusively through these routes.
func (s *Server) handlePlayingItems(w http.ResponseWriter, r *http.Request, path []string, viewer viewer) {
	if len(path) < 4 || len(path) > 5 || path[0] != "users" || path[2] != "playingitems" {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "resource not found")
		return
	}
	if path[1] != viewer.idString() {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "resource not found")
		return
	}
	if len(path) == 5 && path[4] != "progress" && path[4] != "delete" {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "resource not found")
		return
	}

	stopped, ok := playingItemOperation(r.Method, path)
	if !ok {
		writeEmbyError(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "method not allowed")
		return
	}
	id, err := parsePositiveID(path[3])
	if err != nil {
		writeEmbyError(w, http.StatusBadRequest, "InvalidRequest", "invalid media item ID")
		return
	}
	item, err := s.media.GetItemForUser(r.Context(), viewer.mediaAccess(), id)
	if err != nil {
		writePlayingItemError(w, err)
		return
	}

	position, supplied, err := playingItemPosition(w, r)
	if err != nil {
		writeEmbyError(w, http.StatusBadRequest, "InvalidRequest", "invalid playback position")
		return
	}
	if !supplied {
		position = item.UserData.PlaybackPositionTicks
	}
	if position < 0 {
		position = 0
	}

	if _, err := s.media.UpdatePlaybackForUser(r.Context(), viewer.mediaAccess(), id, medialibrary.PlaybackUpdate{
		PositionTicks: position,
		Stopped:       stopped,
	}); err != nil {
		writePlayingItemError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func playingItemOperation(method string, path []string) (stopped bool, ok bool) {
	if len(path) == 4 {
		switch method {
		case http.MethodPost:
			return false, true
		case http.MethodDelete:
			return true, true
		default:
			return false, false
		}
	}
	if len(path) != 5 || method != http.MethodPost {
		return false, false
	}
	switch path[4] {
	case "progress":
		return false, true
	case "delete":
		return true, true
	default:
		return false, false
	}
}

func playingItemPosition(w http.ResponseWriter, r *http.Request) (int64, bool, error) {
	if raw := queryValueFold(r, "PositionTicks"); raw != "" {
		position, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		return position, true, err
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPlayingItemRequestSize))
	if err != nil {
		return 0, false, err
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return 0, false, nil
	}

	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil && strings.TrimSpace(r.Header.Get("Content-Type")) != "" {
		return 0, false, err
	}
	if mediaType == "application/x-www-form-urlencoded" {
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return 0, false, err
		}
		for key, entries := range values {
			if !strings.EqualFold(key, "PositionTicks") || len(entries) == 0 || strings.TrimSpace(entries[0]) == "" {
				continue
			}
			position, err := strconv.ParseInt(strings.TrimSpace(entries[0]), 10, 64)
			return position, true, err
		}
		return 0, false, nil
	}

	var request struct {
		PositionTicks *int64 `json:"PositionTicks"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&request); err != nil {
		return 0, false, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple playback request values")
		}
		return 0, false, err
	}
	if request.PositionTicks == nil {
		return 0, false, nil
	}
	return *request.PositionTicks, true, nil
}

func writePlayingItemError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound), errors.Is(err, medialibrary.ErrDisabled):
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "media item not found")
	case errors.Is(err, medialibrary.ErrInvalidLibrary):
		writeEmbyError(w, http.StatusBadRequest, "InvalidRequest", "invalid playback update")
	default:
		writeEmbyError(w, http.StatusInternalServerError, "ServerError", "could not update playback progress")
	}
}
