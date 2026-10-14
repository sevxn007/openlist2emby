package emby

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
)

func (s *Server) handlePlaybackInfo(w http.ResponseWriter, r *http.Request, id int64, token string, viewer viewer) {
	item, err := s.media.GetItemForUser(r.Context(), viewer.mediaAccess(), id)
	if err != nil || item.Kind.Folder() {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "playable media item not found")
		return
	}
	playSessionID, err := newAccessToken()
	if err != nil {
		writeEmbyError(w, http.StatusInternalServerError, "ServerError", "could not create playback session")
		return
	}
	writeEmbyJSON(w, http.StatusOK, playbackInfoResponse{
		MediaSources: []mediaSourceInfo{s.mediaSource(item, token)}, PlaySessionId: playSessionID,
	})
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request, path []string, viewer viewer) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeEmbyError(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "method not allowed")
		return
	}
	if !strings.HasPrefix(path[2], "stream") {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "stream not found")
		return
	}
	id, err := parsePositiveID(path[1])
	if err != nil {
		writeEmbyError(w, http.StatusBadRequest, "InvalidRequest", "invalid media item ID")
		return
	}
	s.redirectItem(w, r, id, viewer)
}

func (s *Server) redirectItem(w http.ResponseWriter, r *http.Request, id int64, viewer viewer) {
	target, err := s.media.ResolveDownloadURLForUser(r.Context(), viewer.mediaAccess(), id, r.UserAgent())
	if err != nil {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "media stream is unavailable")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, target, http.StatusTemporaryRedirect)
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request, path []string, auth clientAuthorization, viewer viewer) {
	if pathEquals(path, "sessions", "capabilities", "full") && r.Method == http.MethodPost {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if pathEquals(path, "sessions", "logout") && r.Method == http.MethodPost {
		identity, found, err := s.tokens.GetEmbyTokenIdentity(r.Context(), tokenHash(auth.Token))
		if err == nil && found {
			if err := s.credentials.Logout(r.Context(), identity.IdentitySession); err != nil {
				writeEmbyError(w, 500, "ServerError", "could not revoke identity session")
				return
			}
		}
		if err := s.tokens.RevokeEmbyToken(r.Context(), tokenHash(auth.Token)); err != nil {
			writeEmbyError(w, http.StatusInternalServerError, "ServerError", "could not revoke access token")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if len(path) == 2 && path[0] == "sessions" && path[1] == "playing" && r.Method == http.MethodPost {
		s.updatePlayback(w, r, false, viewer)
		return
	}
	if len(path) == 3 && path[0] == "sessions" && path[1] == "playing" && r.Method == http.MethodPost {
		switch path[2] {
		case "progress":
			s.updatePlayback(w, r, false, viewer)
		case "stopped":
			s.updatePlayback(w, r, true, viewer)
		case "ping":
			w.WriteHeader(http.StatusNoContent)
		default:
			writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "session endpoint not found")
		}
		return
	}
	writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "session endpoint not found")
}

func (s *Server) updatePlayback(w http.ResponseWriter, r *http.Request, stopped bool, viewer viewer) {
	var request playbackProgressRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&request); err != nil {
		writeEmbyError(w, http.StatusBadRequest, "InvalidRequest", "invalid playback update")
		return
	}
	if request.ItemId == "" {
		request.ItemId = queryValueFold(r, "ItemId")
	}
	id, err := parsePositiveID(request.ItemId)
	if err != nil {
		writeEmbyError(w, http.StatusBadRequest, "InvalidRequest", "invalid media item ID")
		return
	}
	position := request.PositionTicks
	if position < 0 {
		position = 0
	}
	if _, err := s.media.UpdatePlaybackForUser(r.Context(), viewer.mediaAccess(), id, medialibrary.PlaybackUpdate{
		PositionTicks: position,
		Stopped:       stopped,
	}); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "media item not found")
			return
		}
		writeEmbyError(w, http.StatusInternalServerError, "ServerError", "could not update playback progress")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
