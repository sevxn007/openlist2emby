package emby_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestEmbyPlayedItemsRoute(t *testing.T) {
	server, _, _, item, _ := newEmbyTestServer(t)
	auth := authenticateEmby(t, server)
	itemID := strconv.FormatInt(item.ID, 10)

	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodPost,
		"/emby/Users/"+auth.User.Id+"/PlayedItems/"+itemID+"?api_key="+auth.AccessToken, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	var data struct {
		ItemID    string `json:"ItemId"`
		Played    bool   `json:"Played"`
		PlayCount int    `json:"PlayCount"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if data.ItemID != itemID || !data.Played || data.PlayCount != 1 {
		t.Fatalf("played data = %#v", data)
	}
}
