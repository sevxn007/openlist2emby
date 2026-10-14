package emby_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
)

func TestEmbyHideFromResumePreservesStateAndCanBeReversed(t *testing.T) {
	server, store, _, item, _ := newEmbyTestServer(t)
	auth := authenticateEmby(t, server)
	now := time.Now().UTC()
	original := medialibrary.UserData{PlaybackPositionTicks: 4200000, PlayCount: 2, Favorite: true, LastPlayedAt: &now}
	for _, userID := range []int64{1, 2} {
		if _, err := store.UpdateMediaUserDataForUser(t.Context(), userID, item.ID, original); err != nil {
			t.Fatal(err)
		}
	}
	endpoint := "/emby/Users/" + auth.User.Id + "/Items/" + strconv.FormatInt(item.ID, 10) + "/HideFromResume?api_key=" + auth.AccessToken
	for _, hide := range []bool{true, true, false} {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, endpoint+"&Hide="+strconv.FormatBool(hide), nil))
		if response.Code != http.StatusOK {
			t.Fatalf("status %d: %s", response.Code, response.Body.String())
		}
		stored, err := store.GetMediaItemForUser(t.Context(), 1, item.ID)
		if err != nil {
			t.Fatal(err)
		}
		expected := original
		expected.HideFromResume = hide
		if !reflect.DeepEqual(stored.UserData, expected) {
			t.Fatalf("state changed: %#v", stored.UserData)
		}
		other, err := store.GetMediaItemForUser(t.Context(), 2, item.ID)
		if err != nil || other.UserData.HideFromResume {
			t.Fatalf("other user changed: %v", err)
		}
		resume := httptest.NewRecorder()
		server.ServeHTTP(resume, httptest.NewRequest(http.MethodGet, "/emby/Users/"+auth.User.Id+"/Items/Resume?api_key="+auth.AccessToken, nil))
		var page struct {
			Items            []json.RawMessage
			TotalRecordCount int
		}
		if err := json.Unmarshal(resume.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		want := 1
		if hide {
			want = 0
		}
		if resume.Code != 200 || len(page.Items) != want || page.TotalRecordCount != want {
			t.Fatalf("resume = %s", resume.Body.String())
		}
	}
	for _, value := range []string{"", "invalid"} {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, endpoint+"&Hide="+value, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid hide: %d", response.Code)
		}
	}
}
