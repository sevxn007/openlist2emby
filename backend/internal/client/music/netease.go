package music

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

const (
	netEaseBaseURL   = "https://music.163.com"
	netEaseUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/124.0 Safari/537.36"
)

type netEaseProvider struct {
	httpClient *http.Client
}

func newNetEaseProvider(httpClient *http.Client) Provider {
	return &netEaseProvider{httpClient: httpClient}
}

func (*netEaseProvider) Source() string { return "netease" }

func (p *netEaseProvider) Search(ctx context.Context, query string, limit int) ([]domain.MusicTrack, error) {
	request, err := newNetEaseRequest(ctx, http.MethodGet, "api/cloudsearch/pc", url.Values{
		"s": {query}, "type": {"1"}, "limit": {strconv.Itoa(limit)}, "offset": {"0"},
	}, nil)
	if err != nil {
		return nil, newError(http.StatusBadGateway, "music search failed", err)
	}
	responseBody, err := doJSON(p.httpClient, request)
	if err != nil {
		return nil, err
	}
	var response netEaseSearchEnvelope
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, newError(http.StatusBadGateway, "invalid music search response", err)
	}
	return p.mapSearchResults(response)
}

func (p *netEaseProvider) mapSearchResults(response netEaseSearchEnvelope) ([]domain.MusicTrack, error) {
	if !netEaseResponseOK(response.Code) {
		return nil, newError(http.StatusBadGateway, "music search failed", nil)
	}
	tracks := make([]domain.MusicTrack, 0, len(response.Result.Songs))
	for _, item := range response.Result.Songs {
		id := item.ID.String()
		if id == "" || strings.TrimSpace(item.Name) == "" {
			continue
		}
		tracks = append(tracks, domain.MusicTrack{
			ID: id, Name: strings.TrimSpace(item.Name), Artists: netEaseArtistNames(item.Artists),
			Album: strings.TrimSpace(item.Album.Name), PictureID: id, LyricID: id, Source: p.Source(),
			DurationMS: item.DurationMS.Int64(), TrackNumber: item.TrackNumber.Int(), DiscNumber: parseDiscNumber(item.DiscNumber.String()),
		})
	}
	return tracks, nil
}

func (p *netEaseProvider) Lyrics(ctx context.Context, id string) (string, string, error) {
	request, err := newNetEaseRequest(ctx, http.MethodGet, "api/song/lyric", url.Values{
		"id": {id}, "lv": {"-1"}, "kv": {"-1"}, "tv": {"-1"},
	}, nil)
	if err != nil {
		return "", "", newError(http.StatusBadGateway, "music lyrics request failed", err)
	}
	responseBody, err := doJSON(p.httpClient, request)
	if err != nil {
		return "", "", err
	}
	var response struct {
		Code   int `json:"code"`
		Lyrics struct {
			Text string `json:"lyric"`
		} `json:"lrc"`
		TranslatedLyrics struct {
			Text string `json:"lyric"`
		} `json:"tlyric"`
	}
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return "", "", newError(http.StatusBadGateway, "invalid music lyrics response", err)
	}
	if !netEaseResponseOK(response.Code) {
		return "", "", newError(http.StatusBadGateway, "music lyrics request failed", nil)
	}
	return response.Lyrics.Text, response.TranslatedLyrics.Text, nil
}

func (p *netEaseProvider) CoverURL(ctx context.Context, id string) (string, error) {
	request, err := newNetEaseRequest(ctx, http.MethodGet, "api/song/detail", url.Values{
		"id": {id}, "ids": {"[" + id + "]"},
	}, nil)
	if err != nil {
		return "", newError(http.StatusBadGateway, "music cover request failed", err)
	}
	responseBody, err := doJSON(p.httpClient, request)
	if err != nil {
		return "", err
	}
	var response struct {
		Code  int                     `json:"code"`
		Songs []netEaseSongDetailItem `json:"songs"`
	}
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return "", newError(http.StatusBadGateway, "invalid music cover response", err)
	}
	if !netEaseResponseOK(response.Code) || len(response.Songs) == 0 {
		return "", newError(http.StatusBadGateway, "music cover request failed", nil)
	}
	return validateCoverURL(response.Songs[0].Album.PictureURL)
}

func netEaseResponseOK(code int) bool {
	return code == 0 || code == http.StatusOK
}

func newNetEaseRequest(ctx context.Context, method, route string, values url.Values, body io.Reader) (*http.Request, error) {
	target := serviceURL(netEaseBaseURL, route, values)
	request, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Referer", netEaseBaseURL+"/")
	request.Header.Set("User-Agent", netEaseUserAgent)
	return request, nil
}

type flexibleString string

func (value *flexibleString) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*value = flexibleString(text)
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return fmt.Errorf("music identifier must be a string or number: %w", err)
	}
	*value = flexibleString(number.String())
	return nil
}

func (value flexibleString) String() string { return strings.TrimSpace(string(value)) }

type netEaseSearchEnvelope struct {
	Code   int `json:"code"`
	Result struct {
		Songs []netEasePCSearchItem `json:"songs"`
	} `json:"result"`
}

type netEasePCSearchItem struct {
	ID          flexibleString  `json:"id"`
	Name        string          `json:"name"`
	Artists     []netEaseArtist `json:"ar"`
	Album       netEaseAlbum    `json:"al"`
	DurationMS  flexibleInt64   `json:"dt"`
	TrackNumber flexibleInt     `json:"no"`
	DiscNumber  flexibleString  `json:"cd"`
}

type netEaseSongDetailItem struct {
	Album netEaseAlbum `json:"album"`
}

type flexibleInt int

func (value *flexibleInt) UnmarshalJSON(data []byte) error {
	var number flexibleString
	if err := number.UnmarshalJSON(data); err != nil {
		return err
	}
	parsed, err := strconv.Atoi(number.String())
	if err != nil {
		if number.String() == "" {
			*value = 0
			return nil
		}
		return err
	}
	*value = flexibleInt(parsed)
	return nil
}

func (value flexibleInt) Int() int { return int(value) }

type flexibleInt64 int64

func (value *flexibleInt64) UnmarshalJSON(data []byte) error {
	var number flexibleString
	if err := number.UnmarshalJSON(data); err != nil {
		return err
	}
	parsed, err := strconv.ParseInt(number.String(), 10, 64)
	if err != nil {
		if number.String() == "" {
			*value = 0
			return nil
		}
		return err
	}
	*value = flexibleInt64(parsed)
	return nil
}

func (value flexibleInt64) Int64() int64 { return int64(value) }

type netEaseArtist struct {
	Name string `json:"name"`
}

type netEaseAlbum struct {
	Name       string `json:"name"`
	PictureURL string `json:"picUrl"`
}

func netEaseArtistNames(values []netEaseArtist) []string {
	names := make([]string, 0, len(values))
	for _, value := range values {
		names = append(names, value.Name)
	}
	return compactStrings(names)
}

func parseDiscNumber(value string) int {
	value = strings.TrimSpace(value)
	if number, err := strconv.Atoi(value); err == nil {
		return number
	}
	for index, character := range value {
		if character < '0' || character > '9' {
			if index == 0 {
				return 0
			}
			number, _ := strconv.Atoi(value[:index])
			return number
		}
	}
	return 0
}
