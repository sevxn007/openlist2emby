package emby

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
)

func (s *Server) handleNextUp(w http.ResponseWriter, r *http.Request, token string, viewer viewer) {
	if userID := queryValueFold(r, "UserId"); userID != "" && userID != viewer.idString() {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "user not found")
		return
	}

	query := medialibrary.NextUpQuery{
		EnableResumable:  queryBoolDefault(r, true, "EnableResumable"),
		EnableRewatching: queryBoolDefault(r, false, "EnableRewatching"),
		Start:            queryInt(r, 0, "StartIndex"),
		Limit:            queryInt(r, 100, "Limit"),
	}
	if raw := strings.TrimSpace(queryValueFold(r, "SeriesId")); raw != "" {
		id, err := parsePositiveID(raw)
		if err != nil {
			writeEmbyError(w, http.StatusBadRequest, "InvalidRequest", "invalid series ID")
			return
		}
		query.SeriesID = &id
	}
	if raw := strings.TrimSpace(queryValueFold(r, "ParentId")); raw != "" && raw != "0" {
		if libraryID, ok := parseLibraryViewID(raw); ok {
			query.LibraryID = &libraryID
		} else {
			id, err := parsePositiveID(raw)
			if err != nil {
				writeEmbyError(w, http.StatusBadRequest, "InvalidRequest", "invalid parent ID")
				return
			}
			query.ParentID = &id
		}
	}
	if raw := strings.TrimSpace(queryValueFold(r, "NextUpDateCutoff")); raw != "" {
		value, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeEmbyError(w, http.StatusBadRequest, "InvalidRequest", "invalid next up date cutoff")
			return
		}
		query.DateCutoff = &value
	}

	page, err := s.media.ListNextUpForUser(r.Context(), viewer.mediaAccess(), query)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrNotFound), errors.Is(err, medialibrary.ErrDisabled):
			writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "media item not found")
		case errors.Is(err, medialibrary.ErrInvalidLibrary):
			writeEmbyError(w, http.StatusBadRequest, "InvalidRequest", "invalid next up query")
		default:
			writeEmbyError(w, http.StatusInternalServerError, "ServerError", "could not list next episodes")
		}
		return
	}
	total := page.Total
	if !queryBoolDefault(r, true, "EnableTotalRecordCount") {
		total = 0
	}
	items := s.mapItems(r.Context(), page.Items, embyFieldRequested(r, "MediaSources"), true, token, viewer, embyFieldRequested(r, "People"), embyLanguage(r))
	writeEmbyJSON(w, http.StatusOK, queryResult{Items: items, TotalRecordCount: total, StartIndex: query.Start})
}

func (s *Server) handleContinueWatching(w http.ResponseWriter, r *http.Request, token string, viewer viewer) {
	itemQuery, ok := s.itemQueryFromRequest(w, r)
	if !ok {
		return
	}
	query := medialibrary.NextUpQuery{
		LibraryID:       itemQuery.LibraryID,
		ParentID:        itemQuery.ParentID,
		Kinds:           itemQuery.Kinds,
		EnableResumable: true,
		Start:           itemQuery.Start,
		Limit:           itemQuery.Limit,
	}
	page, err := s.media.ListContinueWatchingForUserQuery(r.Context(), viewer.mediaAccess(), query)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrNotFound), errors.Is(err, medialibrary.ErrDisabled):
			writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "media scope not found")
		case errors.Is(err, medialibrary.ErrInvalidLibrary):
			writeEmbyError(w, http.StatusBadRequest, "InvalidRequest", "invalid continue watching query")
		default:
			writeEmbyError(w, http.StatusInternalServerError, "ServerError", "could not list continue watching items")
		}
		return
	}
	items := make([]medialibrary.Item, len(page.Items))
	for index := range page.Items {
		items[index] = page.Items[index].Item
	}
	writeEmbyJSON(w, http.StatusOK, queryResult{
		Items:            s.mapItems(r.Context(), items, embyFieldRequested(r, "MediaSources"), true, token, viewer, embyFieldRequested(r, "People"), embyLanguage(r)),
		TotalRecordCount: page.Total,
		StartIndex:       query.Start,
	})
}

func queryBoolDefault(r *http.Request, fallback bool, name string) bool {
	raw := strings.TrimSpace(queryValueFold(r, name))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return parsed
}
