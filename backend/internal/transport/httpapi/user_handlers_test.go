package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
	"github.com/sevxn007/openlist2emby/backend/internal/testidentity"
)

func TestUserResponseUsesEmptyLibraryGrantArray(t *testing.T) {
	encoded, err := json.Marshal(userResponseFromDomain(domain.LocalUser{Role: domain.LocalUserRoleAdmin}))
	if err != nil {
		t.Fatal(err)
	}
	var decoded userResponse
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.LibraryGrants == nil {
		t.Fatalf("library_grants must be an empty array: %s", encoded)
	}
}

func TestUserManagementCreatesMediaUsersAndEnforcesRoles(t *testing.T) {
	server, store := newTestServer(t)
	if err := store.SaveIdentitySetup(t.Context(), "http://openlist.local", "admin-key", domain.OpenListIdentity{ID: 1, Username: "admin", Role: 2}); err != nil {
		t.Fatal(err)
	}
	admin, err := store.GetLocalUserByID(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !admin.Enabled || admin.Role != domain.LocalUserRoleAdmin || admin.ID != 1 {
		t.Fatalf("initial user = %#v", admin)
	}
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Movies", ContentType: medialibrary.ContentMovies, RootPath: "/Movies",
	})
	if err != nil {
		t.Fatal(err)
	}
	adminSession, err := testidentity.Session(t.Context(), store, admin.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	grants := []domain.LibraryGrant{{LibraryID: library.ID, Access: domain.LibraryAccessEdit}}
	create := requestJSON(http.MethodPost, "/api/manager/user-authorizations", grantTestRequest{
		OpenListUserID: 20, Role: "media", Enabled: true,
		LibraryGrants: &grants,
	})
	create.AddCookie(&http.Cookie{Name: sessionCookieName, Value: adminSession.Token})
	createdRecorder := httptest.NewRecorder()
	server.ServeHTTP(createdRecorder, create)
	if createdRecorder.Code != http.StatusCreated {
		t.Fatalf("create user status = %d body = %s", createdRecorder.Code, createdRecorder.Body.String())
	}
	var created struct {
		Data userResponse `json:"data"`
	}
	if err := json.Unmarshal(createdRecorder.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Data.Role != string(domain.LocalUserRoleMedia) || !created.Data.Enabled || len(created.Data.LibraryGrants) != 1 || created.Data.LibraryGrants[0].LibraryID != library.ID {
		t.Fatalf("created user = %#v", created.Data)
	}
	if len(created.Data.LibraryGrants) != 1 || created.Data.LibraryGrants[0].Access != domain.LibraryAccessEdit {
		t.Fatalf("created user grants = %#v", created.Data.LibraryGrants)
	}

	viewerSession, err := testidentity.Session(t.Context(), store, created.Data.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	settings := httptest.NewRecorder()
	settingsRequest := httptest.NewRequest(http.MethodGet, "/api/manager/settings", nil)
	settingsRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: viewerSession.Token})
	server.ServeHTTP(settings, settingsRequest)
	if settings.Code != http.StatusForbidden {
		t.Fatalf("media settings status = %d body = %s", settings.Code, settings.Body.String())
	}

	me := httptest.NewRecorder()
	meRequest := httptest.NewRequest(http.MethodGet, "/api/manager/auth/me", nil)
	meRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: viewerSession.Token})
	server.ServeHTTP(me, meRequest)
	if me.Code != http.StatusOK {
		t.Fatalf("me status = %d body = %s", me.Code, me.Body.String())
	}
	var current struct {
		Data currentUserResponse `json:"data"`
	}
	if err := json.Unmarshal(me.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}
	if current.Data.ID != created.Data.ID || current.Data.Role != string(domain.LocalUserRoleMedia) || current.Data.IsAdmin {
		t.Fatalf("current user = %#v", current.Data)
	}
	if len(current.Data.EditableLibraryIDs) != 0 {
		t.Fatalf("current user editable libraries = %#v", current.Data.EditableLibraryIDs)
	}
}

