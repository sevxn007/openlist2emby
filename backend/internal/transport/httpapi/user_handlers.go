package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/sevxn007/openlist2emby/backend/internal/app"
	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	users, err := s.application.ListUsers(r.Context())
	if err != nil {
		writeServiceError(w, err, http.StatusInternalServerError)
		return
	}
	response := make([]userResponse, 0, len(users))
	for _, user := range users {
		response = append(response, userResponseFromDomain(user))
	}
	writeOK(w, map[string]any{"items": response, "total": len(response)})
}

func (s *Server) handleOpenListUsers(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	page, perPage := 1, 50
	var err error
	if raw := r.URL.Query().Get("page"); raw != "" {
		page, err = strconv.Atoi(raw)
		if err != nil {
			writeError(w, 400, "invalid pagination")
			return
		}
	}
	if raw := r.URL.Query().Get("per_page"); raw != "" {
		perPage, err = strconv.Atoi(raw)
		if err != nil {
			writeError(w, 400, "invalid pagination")
			return
		}
	}
	result, err := s.application.OpenListUsers(r.Context(), page, perPage)
	if err != nil {
		writeServiceError(w, err, 500)
		return
	}
	writeOK(w, result)
}
func (s *Server) handleGrantIdentity(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var request struct {
		OpenListUserID int64                 `json:"openlist_user_id"`
		Role           string                `json:"role"`
		Enabled        bool                  `json:"enabled"`
		LibraryGrants  []domain.LibraryGrant `json:"library_grants"`
	}
	if !readAuthorizationJSON(w, r, &request) {
		return
	}
	user, err := s.application.GrantIdentity(r.Context(), app.IdentityGrantInput{OpenListUserID: request.OpenListUserID, Role: domain.LocalUserRole(request.Role), Enabled: request.Enabled, LibraryGrants: request.LibraryGrants})
	if err != nil {
		writeServiceError(w, err, 500)
		return
	}
	writeData(w, 201, userResponseFromDomain(user))
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request, session domain.Session) {
	userID, ok := parseUserID(w, r.PathValue("id"))
	if !ok {
		return
	}
	var request userUpdateRequest
	if !readAuthorizationJSON(w, r, &request) {
		return
	}
	input := app.UserUpdateInput{
		Enabled:       request.Enabled,
		LibraryGrants: request.LibraryGrants,
	}
	if request.Role != nil {
		role := domain.LocalUserRole(strings.TrimSpace(*request.Role))
		input.Role = &role
	}
	user, err := s.application.UpdateUser(r.Context(), session, userID, input)
	if err != nil {
		writeServiceError(w, err, http.StatusInternalServerError)
		return
	}
	writeOK(w, userResponseFromDomain(user))
}

func userResponseFromDomain(user domain.LocalUser) userResponse {
	if user.UpstreamStatus == "" {
		user.UpstreamStatus = "unavailable"
	}
	return userResponse{
		OpenListUserID: user.OpenListUserID, UpstreamStatus: user.UpstreamStatus, ID: user.ID, Username: user.Username, Role: string(user.Role), Enabled: user.Enabled,
		LibraryGrants: append([]domain.LibraryGrant{}, user.LibraryGrants...),
		CreatedAt:     user.CreatedAt, UpdatedAt: user.UpdatedAt,
	}
}

func parseUserID(w http.ResponseWriter, value string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "id must be a positive integer")
		return 0, false
	}
	return id, true
}

// Authorization writes accept only the documented fields, including rejecting
// retired credential keys even when their JSON values are null.
func readAuthorizationJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, 400, "invalid authorization fields")
		return false
	}
	if decoder.Decode(new(any)) != io.EOF {
		writeError(w, 400, "invalid JSON body")
		return false
	}
	return true
}
