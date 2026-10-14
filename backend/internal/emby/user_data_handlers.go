package emby

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
)

type userDataUpdateRequest struct {
	PlaybackPositionTicks *int64 `json:"PlaybackPositionTicks"`
	Played                *bool  `json:"Played"`
	PlayCount             *int   `json:"PlayCount"`
	IsFavorite            *bool  `json:"IsFavorite"`
}

func (s *Server) handleUserItemData(w http.ResponseWriter, r *http.Request, path []string, viewer viewer) {
	if r.Method != http.MethodPost || len(path) != 5 || path[1] != viewer.idString() {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "resource not found")
		return
	}
	itemID, err := parsePositiveID(path[3])
	if err != nil {
		writeEmbyError(w, http.StatusBadRequest, "InvalidRequest", "invalid item ID")
		return
	}

	var request userDataUpdateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&request); err != nil {
		writeEmbyError(w, http.StatusBadRequest, "InvalidRequest", "invalid user data")
		return
	}
	if (request.PlaybackPositionTicks != nil && *request.PlaybackPositionTicks < 0) ||
		(request.PlayCount != nil && *request.PlayCount < 0) {
		writeEmbyError(w, http.StatusBadRequest, "InvalidRequest", "playback position and play count must not be negative")
		return
	}

	targetID := itemID
	if request.Played != nil {
		targetID, err = s.playedItemTargetID(r.Context(), viewer.mediaAccess(), itemID, queryValueFold(r, "SeasonId"))
		if err != nil {
			writeUserDataUpdateError(w, err)
			return
		}
	}
	item, err := s.media.GetItemForUser(r.Context(), viewer.mediaAccess(), targetID)
	if err != nil {
		writeUserDataUpdateError(w, err)
		return
	}
	data := item.UserData
	if request.Played != nil {
		data, err = s.media.SetPlayedForUser(r.Context(), viewer.mediaAccess(), targetID, *request.Played)
		if err != nil {
			writeUserDataUpdateError(w, err)
			return
		}
	}
	if request.PlaybackPositionTicks != nil {
		data.PlaybackPositionTicks = *request.PlaybackPositionTicks
	}
	if request.PlayCount != nil {
		data.PlayCount = *request.PlayCount
	}
	if request.IsFavorite != nil {
		data.Favorite = *request.IsFavorite
	}
	updated, err := s.media.UpdateUserDataForUser(r.Context(), viewer.mediaAccess(), targetID, data)
	if err != nil {
		writeUserDataUpdateError(w, err)
		return
	}
	item.UserData = updated
	writeEmbyJSON(w, http.StatusOK, mapUserData(item))
}

func writeUserDataUpdateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errInvalidPlayedSeason):
		writeEmbyError(w, http.StatusBadRequest, "InvalidRequest", "invalid season ID")
	case errors.Is(err, domain.ErrNotFound), errors.Is(err, medialibrary.ErrDisabled):
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "media item not found")
	default:
		writeEmbyError(w, http.StatusInternalServerError, "ServerError", "could not update user data")
	}
}