func TestAdministratorCreateDistinguishesDefaultAndEmptyLibrarySelection(t *testing.T) {
	server, store := newTestServer(t)
	if err := store.SaveIdentitySetup(t.Context(), "http://openlist.local", "admin-key", domain.OpenListIdentity{ID: 1, Username: "admin", Role: 2}); err != nil {
		t.Fatal(err)
	}
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Movies", ContentType: medialibrary.ContentMovies, RootPath: "/Movies",
	})
	if err != nil {
		t.Fatal(err)
	}
	admin, err := store.GetLocalUserByID(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	session, err := testidentity.Session(t.Context(), store, admin.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	create := func(request grantTestRequest) userResponse {
		t.Helper()
		httpRequest := requestJSON(http.MethodPost, "/api/manager/user-authorizations", request)
		httpRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httpRequest)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("create administrator status = %d body = %s", recorder.Code, recorder.Body.String())
		}
		var response struct {
			Data userResponse `json:"data"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		return response.Data
	}

	_ = library
	defaultAdmin := create(grantTestRequest{
		OpenListUserID: 30, Enabled: true, Role: string(domain.LocalUserRoleAdmin),
	})
	if len(defaultAdmin.LibraryGrants) != 0 {
		t.Fatalf("default administrator grants = %#v", defaultAdmin.LibraryGrants)
	}
	emptyGrants := []domain.LibraryGrant{}
	emptyAdmin := create(grantTestRequest{
		OpenListUserID: 31, Enabled: true, Role: string(domain.LocalUserRoleAdmin), LibraryGrants: &emptyGrants,
	})
	if len(emptyAdmin.LibraryGrants) != 0 {
		t.Fatalf("empty administrator grants = %#v", emptyAdmin.LibraryGrants)
	}
}

func TestUserManagementProtectsTheLastAdministrator(t *testing.T) {
	server, store := newTestServer(t)
	if err := store.SaveIdentitySetup(t.Context(), "http://openlist.local", "admin-key", domain.OpenListIdentity{ID: 1, Username: "admin", Role: 2}); err != nil {
		t.Fatal(err)
	}
	admin, err := store.GetLocalUserByID(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	session, err := testidentity.Session(t.Context(), store, admin.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	role := string(domain.LocalUserRoleMedia)
	request := requestJSON(http.MethodPut, "/api/manager/users/1", userUpdateRequest{Role: &role})
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("last admin demotion status = %d body = %s", recorder.Code, recorder.Body.String())
	}

	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/manager/users/1", nil)
	deleteRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
	deleted := httptest.NewRecorder()
	server.ServeHTTP(deleted, deleteRequest)
	if deleted.Code != http.StatusNotFound {
		t.Fatalf("self deletion status = %d body = %s", deleted.Code, deleted.Body.String())
	}
}

type grantTestRequest struct {
	OpenListUserID int64                  `json:"openlist_user_id"`
	Role           string                 `json:"role"`
	Enabled        bool                   `json:"enabled"`
	LibraryGrants  *[]domain.LibraryGrant `json:"library_grants"`
}

func TestRetiredAccountEndpointsAndCredentialFields(t *testing.T) {
	server, store := newTestServer(t)
	if err := store.SaveIdentitySetup(t.Context(), "http://openlist.local", "admin-key", domain.OpenListIdentity{ID: 1, Username: "admin", Role: 2}); err != nil {
		t.Fatal(err)
	}
	session, err := testidentity.Session(t.Context(), store, 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{http.MethodPost, "/api/manager/users", `{}`, 404},
		{http.MethodDelete, "/api/manager/users/1", `{}`, 404},
		{http.MethodPut, "/api/manager/users/1", `{"username":null}`, 400},
		{http.MethodPut, "/api/manager/users/1", `{"password":null}`, 400},
		{http.MethodPut, "/api/manager/users/1", `{"openlist_user_id":2}`, 400},
		{http.MethodPut, "/api/manager/users/1", `{} {}`, 400},
		{http.MethodGet, "/api/manager/openlist/users?page=0", ``, 400},
		{http.MethodGet, "/api/manager/openlist/users?per_page=101", ``, 400},
	} {
		t.Run(tc.method+tc.path+tc.body, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, req)
			var response struct {
				Code int `json:"code"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || recorder.Code != tc.status || response.Code != tc.status {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
	user, err := store.GetLocalUserByID(t.Context(), 1)
	if err != nil || user.Username != "admin" || !user.Enabled || user.OpenListUserID != 1 {
		t.Fatalf("rejected writes changed authorization: %+v %v", user, err)
	}
}
