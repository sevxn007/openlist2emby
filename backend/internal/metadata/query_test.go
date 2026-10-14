package metadata

import (
	"context"
	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"testing"
)

func TestParseQueryYear(t *testing.T) {
	for _, tc := range []struct {
		query, name string
		year        int
	}{
		{"逍遥 (2025)", "逍遥", 2025}, {"逍遥（2025）", "逍遥", 2025},
		{"神雕侠侣[2006]", "神雕侠侣", 2006}, {"神雕侠侣【1995】", "神雕侠侣", 1995},
		{"神雕侠侣.2014", "神雕侠侣", 2014}, {"神雕侠侣 1983", "神雕侠侣", 1983},
		{"2025 逍遥", "逍遥", 2025}, {"2001", "2001", 0},
		{"Blade Runner 2049", "Blade Runner", 2049},
		{"神雕侠侣2006", "神雕侠侣2006", 0}, {"Show 20250", "Show 20250", 0},
	} {
		t.Run(tc.query, func(t *testing.T) {
			name, year := ParseQuery(tc.query)
			if name != tc.name || tc.year == 0 && year != nil || tc.year != 0 && (year == nil || *year != tc.year) {
				t.Fatalf("name=%q year=%v", name, year)
			}
		})
	}
}

func TestRegistrySearchSeparatesYear(t *testing.T) {
	for _, source := range []string{TMDB, TVDB} {
		t.Run(source, func(t *testing.T) {
			provider := &queryProvider{}
			registry := NewRegistry()
			_ = registry.Register(source, provider)
			got, err := registry.Search(t.Context(), domain.Settings{TMDBAPIKey: "synthetic", TVDBAPIKey: "synthetic"}, source, "神雕侠侣（2006）")
			if err != nil || provider.query != "神雕侠侣" || len(got) != 1 || got[0].ID != 2 || got[0].Source != source {
				t.Fatalf("query=%q results=%#v err=%v", provider.query, got, err)
			}
		})
	}
}

type queryProvider struct {
	fakeProvider
	query string
}

func (p *queryProvider) Search(_ context.Context, _ Credentials, query, _ string) ([]domain.MediaSearchResult, error) {
	p.query = query
	return []domain.MediaSearchResult{
		{ID: 1, MediaType: "tv", Name: "神雕侠侣", FirstAirDate: "1995-01-01"},
		{ID: 2, MediaType: "tv", Name: "神雕侠侣", FirstAirDate: "2006-01-01"},
		{ID: 3, MediaType: "tv", Name: "神雕侠侣"},
	}, nil
}
