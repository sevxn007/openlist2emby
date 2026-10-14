// Package metadata owns provider selection. An item is resolved from one
// provider, never assembled by mixing fields from several providers.
package metadata

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

const (
	TMDB = "tmdb"
	TVDB = "tvdb"
	Auto = "auto"
)

var (
	ErrNotConfigured  = errors.New("metadata source is not configured")
	ErrNoMatch        = errors.New("no unambiguous metadata match")
	ErrInvalidSource  = errors.New("invalid metadata source")
	ErrAuthentication = errors.New("metadata source authentication failed")
	ErrRateLimited    = errors.New("metadata source rate limited")
	ErrUnavailable    = errors.New("metadata source unavailable")
)

type Credentials struct{ Key, PIN string }
type Provider interface {
	Search(context.Context, Credentials, string, string) ([]domain.MediaSearchResult, error)
	Detail(context.Context, Credentials, string, int64, *int, string) (domain.MediaDetail, error)
	Credits(context.Context, Credentials, domain.MediaCreditsQuery, string) ([]domain.MediaPerson, error)
}

type Registry struct{ providers map[string]Provider }

func NewRegistry() *Registry { return &Registry{providers: make(map[string]Provider)} }

// Register is called during startup, before the registry is shared by requests.
func (r *Registry) Register(name string, provider Provider) error {
	if name == "" || name == Auto || provider == nil || r.providers[name] != nil {
		return ErrInvalidSource
	}
	r.providers[name] = provider
	return nil
}
func (r *Registry) ValidatePriority(priority []string) error {
	if len(priority) == 0 {
		return ErrInvalidSource
	}
	seen := make(map[string]bool)
	for _, source := range priority {
		if r.providers[source] == nil || seen[source] {
			return ErrInvalidSource
		}
		seen[source] = true
	}
	return nil
}
func Priority(settings domain.Settings) []string {
	if len(settings.MetadataPriority) == 0 {
		return []string{TMDB, TVDB}
	}
	return settings.MetadataPriority
}
func credentials(settings domain.Settings, source string) Credentials {
	switch source {
	case TMDB:
		return Credentials{Key: settings.TMDBAPIKey}
	case TVDB:
		return Credentials{Key: settings.TVDBAPIKey, PIN: settings.TVDBPIN}
	}
	return Credentials{}
}
func (r *Registry) Configured(settings domain.Settings, source string) bool {
	return r.providers[source] != nil && strings.TrimSpace(credentials(settings, source).Key) != ""
}
func (r *Registry) Search(ctx context.Context, settings domain.Settings, source, query string) ([]domain.MediaSearchResult, error) {
	p := r.providers[source]
	if p == nil {
		return nil, ErrInvalidSource
	}
	c := credentials(settings, source)
	if c.Key == "" {
		return nil, ErrNotConfigured
	}
	name, year := ParseQuery(query)
	results, err := p.Search(ctx, c, name, "zh-CN")
	if err != nil {
		return nil, safeProviderError(err)
	}
	for i := range results {
		results[i].Source = source
	}
	return FilterYear(results, year), nil
}
func (r *Registry) Detail(ctx context.Context, settings domain.Settings, source, kind string, id int64, season *int) (domain.MediaDetail, error) {
	p := r.providers[source]
	if p == nil {
		return domain.MediaDetail{}, ErrInvalidSource
	}
	c := credentials(settings, source)
	if c.Key == "" {
		return domain.MediaDetail{}, ErrNotConfigured
	}
	d, err := p.Detail(ctx, c, kind, id, season, "zh-CN")
	if err != nil {
		return domain.MediaDetail{}, safeProviderError(err)
	}
	if d.ID <= 0 || d.MediaType != kind || (season == nil && d.ID != id) {
		return domain.MediaDetail{}, fmt.Errorf("%w: invalid source identity", ErrUnavailable)
	}
	d.Source = source
	for i := range d.Seasons {
		d.Seasons[i].Source = source
	}
	for i := range d.Episodes {
		d.Episodes[i].Source = source
		tagPeople(d.Episodes[i].People, source)
	}
	tagPeople(d.People, source)
	return d, nil
}
func tagPeople(people []domain.MediaPerson, source string) {
	for i := range people {
		people[i].Source = source
		if people[i].ExternalID == 0 {
			people[i].ExternalID = people[i].ID
		}
	}
}
func (r *Registry) Credits(ctx context.Context, settings domain.Settings, query domain.MediaCreditsQuery) ([]domain.MediaPerson, error) {
	source := query.Source
	if source == "" {
		source = TMDB
	}
	p := r.providers[source]
	if p == nil {
		return nil, ErrInvalidSource
	}
	c := credentials(settings, source)
	if c.Key == "" {
		return nil, ErrNotConfigured
	}
	values, err := p.Credits(ctx, c, query, "zh-CN")
	tagPeople(values, source)
	return values, err
}

