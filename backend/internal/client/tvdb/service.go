// Package tvdb implements TheTVDB's official v4 API. Authentication tokens and
// vendor-specific ordering/pagination stay behind the metadata adapter.
package tvdb

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/metadata"
)

const apiURL = "https://api4.thetvdb.com/v4"
const maxResponseBytes = 16 << 20

type Service struct {
	client      *http.Client
	baseURL     string
	mu          sync.Mutex
	fingerprint [32]byte
	token       string
	expires     time.Time
	now         func() time.Time
}

func NewService() *Service {
	return &Service{client: &http.Client{Timeout: 30 * time.Second}, baseURL: apiURL, now: time.Now}
}

type envelope struct {
	Status string          `json:"status"`
	Data   json.RawMessage `json:"data"`
	Links  struct {
		Next *string `json:"next"`
	} `json:"links"`
}

func (s *Service) request(ctx context.Context, method, endpoint, token string, body []byte) (envelope, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.baseURL+endpoint, bytes.NewReader(body))
	if err != nil {
		return envelope{}, 0, metadata.ErrUnavailable
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return envelope{}, 0, ctx.Err()
		}
		return envelope{}, 0, metadata.ErrUnavailable
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return envelope{}, resp.StatusCode, metadata.ErrAuthentication
	case http.StatusNotFound:
		return envelope{}, resp.StatusCode, domain.ErrNotFound
	case http.StatusTooManyRequests:
		return envelope{}, resp.StatusCode, metadata.ErrRateLimited
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return envelope{}, resp.StatusCode, metadata.ErrUnavailable
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil || len(payload) > maxResponseBytes {
		return envelope{}, resp.StatusCode, metadata.ErrUnavailable
	}
	var out envelope
	if json.Unmarshal(payload, &out) != nil || out.Status != "success" {
		return envelope{}, resp.StatusCode, metadata.ErrUnavailable
	}
	return out, resp.StatusCode, nil
}
func (s *Service) login(ctx context.Context, c metadata.Credentials) (string, error) {
	if strings.TrimSpace(c.Key) == "" {
		return "", metadata.ErrNotConfigured
	}
	fingerprint := sha256.Sum256([]byte(c.Key + "\x00" + c.PIN))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fingerprint == fingerprint && s.token != "" && s.now().Before(s.expires) {
		return s.token, nil
	}
	payload, _ := json.Marshal(struct {
		Key string `json:"apikey"`
		PIN string `json:"pin,omitempty"`
	}{c.Key, c.PIN})
	result, _, err := s.request(ctx, http.MethodPost, "/login", "", payload)
	if err != nil {
		return "", err
	}
	var data struct {
		Token string `json:"token"`
	}
	if json.Unmarshal(result.Data, &data) != nil || data.Token == "" {
		return "", metadata.ErrAuthentication
	}
	s.fingerprint = fingerprint
	s.token = data.Token
	s.expires = s.now().Add(27 * 24 * time.Hour)
	return data.Token, nil
}
func (s *Service) get(ctx context.Context, c metadata.Credentials, endpoint string, target any) (envelope, error) {
	for attempt := 0; attempt < 2; attempt++ {
		token, err := s.login(ctx, c)
		if err != nil {
			return envelope{}, err
		}
		result, status, err := s.request(ctx, http.MethodGet, endpoint, token, nil)
		if status == http.StatusUnauthorized && attempt == 0 {
			s.mu.Lock()
			if s.token == token {
				s.token = ""
			}
			s.mu.Unlock()
			continue
		}
		if err != nil {
			return envelope{}, err
		}
		if json.Unmarshal(result.Data, target) != nil {
			return envelope{}, metadata.ErrUnavailable
		}
		return result, nil
	}
	return envelope{}, metadata.ErrAuthentication
}
func (s *Service) Test(ctx context.Context, c metadata.Credentials) error {
	_, err := s.login(ctx, c)
	return err
}
func (s *Service) Search(ctx context.Context, c metadata.Credentials, query, language string) ([]domain.MediaSearchResult, error) {
	results := []domain.MediaSearchResult{}
	for offset := 0; offset < 5000; offset += 100 {
		var records []struct {
			ID                         string `json:"tvdb_id"`
			Type, Name, Overview, Year string
			Image                      string `json:"image_url"`
			Date                       string `json:"first_air_time"`
			Translations               map[string]string
			Overviews                  map[string]string
		}
		e, err := s.get(ctx, c, "/search?query="+url.QueryEscape(query)+"&limit=100&offset="+strconv.Itoa(offset), &records)
		if err != nil {
			return nil, err
		}
		for _, r := range records {
			id, err := strconv.ParseInt(r.ID, 10, 64)
			if err != nil || id <= 0 || r.Type != "movie" && r.Type != "series" {
				continue
			}
			name, overview := r.Name, r.Overview
			if r.Translations["zho"] != "" {
				name = r.Translations["zho"]
			}
			if r.Overviews["zho"] != "" {
				overview = r.Overviews["zho"]
			}
			kind := r.Type
			if kind == "series" {
				kind = "tv"
			}
			date := r.Date
			if date == "" {
				date = r.Year
			}
			item := domain.MediaSearchResult{Source: metadata.TVDB, ID: id, MediaType: kind, Title: name, Name: name, OriginalTitle: r.Name, Overview: overview, PosterURL: r.Image}
			if kind == "tv" {
				item.FirstAirDate = date
			} else {
				item.ReleaseDate = date
			}
			results = append(results, item)
		}
		if len(records) < 100 || e.Links.Next == nil || *e.Links.Next == "" {
			return results, nil
		}
	}
	return nil, fmt.Errorf("%w: search pagination limit", metadata.ErrUnavailable)
}

