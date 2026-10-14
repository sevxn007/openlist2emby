package emby_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestEmbyItemQueriesDoNotIgnoreUnsupportedTypes(t *testing.T) {
	server, _, _, movie, _ := newEmbyTestServer(t)
	auth := authenticateEmby(t, server)
	for _, route := range []string{"/Users/1/Items", "/emby/Items"} {
		for _, test := range []struct {
			name  string
			query string
			count int
			start int
		}{
			{name: "playlist", query: "IncludeItemTypes=Playlist&Recursive=true"},
			{name: "playlist root", query: "IncludeItemTypes=Playlist"},
			{name: "playlist library", query: "IncludeItemTypes=Playlist&ParentId=" + strconv.FormatInt(8_000_000_000_000_000_000+movie.LibraryID, 10)},
			{name: "unsupported types", query: "IncludeItemTypes=Playlist,BoxSet&Recursive=true"},
			{name: "pagination", query: "IncludeItemTypes=Playlist&StartIndex=20", start: 20},
			{name: "case insensitive", query: "includeitemtypes=pLaYlIsT&recursive=true"},
			{name: "mixed types", query: "IncludeItemTypes=Playlist,Movie&Recursive=true", count: 1},
			{name: "supported type", query: "IncludeItemTypes=Movie&Recursive=true", count: 1},
			{name: "unfiltered", query: "Recursive=true", count: 1},
		} {
			t.Run(route+"/"+test.name, func(t *testing.T) {
				request := httptest.NewRequest(http.MethodGet, route+"?"+test.query, nil)
				request.Header.Set("X-Emby-Token", auth.AccessToken)
				response := httptest.NewRecorder()
				server.ServeHTTP(response, request)
				if response.Code != http.StatusOK {
					t.Fatalf("status = %d", response.Code)
				}
				var page struct {
					Items []struct {
						Id   string
						Type string
					}
					TotalRecordCount int
					StartIndex       int
				}
				if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
					t.Fatal(err)
				}
				if page.Items == nil || len(page.Items) != test.count || page.TotalRecordCount != test.count || page.StartIndex != test.start {
					t.Fatalf("unexpected page: %s", response.Body.String())
				}
				if test.count == 1 && (page.Items[0].Type != "Movie" || page.Items[0].Id != strconv.FormatInt(movie.ID, 10)) {
					t.Fatalf("unexpected item: %+v", page.Items[0])
				}
			})
		}
	}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/Users/1/Items?IncludeItemTypes=Playlist", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated playlist status = %d", response.Code)
	}
}
