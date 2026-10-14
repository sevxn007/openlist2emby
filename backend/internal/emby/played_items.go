package emby

import (
	"context"
	"errors"
	"net/http"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
)

var errInvalidPlayedSeason = errors.New("invalid played season")

func (s *Server) handlePlayedItems(w http.ResponseWriter, r *http.Request, path []string, token string, viewer viewer) {
	if len(path) < 4 || len(path) > 5 || path[0] != "users" || path[2] != "playeditems" {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "played item endpoint not found")
		return
	}
	if path[1] != viewer.idString() {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "user not found")
		return
	}

	played := false
	switch {
	case len(path) == 4 && r.Method == http.MethodPost:
		played = true
	case len(path) == 4 && r.Method == http.MethodDelete:
	case len(path) == 5 && path[4] == "delete" && r.Method == http.MethodPost:
	case len(path) == 5 && path[4] != "delete":
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "played item endpoint not found")
		return
	default:
		writeEmbyError(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "method not allowed")
		return
	}

	id, err := parsePositiveID(path[3])
	if err != nil {
		writeEmbyError(w, http.StatusBadRequest, "InvalidRequest", "invalid media item ID")
		return
	}
	seasonID := queryValueFold(r, "SeasonId")
	if seasonID == "" {
		seasonID = s.takeVidHubSeason(r, viewer, token, id)
	}
	targetID, err := s.playedItemTargetID(r.Context(), viewer.mediaAccess(), id, seasonID)
	if err != nil {
		switch {
		case errors.Is(err, errInvalidPlayedSeason):
			writeEmbyError(w, http.StatusBadRequest, "InvalidRequest", "invalid season ID")
		case errors.Is(err, domain.ErrNotFound), errors.Is(err, medialibrary.ErrDisabled):
			writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "media item not found")
		default:
			writeEmbyError(w, http.StatusInternalServerError, "ServerError", "could not update played state")
		}
		return
	}
	data, err := s.media.SetPlayedForUser(r.Context(), viewer.mediaAccess(), targetID, played)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) || errors.Is(err, medialibrary.ErrDisabled) {
			writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "media item not found")
			return
		}
		writeEmbyError(w, http.StatusInternalServerError, "ServerError", "could not update played state")
		return
	}
	writeEmbyJSON(w, http.StatusOK, mapUserDataValue(targetID, 0, data))
}

func (s *Server) playedItemTargetID(ctx context.Context, access medialibrary.UserAccess, itemID int64, rawSeasonID string) (int64, error) {
	if rawSeasonID == "" {
		return itemID, nil
	}
	seasonID, err := parsePositiveID(rawSeasonID)
	if err != nil {
		return 0, errInvalidPlayedSeason
	}
	series, err := s.media.GetItemForUser(ctx, access, itemID)
	if err != nil {
		return 0, err
	}
	season, err := s.media.GetItemForUser(ctx, access, seasonID)
	if err != nil {
		return 0, err
	}
	if series.Kind != medialibrary.KindSeries || season.Kind != medialibrary.KindSeason || season.ParentID == nil || *season.ParentID != series.ID {
		return 0, errInvalidPlayedSeason
	}
	return season.ID, nil
}