type translation struct{ Language, Name, Overview string }
type translations struct {
	Names     []translation `json:"nameTranslations"`
	Overviews []translation `json:"overviewTranslations"`
}
type season struct {
	ID           int64
	Number       int
	Name, Image  string
	Type         struct{ Type string }
	Translations []translation
}
type artwork struct {
	Type                       int
	Image, Thumbnail, Language string
	Width, Height              int
}
type character struct {
	PeopleID   int64 `json:"peopleId"`
	Name       string
	PersonName string `json:"personName"`
	Image      string `json:"personImgURL"`
	PeopleType string `json:"peopleType"`
	Sort       int
	Type       int
}
type record struct {
	ID                                                  int64
	Name, Overview, Image, FirstAired, OriginalLanguage string
	AverageRuntime                                      int
	Runtime                                             int
	FirstRelease                                        struct{ Date string } `json:"first_release"`
	Genres                                              []struct{ Name string }
	RemoteIDs                                           []struct{ ID, SourceName string }
	Seasons                                             []season
	Artworks                                            []artwork
	Characters                                          []character
	Translations                                        translations
}
type episode struct {
	ID, SeriesID                  int64
	Number, SeasonNumber, Runtime int
	Name, Overview, Image, Aired  string
	NameTranslations              []string
	OverviewTranslations          []string
}

