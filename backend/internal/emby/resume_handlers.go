package emby

import (
	"net/http"
	"strconv"
)

func (s *Server) handleHideFromResume(w http.ResponseWriter, r *http.Request, path []string, viewer viewer) {
	if r.Method != http.MethodPost || path[1] != viewer.idString() {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "resource not found")
		return
	}
	itemID, err := parsePositiveID(path[3])
	if err != nil {
		writeEmbyError(w, http.StatusBadRequest, "InvalidRequest", "invalid item ID")
		return
	}
	hide, err := strconv.ParseBool(queryValueFold(r, "Hide"))
	if err != nil {
		writeEmbyError(w, http.StatusBadRequest, "InvalidRequest", "invalid hide value")
		return
	}
	item, err := s.media.GetItemForUser(r.Context(), viewer.mediaAccess(), itemID)
	if err != nil {
		writeUserDataUpdateError(w, err)
		return
	}
	updated, err := s.media.SetHideFromResumeForUser(r.Context(), viewer.mediaAccess(), itemID, hide)
	if err != nil {
		writeUserDataUpdateError(w, err)
		return
	}
	item.UserData = updated
	writeEmbyJSON(w, http.StatusOK, mapUserData(item))
}
