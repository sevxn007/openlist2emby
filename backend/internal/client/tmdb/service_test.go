package tmdbclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	tmdb "github.com/cyruzin/golang-tmdb"
	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

func TestMapTMDBSearchResultsFiltersPeopleAndNormalizesTitles(t *testing.T) {
	var response tmdb.SearchMulti
	if err := json.Unmarshal([]byte(`{
		"page": 1,
		"results": [
			{"id": 1, "media_type": "person", "name": "Someone"},
			{"id": 2, "media_type": "movie", "title": "Dune", "original_title": "Dune", "release_date": "2021-10-22", "poster_path": "/movie.jpg", "vote_average": 8.1},
			{"id": 3, "media_type": "tv", "name": "Severance", "original_name": "Severance", "first_air_date": "2022-02-18", "poster_path": "/tv.jpg", "vote_average": 8.4}
		]
	}`), &response); err != nil {
		t.Fatal(err)
	}

	results := mapSearchResults(&response)
	if len(results) != 2 {
		t.Fatalf("result count = %d, want 2", len(results))
	}
	if results[0].MediaType != "movie" || results[0].Title != "Dune" || results[0].PosterURL != imageBaseURL+"/movie.jpg" {
		t.Fatalf("unexpected movie mapping: %#v", results[0])
	}
	if results[1].MediaType != "tv" || results[1].Title != "Severance" || results[1].FirstAirDate != "2022-02-18" {
		t.Fatalf("unexpected tv mapping: %#v", results[1])
	}
}