func localized(r record) (string, string) {
	name, overview := r.Name, r.Overview
	// Movie records may only expose overviews in translations. Prefer Chinese,
	// otherwise original language, without borrowing another source's text.
	for _, lang := range []string{r.OriginalLanguage, "zho"} {
		for _, t := range r.Translations.Names {
			if t.Language == lang && t.Name != "" {
				name = t.Name
			}
		}
		for _, t := range r.Translations.Overviews {
			if t.Language == lang && t.Overview != "" {
				overview = t.Overview
			}
		}
	}
	return name, overview
}
func people(values []character) []domain.MediaPerson {
	out := make([]domain.MediaPerson, 0, len(values))
	seen := map[int64]bool{}
	for _, c := range values {
		if c.PeopleID <= 0 || c.PersonName == "" || seen[c.PeopleID] {
			continue
		}
		seen[c.PeopleID] = true
		kind := c.PeopleType
		if kind == "" {
			kind = "Actor"
		}
		out = append(out, domain.MediaPerson{ID: c.PeopleID, ExternalID: c.PeopleID, Source: metadata.TVDB, Name: c.PersonName, ProfileURL: c.Image, Type: kind, Role: c.Name, SortOrder: c.Sort})
	}
	return out
}
func (s *Service) Detail(ctx context.Context, c metadata.Credentials, kind string, id int64, number *int, language string) (domain.MediaDetail, error) {
	if id <= 0 || kind != "movie" && kind != "tv" {
		return domain.MediaDetail{}, metadata.ErrInvalidSource
	}
	endpoint := "/movies/"
	if kind == "tv" {
		endpoint = "/series/"
	}
	var r record
	_, err := s.get(ctx, c, endpoint+strconv.FormatInt(id, 10)+"/extended?meta=translations", &r)
	if err != nil {
		return domain.MediaDetail{}, err
	}
	if r.ID != id {
		return domain.MediaDetail{}, metadata.ErrUnavailable
	}
	name, overview := localized(r)
	d := domain.MediaDetail{ID: r.ID, Source: metadata.TVDB, MediaType: kind, Title: name, Name: name, OriginalTitle: r.Name, Overview: overview, PosterURL: r.Image, Runtime: r.Runtime, People: people(r.Characters)}
	if d.Runtime == 0 {
		d.Runtime = r.AverageRuntime
	}
	if kind == "tv" {
		d.FirstAirDate = r.FirstAired
	} else {
		d.ReleaseDate = r.FirstRelease.Date
	}
	for _, g := range r.Genres {
		d.Genres = append(d.Genres, g.Name)
	}
	for _, rid := range r.RemoteIDs {
		if strings.EqualFold(rid.SourceName, "IMDB") {
			d.IMDbID = rid.ID
		}
	}
	// API artwork types are resolved by their official names, not hard-coded IDs.
	if len(r.Artworks) > 0 {
		var types []struct {
			ID   int
			Name string
		}
		if _, err := s.get(ctx, c, "/artwork/types", &types); err != nil {
			return domain.MediaDetail{}, err
		}
		names := map[int]string{}
		for _, t := range types {
			names[t.ID] = strings.ToLower(t.Name)
		}
		for _, lang := range []string{"", "eng", "zho"} {
			for _, a := range r.Artworks {
				if a.Language != lang {
					continue
				}
				n := names[a.Type]
				if strings.Contains(n, "background") || strings.Contains(n, "fanart") {
					d.BackdropURL = a.Image
				}
				if strings.Contains(n, "poster") {
					d.PosterURL = a.Image
				}
			}
		}
	}
	if kind == "movie" || number == nil {
		for _, s := range r.Seasons {
			if s.Type.Type != "official" {
				continue
			}
			d.Seasons = append(d.Seasons, domain.MediaSeason{Source: metadata.TVDB, ID: s.ID, SeasonNumber: s.Number, Name: s.Name, PosterURL: s.Image})
		}
		d.NumberOfSeasons = len(d.Seasons)
		return d, nil
	}
	var selected *season
	for i := range r.Seasons {
		if r.Seasons[i].Type.Type == "official" && r.Seasons[i].Number == *number {
			if selected != nil {
				return domain.MediaDetail{}, metadata.ErrUnavailable
			}
			selected = &r.Seasons[i]
		}
	}
	if selected == nil {
		return domain.MediaDetail{}, domain.ErrNotFound
	}
	var seasonRecord season
	if _, err := s.get(ctx, c, "/seasons/"+strconv.FormatInt(selected.ID, 10)+"/extended", &seasonRecord); err != nil {
		return domain.MediaDetail{}, err
	}
	if seasonRecord.ID != selected.ID || seasonRecord.Number != *number {
		return domain.MediaDetail{}, metadata.ErrUnavailable
	}
	d.ID = seasonRecord.ID
	d.Title = ""
	d.Name = seasonRecord.Name
	d.Overview = ""
	d.PosterURL = seasonRecord.Image
	d.BackdropURL = ""
	d.SeasonNumber = number
	d.People = nil
	for _, t := range seasonRecord.Translations {
		if t.Language == "zho" {
			if t.Name != "" {
				d.Name = t.Name
			}
			d.Overview = t.Overview
		}
	}
	episodes, err := s.episodes(ctx, c, id, *number)
	if err != nil {
		return domain.MediaDetail{}, err
	}
	d.Episodes = episodes
	d.NumberOfEpisodes = len(episodes)
	return d, nil
}
func (s *Service) episodes(ctx context.Context, c metadata.Credentials, id int64, number int) ([]domain.MediaEpisode, error) {
	out := []domain.MediaEpisode{}
	seen := map[int]bool{}
	for page := 0; page < 100; page++ {
		var data struct{ Episodes []episode }
		e, err := s.get(ctx, c, fmt.Sprintf("/series/%d/episodes/official?season=%d&page=%d", id, number, page), &data)
		if err != nil {
			return nil, err
		}
		for _, r := range data.Episodes {
			if r.ID <= 0 || r.SeriesID != id || r.SeasonNumber != number || seen[r.Number] {
				return nil, metadata.ErrUnavailable
			}
			seen[r.Number] = true
			name, overview := r.Name, r.Overview
			if contains(r.NameTranslations, "zho") || contains(r.OverviewTranslations, "zho") {
				var t translation
				_, err := s.get(ctx, c, fmt.Sprintf("/episodes/%d/translations/zho", r.ID), &t)
				if err != nil && !errors.Is(err, domain.ErrNotFound) {
					return nil, err
				}
				if t.Name != "" {
					name = t.Name
				}
				if t.Overview != "" {
					overview = t.Overview
				}
			}
			out = append(out, domain.MediaEpisode{Source: metadata.TVDB, ID: r.ID, Name: name, Overview: overview, SeasonNumber: r.SeasonNumber, EpisodeNumber: r.Number, AirDate: r.Aired, Runtime: r.Runtime, StillURL: r.Image})
		}
		if e.Links.Next == nil || *e.Links.Next == "" {
			return out, nil
		}
		if len(data.Episodes) == 0 {
			return nil, metadata.ErrUnavailable
		}
	}
	return nil, fmt.Errorf("%w: episode pagination limit", metadata.ErrUnavailable)
}
func contains(values []string, s string) bool {
	for _, v := range values {
		if v == s {
			return true
		}
	}
	return false
}
func (s *Service) Credits(ctx context.Context, c metadata.Credentials, q domain.MediaCreditsQuery, l string) ([]domain.MediaPerson, error) {
	id := q.ExternalID
	if id == 0 {
		id = q.TMDBID
	}
	if q.EpisodeNumber != nil && q.SeasonNumber != nil {
		eps, err := s.episodes(ctx, c, id, *q.SeasonNumber)
		if err != nil {
			return nil, err
		}
		for _, ep := range eps {
			if ep.EpisodeNumber == *q.EpisodeNumber {
				var r record
				_, err := s.get(ctx, c, fmt.Sprintf("/episodes/%d/extended", ep.ID), &r)
				return people(r.Characters), err
			}
		}
		return []domain.MediaPerson{}, nil
	}
	d, err := s.Detail(ctx, c, q.MediaType, id, nil, l)
	return d.People, err
}
