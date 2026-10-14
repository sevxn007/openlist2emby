package emby

import (
	"net/http/httptest"
	"testing"

	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
)

func TestHideFromResumeRejectsInvalidAccess(t *testing.T) {
	for _, test := range []struct {
		name, method, user, item string
		allowed                  bool
		want                     int
	}{
		{"other user", "POST", "2", "7", true, 404},
		{"ungranted library", "POST", "3", "7", false, 404},
		{"invalid item", "POST", "3", "bad", true, 400},
		{"missing item", "POST", "3", "8", true, 404},
		{"wrong method", "GET", "3", "7", true, 404},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &playedItemsStore{item: medialibrary.Item{ID: 7, LibraryID: 4}, allowed: test.allowed}
			server, viewer := newPlayedItemsHandler(store)
			response := httptest.NewRecorder()
			server.handleHideFromResume(response, httptest.NewRequest(test.method, "/?Hide=true", nil), []string{"users", test.user, "items", test.item, "hidefromresume"}, viewer)
			if response.Code != test.want || store.item.UserData.HideFromResume {
				t.Fatalf("status=%d state=%#v", response.Code, store.item.UserData)
			}
		})
	}
}
