package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
	"github.com/sevxn007/openlist2emby/backend/internal/testidentity"
)

func TestMediaImportQueuesValidatedPathsAndRequiresAdmin(t *testing.T) {
	server, store, cookie, item := newAuthenticatedMediaLibraryServer(t)
	endpoint := "/api/manager/media-libraries/" + strconv.FormatInt(item.LibraryID, 10) + "/import"
	for _, test := range []struct {
		name   string
		paths  []string
		status int
	}{
		{"empty", nil, http.StatusBadRequest},
		{"relative", []string{"Dune.mp4"}, http.StatusBadRequest},
		{"outside library", []string{"/MoviesOther/Dune.mp4"}, http.StatusBadRequest},
		{"traversal", []string{"/Movies/../Private/Dune.mp4"}, http.StatusBadRequest},
		{"valid", []string{"/Movies/Dune.mp4", "/Movies/Dune.mp4"}, http.StatusAccepted},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := requestJSON(http.MethodPost, endpoint, map[string]any{"paths": test.paths})
			request.AddCookie(cookie)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "admin-key") || strings.Contains(response.Body.String(), "openlist_base_url") {
				t.Fatal("response exposes integration credentials")
			}
			if test.status == http.StatusAccepted {
				var body struct {
					Data struct {
						ID      int64                    `json:"id"`
						Subject medialibrary.ScanPayload `json:"subject"`
					} `json:"data"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Data.ID <= 0 || body.Data.Subject.LibraryID != item.LibraryID || len(body.Data.Subject.Paths) != 1 || body.Data.Subject.Paths[0] != "/Movies/Dune.mp4" {
					t.Fatalf("queued import=%#v", body)
				}
			}
		})
	}
	editor, err := store.CreateUser(t.Context(), domain.LocalUser{OpenListUserID: 101, InstanceID: "test-instance", Username: "editor", Role: domain.LocalUserRoleMedia, Enabled: true, LibraryGrants: []domain.LibraryGrant{{LibraryID: item.LibraryID, Access: domain.LibraryAccessEdit}}})
	if err != nil {
		t.Fatal(err)
	}
	session, err := testidentity.Session(t.Context(), store, editor.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		cookie *http.Cookie
		status int
	}{
		{nil, http.StatusUnauthorized},
		{&http.Cookie{Name: sessionCookieName, Value: session.Token}, http.StatusForbidden},
	} {
		request := requestJSON(http.MethodPost, endpoint, map[string]any{"paths": []string{"/Movies/Dune.mp4"}})
		if test.cookie != nil {
			request.AddCookie(test.cookie)
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("authorization status=%d want=%d", response.Code, test.status)
		}
	}
}
