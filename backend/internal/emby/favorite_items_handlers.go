package emby

import (
	"errors"
	"net/http"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
)

func (s *Server) handleUserFavoriteItems(w http.ResponseWriter, r *http.Request, path []string, viewer viewer) {
	if len(path) < 4 || len(path) > 5 || path[0] != "users" || path[2] != "favoriteitems" {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "resource not found")
		return
	}
	if path[1] != viewer.idString() {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "user not found")
		return
	}

	favorite := false
	switch {
	case len(path) == 4 && r.Method == http.MethodPost:
		favorite = true
	case len(path) == 4 && r.Method == http.MethodDelete:
	case len(path) == 5 && path[4] == "delete" && r.Method == http.MethodPost:
	case len(path) == 5 && path[4] != "delete":
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "resource not found")
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
	item, err := s.media.GetItemForUser(r.Context(), viewer.mediaAccess(), id)
	if err != nil {
		writeFavoriteItemError(w, err)
		return
	}

	data := item.UserData
	data.Favorite = favorite
	data, err = s.media.UpdateUserDataForUser(r.Context(), viewer.mediaAccess(), id, data)
	if err != nil {
		writeFavoriteItemError(w, err)
		return
	}
	item.UserData = data
	writeEmbyJSON(w, http.StatusOK, mapUserData(item))
}

func writeFavoriteItemError(w http.ResponseWriter, err error) {
	if errors.Is(err, domain.ErrNotFound) || errors.Is(err, medialibrary.ErrDisabled) {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "media item not found")
		return
	}
	writeEmbyError(w, http.StatusInternalServerError, "ServerError", "could not update favorite state")
}
