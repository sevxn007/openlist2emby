package metadata

import (
	"context"
	"errors"
	"testing"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

type fakeProvider struct {
	searches, details int
	results           []domain.MediaSearchResult
	err               error
}

func (p *fakeProvider) Search(context.Context, Credentials, string, string) ([]domain.MediaSearchResult, error) {
	p.searches++
	return p.results, p.err
}
func (p *fakeProvider) Detail(_ context.Context, _ Credentials, kind string, id int64, _ *int, _ string) (domain.MediaDetail, error) {
	p.details++
	return domain.MediaDetail{ID: id, MediaType: kind, Overview: "only this source"}, p.err
}
func (p *fakeProvider) Credits(context.Context, Credentials, domain.MediaCreditsQuery, string) ([]domain.MediaPerson, error) {
	return nil, p.err
}
func TestSelectionPriorityAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name, mode   string
		firstErr     error
		firstResults []domain.MediaSearchResult
		wantSource   string
		wantErr      error
	}{
		{name: "no match falls through", wantSource: TVDB},
		{name: "ambiguity falls through", firstResults: []domain.MediaSearchResult{{ID: 1, MediaType: "tv", Name: "Show"}, {ID: 2, MediaType: "tv", Name: "Show"}}, wantSource: TVDB},
		{name: "auth stops", firstErr: ErrAuthentication, wantErr: ErrAuthentication},
		{name: "network stops", firstErr: ErrUnavailable, wantErr: ErrUnavailable},
		{name: "rate limit stops", firstErr: ErrRateLimited, wantErr: ErrRateLimited},
		{name: "fixed source no fallback", mode: TMDB, wantErr: ErrNoMatch},
		{name: "first reliable wins", firstResults: []domain.MediaSearchResult{{ID: 1, MediaType: "tv", Name: "Show"}}, wantSource: TMDB},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRegistry()
			first := &fakeProvider{results: tc.firstResults, err: tc.firstErr}
			second := &fakeProvider{results: []domain.MediaSearchResult{{ID: 3, MediaType: "tv", Name: "Show"}}}
			_ = r.Register(TMDB, first)
			_ = r.Register(TVDB, second)
			got, err := r.Resolve(t.Context(), domain.Settings{TMDBAPIKey: "synthetic", TVDBAPIKey: "synthetic"}, Selection{Mode: tc.mode, Kind: "tv", Name: "Show"})
			if !errors.Is(err, tc.wantErr) || got.Source != tc.wantSource {
				t.Fatalf("source=%s error=%v", got.Source, err)
			}
			if tc.firstErr != nil && second.searches != 0 {
				t.Fatal("fell back after provider failure")
			}
		})
	}
}
func TestSelectionUsesOnlyChosenSourceID(t *testing.T) {
	r := NewRegistry()
	p := &fakeProvider{}
	_ = r.Register(TVDB, p)
	got, err := r.Resolve(t.Context(), domain.Settings{TVDBAPIKey: "key"}, Selection{Mode: TVDB, Kind: "tv", IDs: map[string]int64{TMDB: 1, TVDB: 72860}})
	if err != nil || got.ID != 72860 || p.searches != 0 {
		t.Fatalf("result=%#v error=%v", got, err)
	}
}
func TestUnconfiguredAndPriorityValidation(t *testing.T) {
	r := NewRegistry()
	p := &fakeProvider{}
	_ = r.Register(TMDB, p)
	_ = r.Register(TVDB, p)
	for _, v := range [][]string{nil, {"tmdb", "tmdb"}, {"unknown"}} {
		if r.ValidatePriority(v) == nil {
			t.Fatalf("accepted %v", v)
		}
	}
	if r.ValidatePriority([]string{TVDB, TMDB}) != nil {
		t.Fatal("valid order rejected")
	}
	_, err := r.Resolve(t.Context(), domain.Settings{}, Selection{Mode: TVDB})
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatal(err)
	}
}
func TestReliableMatchRequiresUniqueTitleAndYear(t *testing.T) {
	year := 1940
	s := Selection{Kind: "tv", Name: "Tom & Jerry", Year: &year}
	values := []domain.MediaSearchResult{{ID: 1, MediaType: "tv", Name: "Tom & Jerry", FirstAirDate: "2014-01-01"}, {ID: 2, MediaType: "tv", OriginalTitle: "Tom & Jerry", FirstAirDate: "1940-02-10"}}
	if id := uniqueMatch(values, s); id != 2 {
		t.Fatal(id)
	}
	values = append(values, domain.MediaSearchResult{ID: 3, MediaType: "tv", Name: "Tom & Jerry", FirstAirDate: "1940-02-10"})
	if id := uniqueMatch(values, s); id != 0 {
		t.Fatal(id)
	}
}
