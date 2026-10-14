package emby

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

func TestVidHubSeasonSelectionIsScopedAndConsumed(t *testing.T) {
	server := &Server{}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("User-Agent", "VidHub/2.3.5")
	alice := viewer{user: domain.LocalUser{ID: 1}}
	bob := viewer{user: domain.LocalUser{ID: 2}}

	server.rememberVidHubSeason(request, alice, "alice-token", 10, 11)
	if got := server.takeVidHubSeason(request, alice, "other-token", 10); got != "" {
		t.Fatalf("different token selection = %q", got)
	}
	if got := server.takeVidHubSeason(request, bob, "alice-token", 10); got != "" {
		t.Fatalf("different user selection = %q", got)
	}
	if got := server.takeVidHubSeason(request, alice, "alice-token", 20); got != "" {
		t.Fatalf("different series selection = %q", got)
	}
	if got := server.takeVidHubSeason(request, alice, "alice-token", 10); got != "11" {
		t.Fatalf("matching selection = %q", got)
	}
	if got := server.takeVidHubSeason(request, alice, "alice-token", 10); got != "" {
		t.Fatalf("consumed selection = %q", got)
	}
}

func TestNonVidHubRequestDoesNotUseSeasonSelection(t *testing.T) {
	server := &Server{}
	vidHubRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	vidHubRequest.Header.Set("User-Agent", "VidHub/2.3.5")
	otherRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	otherRequest.Header.Set("User-Agent", "Emby for Android")
	currentViewer := viewer{user: domain.LocalUser{ID: 1}}

	server.rememberVidHubSeason(vidHubRequest, currentViewer, "token", 10, 11)
	if got := server.takeVidHubSeason(otherRequest, currentViewer, "token", 10); got != "" {
		t.Fatalf("non-VidHub selection = %q", got)
	}
	if got := server.takeVidHubSeason(vidHubRequest, currentViewer, "token", 10); got != "11" {
		t.Fatalf("VidHub selection after other client = %q", got)
	}
}
