package httpapi

import (
	"net/http"

	"github.com/sevxn007/openlist2emby/backend/internal/app"
	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

func (s *Server) handleSelectMediaImage(w http.ResponseWriter, r *http.Request, session domain.Session) {
	if !s.requireMediaLibraries(w) {
		return
	}
	id, err := app.ParsePositiveInt(r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err, http.StatusBadRequest)
		return
	}
	var input struct {
		Source  string `json:"source"`
		Type    string `json:"type"`
		ID      int64  `json:"id"`
		Season  *int   `json:"season"`
		Episode *int   `json:"episode"`
		URL     string `json:"url"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if _, err := s.mediaLibraries.GetItemForUser(r.Context(), mediaUserAccess(session), id); err != nil {
		writeMediaLibraryError(w, err)
		return
	}
	images, err := s.application.MediaImages(r.Context(), domain.MediaImageQuery{Source: input.Source, Type: input.Type, ID: input.ID, Season: input.Season, Episode: input.Episode, ImageType: r.PathValue("type")})
	if err != nil {
		writeServiceError(w, err, http.StatusBadGateway)
		return
	}
	for _, image := range images {
		if image.URL != input.URL {
			continue
		}
		item, err := s.mediaLibraries.SetItemRemoteImageForUser(r.Context(), mediaUserAccess(session), id, r.PathValue("type"), image.URL)
		if err != nil {
			writeServiceError(w, err, http.StatusBadGateway)
			return
		}
		writeOK(w, item)
		return
	}
	writeError(w, http.StatusBadRequest, "image is no longer available")
}