type Selection struct {
	Mode, Kind, Name string
	Year             *int
	IDs              map[string]int64
}

// Resolve stops on provider failures. Only an absent/ambiguous match allows the
// next source to be tried; outages must not replace existing metadata.
func (r *Registry) Resolve(ctx context.Context, settings domain.Settings, selection Selection) (domain.MediaDetail, error) {
	sources := Priority(settings)
	if selection.Mode != "" && selection.Mode != Auto {
		sources = []string{selection.Mode}
	}
	for _, source := range sources {
		if r.providers[source] == nil {
			return domain.MediaDetail{}, ErrInvalidSource
		}
		if !r.Configured(settings, source) {
			if selection.Mode != Auto && selection.Mode != "" {
				return domain.MediaDetail{}, ErrNotConfigured
			}
			continue
		}
		id := selection.IDs[source]
		if id <= 0 {
			values, err := r.Search(ctx, settings, source, selection.Name)
			if err != nil {
				return domain.MediaDetail{}, err
			}
			id = uniqueMatch(values, selection)
			if id == 0 {
				continue
			}
		}
		d, err := r.Detail(ctx, settings, source, selection.Kind, id, nil)
		if errors.Is(err, domain.ErrNotFound) {
			continue
		}
		if err != nil {
			return domain.MediaDetail{}, err
		}
		return d, nil
	}
	return domain.MediaDetail{}, ErrNoMatch
}
func normalizeTitle(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
func uniqueMatch(values []domain.MediaSearchResult, selection Selection) int64 {
	ids := map[int64]bool{}
	target := normalizeTitle(selection.Name)
	for _, v := range values {
		if v.ID <= 0 || v.MediaType != selection.Kind {
			continue
		}
		if target != "" && normalizeTitle(v.Title) != target && normalizeTitle(v.Name) != target && normalizeTitle(v.OriginalTitle) != target {
			continue
		}
		date := v.ReleaseDate
		if date == "" {
			date = v.FirstAirDate
		}
		if selection.Year != nil {
			if len(date) < 4 {
				continue
			}
			year, err := strconv.Atoi(date[:4])
			if err != nil || year != *selection.Year {
				continue
			}
		}
		ids[v.ID] = true
	}
	if len(ids) != 1 {
		return 0
	}
	for id := range ids {
		return id
	}
	return 0
}

func (r *Registry) SeasonEpisodeCredits(ctx context.Context, settings domain.Settings, q domain.MediaCreditsQuery) (map[int][]domain.MediaPerson, error) {
	source := q.Source
	if source == "" {
		source = TMDB
	}
	p := r.providers[source]
	if p == nil {
		return nil, ErrInvalidSource
	}
	c := credentials(settings, source)
	if c.Key == "" {
		return nil, ErrNotConfigured
	}
	if batch, ok := p.(interface {
		SeasonEpisodeCredits(context.Context, Credentials, domain.MediaCreditsQuery, string) (map[int][]domain.MediaPerson, error)
	}); ok {
		result, err := batch.SeasonEpisodeCredits(ctx, c, q, "zh-CN")
		for _, people := range result {
			tagPeople(people, source)
		}
		return result, err
	}
	id := q.ExternalID
	if id == 0 {
		id = q.TMDBID
	}
	d, err := r.Detail(ctx, settings, source, "tv", id, q.SeasonNumber)
	if err != nil {
		return nil, safeProviderError(err)
	}
	result := map[int][]domain.MediaPerson{}
	for _, ep := range d.Episodes {
		result[ep.EpisodeNumber] = ep.People
	}
	return result, nil
}

func safeProviderError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, domain.ErrNotFound) || errors.Is(err, ErrAuthentication) || errors.Is(err, ErrRateLimited) || errors.Is(err, ErrNotConfigured) {
		return err
	}
	if status, ok := err.(interface{ StatusCode() int }); ok {
		switch status.StatusCode() {
		case 401, 403:
			return ErrAuthentication
		case 404:
			return domain.ErrNotFound
		case 429:
			return ErrRateLimited
		}
	}
	return ErrUnavailable
}