func TestNormalizeTMDBLanguageUsesSupportedUILocales(t *testing.T) {
	tests := map[string]string{
		"":      "en-US",
		"en":    "en-US",
		"en-GB": "en-US",
		"fr-FR": "en-US",
		"zh":    "zh-CN",
		"zh-TW": "zh-CN",
	}
	for input, want := range tests {
		if got := normalizeLanguage(input); got != want {
			t.Fatalf("normalizeTMDBLanguage(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestChinesePersonPresentationUsesChineseAliasesAndCreditLabels(t *testing.T) {
	if got := chinesePersonName("Adam Scott", "Adam Scott", "亚当·斯科特"); got != "亚当·斯科特" {
		t.Fatalf("chinese person name = %q", got)
	}
	if got := chinesePersonName("Adam Scott", "Adam Scott"); got != "" {
		t.Fatalf("untranslated person name = %q", got)
	}
	if got := localizedCreditDisplayRole("Mark Scout", "Actor", "zh-CN"); got != "演员" {
		t.Fatalf("actor display role = %q", got)
	}
	if got := localizedCreditDisplayRole("制片人", "Producer", "zh-CN"); got != "制片人" {
		t.Fatalf("translated display role = %q", got)
	}
	if got := localizedCreditDisplayRole("Mark Scout", "Actor", "en-US"); got != "Mark Scout" {
		t.Fatalf("english display role = %q", got)
	}
}

func TestMapTVSeasonDetailIncludesSeasonAndEpisodeMetadata(t *testing.T) {
	var response tmdb.TVSeasonDetails
	if err := json.Unmarshal([]byte(`{
		"id": 200,
		"air_date": "2022-02-18",
		"name": "Season 1",
		"overview": "Season overview",
		"poster_path": "/season.jpg",
		"season_number": 1,
		"vote_average": 8.2,
		"episodes": [{
			"id": 300,
			"air_date": "2022-02-18",
			"episode_number": 1,
			"name": "Good News About Hell",
			"overview": "Episode overview",
			"runtime": 57,
			"season_number": 1,
			"still_path": "/episode.jpg",
			"vote_average": 8.4,
			"vote_count": 42
		}]
	}`), &response); err != nil {
		t.Fatal(err)
	}

	detail := mapTVSeasonDetail(&response)
	if detail.ID != 200 || detail.Name != "Season 1" || detail.FirstAirDate != "2022-02-18" || detail.PosterURL != imageBaseURL+"/season.jpg" {
		t.Fatalf("season detail = %#v", detail)
	}
	if len(detail.Episodes) != 1 {
		t.Fatalf("episodes = %#v", detail.Episodes)
	}
	episode := detail.Episodes[0]
	if episode.ID != 300 || episode.Name != "Good News About Hell" || episode.Runtime != 57 || episode.StillURL != imageBaseURL+"/episode.jpg" {
		t.Fatalf("episode detail = %#v", episode)
	}
}

func TestMapTVSeasonEpisodeCreditsGroupsGuestsAndCrew(t *testing.T) {
	var response tmdb.TVSeasonDetails
	if err := json.Unmarshal([]byte(`{
		"season_number": 1,
		"episodes": [
			{
				"episode_number": 1,
				"guest_stars": [
					{"id": 11, "name": "Guest One", "character": "Reporter", "order": 2, "profile_path": "/guest-one.jpg"},
					{"id": 12, "name": "Guest Two", "character": "Doctor", "order": 1, "profile_path": "/guest-two.jpg"}
				],
				"crew": [
					{"id": 21, "name": "Director One", "job": "Director", "profile_path": "/director.jpg"}
				]
			},
			{
				"episode_number": 2,
				"guest_stars": [
					{"id": 11, "name": "Guest One", "character": "Anchor", "order": 0, "profile_path": "/guest-one.jpg"}
				],
				"crew": [
					{"id": 22, "name": "Writer Two", "job": "Writer", "profile_path": "/writer.jpg"}
				]
			}
		]
	}`), &response); err != nil {
		t.Fatal(err)
	}

	episodes := mapTVSeasonEpisodeCredits(&response)
	if len(episodes) != 2 {
		t.Fatalf("episode count = %d, want 2", len(episodes))
	}
	if got := episodes[1]; len(got) != 3 || got[0].ID != 12 || got[1].ID != 11 || got[2].ID != 21 {
		t.Fatalf("episode 1 credits = %#v", got)
	}
	if got := episodes[1][0]; got.Type != "Actor" || got.Role != "Doctor" || got.ProfileURL != imageBaseURL+"/guest-two.jpg" {
		t.Fatalf("episode 1 guest = %#v", got)
	}
	if got := episodes[2]; len(got) != 2 || got[0].Role != "Anchor" || got[1].Type != "Writer" {
		t.Fatalf("episode 2 credits = %#v", got)
	}
}

func TestApplySeasonEpisodeLocalizationPreservesEpisodeRoles(t *testing.T) {
	episodes := map[int][]domain.MediaPerson{
		1: {{ID: 11, Name: "Guest One", Type: "Actor", Role: "Reporter"}},
		2: {{ID: 11, Name: "Guest One", Type: "Actor", Role: "Anchor"}},
	}
	localized := []domain.MediaPerson{{ID: 11, Name: "嘉宾一", Type: "Actor", Role: "Reporter", DisplayRole: "演员"}}

	applySeasonEpisodeLocalization(episodes, localized, "zh-CN")

	if episodes[1][0].Name != "嘉宾一" || episodes[2][0].Name != "嘉宾一" {
		t.Fatalf("localized names = %#v", episodes)
	}
	if episodes[1][0].Role != "Reporter" || episodes[2][0].Role != "Anchor" {
		t.Fatalf("episode roles changed = %#v", episodes)
	}
	if episodes[1][0].DisplayRole != "演员" || episodes[2][0].DisplayRole != "演员" {
		t.Fatalf("display roles = %#v", episodes)
	}
}

func TestClassifyTMDBErrors(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "invalid key", err: tmdb.Error{StatusCode: 7}, status: http.StatusUnauthorized},
		{name: "rate limit", err: tmdb.Error{StatusCode: 25}, status: http.StatusTooManyRequests},
		{name: "timeout", err: context.DeadlineExceeded, status: http.StatusGatewayTimeout},
		{name: "upstream", err: errors.New("network unavailable"), status: http.StatusBadGateway},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			classified := classifyError(test.err)
			serviceErr, ok := classified.(*Error)
			if !ok || serviceErr.StatusCode() != test.status {
				got := 0
				if ok {
					got = serviceErr.StatusCode()
				}
				t.Fatalf("status = %d, want %d", got, test.status)
			}
		})
	}
}
