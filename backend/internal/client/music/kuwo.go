package music

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

const (
	kuwoBaseURL   = "https://www.kuwo.cn"
	kuwoLegacyURL = "https://search.kuwo.cn"
	kuwoUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/124.0 Safari/537.36"
	kuwoTokenSize = 32
)

type kuwoProvider struct {
	httpClient *http.Client
}

func newKuwoProvider(httpClient *http.Client) Provider {
	return &kuwoProvider{httpClient: httpClient}
}

func (*kuwoProvider) Source() string { return "kuwo" }

func (p *kuwoProvider) Search(ctx context.Context, query string, limit int) ([]domain.MusicTrack, error) {
	request, err := newKuwoRequest(ctx, "api/www/search/searchMusicBykeyWord", url.Values{
		"key": {query}, "pn": {"1"}, "rn": {strconv.Itoa(limit)}, "httpsStatus": {"1"},
	})
	if err != nil {
		return nil, newError(http.StatusBadGateway, "music search failed", err)
	}
	responseBody, err := doJSON(p.httpClient, request)
	if err != nil {
		return nil, err
	}
	var response kuwoSearchEnvelope
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, newError(http.StatusBadGateway, "invalid music search response", err)
	}
	if response.Code != http.StatusOK || response.Data == nil {
		// Kuwo periodically changes the anti-bot contract of the web endpoint.
		// The legacy public search endpoint remains available and does not require
		// credentials, so use it only when the primary endpoint rejects the request.
		return p.searchLegacy(ctx, query, limit)
	}
	tracks := make([]domain.MusicTrack, 0, len(response.Data.List))
	for _, item := range response.Data.List {
		id := item.ID.String()
		name := strings.TrimSpace(item.Name)
		if id == "" || name == "" {
			continue
		}
		tracks = append(tracks, domain.MusicTrack{
			ID: id, Name: name, Artists: kuwoArtistNames(item.Artist), Album: strings.TrimSpace(item.Album),
			PictureID: id, LyricID: id, Source: p.Source(), DurationMS: item.Duration.Milliseconds(),
			TrackNumber: item.TrackNumber.Int(), DiscNumber: item.DiscNumber.Int(),
		})
	}
	return tracks, nil
}

func (p *kuwoProvider) searchLegacy(ctx context.Context, query string, limit int) ([]domain.MusicTrack, error) {
	values := url.Values{"all": {query}, "ft": {"music"}, "itemset": {"web_2013"}, "client": {"kt"}, "pn": {"0"}, "rn": {strconv.Itoa(limit)}, "rformat": {"json"}, "encoding": {"utf8"}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, serviceURL(kuwoLegacyURL, "r.s", values), nil)
	if err != nil {
		return nil, newError(http.StatusBadGateway, "music search failed", err)
	}
	request.Header.Set("User-Agent", kuwoUserAgent)
	responseBody, err := doJSON(p.httpClient, request)
	if err != nil {
		return nil, err
	}
	// This endpoint historically labels its JSON with single quotes. Kuwo's
	// payload fields do not contain escaped apostrophes in this response format.
	var response kuwoLegacySearchEnvelope
	if err := json.Unmarshal([]byte(strings.ReplaceAll(string(responseBody), "'", `"`)), &response); err != nil {
		return nil, newError(http.StatusBadGateway, "invalid music search response", err)
	}
	tracks := make([]domain.MusicTrack, 0, len(response.Results))
	for _, item := range response.Results {
		id := strings.TrimSpace(item.ID)
		name := strings.TrimSpace(item.Name)
		if id == "" || name == "" {
			continue
		}
		name = html.UnescapeString(name)
		tracks = append(tracks, domain.MusicTrack{ID: id, Name: name, Artists: kuwoArtistNames(html.UnescapeString(item.Artist)), Album: html.UnescapeString(strings.TrimSpace(item.Album)), PictureID: id, LyricID: id, Source: p.Source(), DurationMS: parseLegacyDuration(item.Duration), TrackNumber: parseLegacyInt(item.TrackNumber), DiscNumber: parseLegacyInt(item.DiscNumber)})
	}
	if len(tracks) == 0 {
		return nil, newError(http.StatusBadGateway, "music search failed", nil)
	}
	return tracks, nil
}

func (p *kuwoProvider) CoverURL(ctx context.Context, id string) (string, error) {
	request, err := newKuwoRequest(ctx, "api/www/music/musicInfo", url.Values{
		"mid": {id}, "httpsStatus": {"1"},
	})
	if err != nil {
		return "", newError(http.StatusBadGateway, "music cover request failed", err)
	}
	responseBody, err := doJSON(p.httpClient, request)
	if err != nil {
		return "", err
	}
	var response kuwoSongEnvelope
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return "", newError(http.StatusBadGateway, "invalid music cover response", err)
	}
	if response.Code != http.StatusOK || response.Data == nil {
		return "", newError(http.StatusBadGateway, "music cover request failed", nil)
	}
	return validateCoverURL(response.Data.PictureURL())
}

func (p *kuwoProvider) Lyrics(ctx context.Context, id string) (string, string, error) {
	request, err := newKuwoRequest(ctx, "newh5/singles/songinfoandlrc", url.Values{
		"musicId": {id}, "mid": {id}, "type": {"music"}, "httpsStatus": {"1"}, "plat": {"web_www"},
	})
	if err != nil {
		return "", "", newError(http.StatusBadGateway, "music lyrics request failed", err)
	}
	responseBody, err := doJSON(p.httpClient, request)
	if err != nil {
		return "", "", err
	}
	var response kuwoLyricsEnvelope
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return "", "", newError(http.StatusBadGateway, "invalid music lyrics response", err)
	}
	if response.Data == nil {
		return "", "", newError(http.StatusBadGateway, "music lyrics request failed", nil)
	}
	lyrics, err := formatKuwoLyrics(response.Data.Lyrics)
	if err != nil {
		return "", "", newError(http.StatusBadGateway, "invalid music lyrics response", err)
	}
	return lyrics, "", nil
}

func newKuwoRequest(ctx context.Context, route string, values url.Values) (*http.Request, error) {
	token, err := generateKuwoToken()
	if err != nil {
		return nil, err
	}
	shaSum := sha1.Sum([]byte(token))
	shaText := hex.EncodeToString(shaSum[:])
	crossSum := md5.Sum([]byte(shaText)) // #nosec G401 -- required by Kuwo's request protocol.

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, serviceURL(kuwoBaseURL, route, values), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", kuwoUserAgent)
	request.Header.Set("Referer", kuwoBaseURL+"/")
	request.Header.Set("Cross", hex.EncodeToString(crossSum[:]))
	request.Header.Set("Cookie", "Hm_token="+token)
	return request, nil
}

func generateKuwoToken() (string, error) {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	random := make([]byte, kuwoTokenSize)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	token := make([]byte, len(random))
	for index, value := range random {
		token[index] = alphabet[int(value)%len(alphabet)]
	}
	return string(token), nil
}

type kuwoSearchEnvelope struct {
	Code int `json:"code"`
	Data *struct {
		List []kuwoSearchItem `json:"list"`
	} `json:"data"`
}

type kuwoSearchItem struct {
	ID          flexibleString `json:"rid"`
	Name        string         `json:"name"`
	Artist      string         `json:"artist"`
	Album       string         `json:"album"`
	Duration    kuwoDuration   `json:"duration"`
	TrackNumber kuwoNumber     `json:"track"`
	DiscNumber  kuwoNumber     `json:"disc"`
}

type kuwoLegacySearchEnvelope struct {
	Results []kuwoLegacySearchItem `json:"abslist"`
}

type kuwoLegacySearchItem struct {
	ID          string `json:"DC_TARGETID"`
	Name        string `json:"SONGNAME"`
	Artist      string `json:"ARTIST"`
	Album       string `json:"ALBUM"`
	Duration    string `json:"DURATION"`
	TrackNumber string `json:"TRACK"`
	DiscNumber  string `json:"DISCNUMBER"`
}

func parseLegacyInt(value string) int { n, _ := strconv.Atoi(strings.TrimSpace(value)); return n }

func parseLegacyDuration(value string) int64 {
	seconds, _ := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return int64(math.Round(seconds * 1000))
}

type kuwoSongEnvelope struct {
	Code int           `json:"code"`
	Data *kuwoSongData `json:"data"`
}

type kuwoSongData struct {
	Picture    string `json:"pic"`
	Picture120 string `json:"pic120"`
	Picture300 string `json:"pic300"`
	Picture500 string `json:"pic500"`
	AlbumPic   string `json:"albumpic"`
}

func (value kuwoSongData) PictureURL() string {
	for _, candidate := range []string{value.Picture500, value.Picture300, value.Picture120, value.Picture, value.AlbumPic} {
		if strings.TrimSpace(candidate) != "" {
			return strings.TrimSpace(candidate)
		}
	}
	return ""
}

type kuwoLyricsEnvelope struct {
	Data *struct {
		Lyrics []kuwoLyricLine `json:"lrclist"`
	} `json:"data"`
}

type kuwoLyricLine struct {
	Time kuwoNumber `json:"time"`
	Text string     `json:"lineLyric"`
}

type kuwoNumber float64

type kuwoDuration int64

func (value *kuwoNumber) UnmarshalJSON(data []byte) error {
	if string(data) == "null" || string(data) == `""` {
		*value = 0
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err == nil {
		parsed, parseErr := strconv.ParseFloat(number.String(), 64)
		if parseErr != nil {
			return parseErr
		}
		*value = kuwoNumber(parsed)
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("Kuwo number must be a string or number: %w", err)
	}
	parsed, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil {
		return err
	}
	*value = kuwoNumber(parsed)
	return nil
}

func (value kuwoNumber) Int() int {
	if value <= 0 || float64(value) > float64(math.MaxInt) {
		return 0
	}
	return int(value)
}

func (value kuwoNumber) Milliseconds() int64 {
	if value <= 0 || float64(value) > float64(math.MaxInt64/1000) {
		return 0
	}
	return int64(math.Round(float64(value) * 1000))
}

func (value *kuwoDuration) UnmarshalJSON(data []byte) error {
	var number kuwoNumber
	if err := json.Unmarshal(data, &number); err == nil {
		*value = kuwoDuration(number.Milliseconds())
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("Kuwo duration must be a string or number: %w", err)
	}
	parts := strings.Split(strings.TrimSpace(text), ":")
	if len(parts) < 2 || len(parts) > 3 {
		return fmt.Errorf("invalid Kuwo duration %q", text)
	}
	var seconds float64
	for _, part := range parts {
		component, err := strconv.ParseFloat(part, 64)
		if err != nil || component < 0 {
			return fmt.Errorf("invalid Kuwo duration %q", text)
		}
		seconds = seconds*60 + component
	}
	if seconds > float64(math.MaxInt64/1000) {
		return fmt.Errorf("invalid Kuwo duration %q", text)
	}
	*value = kuwoDuration(math.Round(seconds * 1000))
	return nil
}

func (value kuwoDuration) Milliseconds() int64 { return int64(value) }

func kuwoArtistNames(value string) []string {
	return compactStrings(strings.Split(value, "&"))
}

func formatKuwoLyrics(lines []kuwoLyricLine) (string, error) {
	formatted := make([]string, 0, len(lines))
	for _, line := range lines {
		seconds := float64(line.Time)
		if seconds < 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
			return "", fmt.Errorf("invalid lyric timestamp %v", seconds)
		}
		if strings.TrimSpace(line.Text) == "" {
			continue
		}
		centiseconds := int64(math.Round(seconds * 100))
		minutes := centiseconds / 6000
		remaining := centiseconds % 6000
		formatted = append(formatted, fmt.Sprintf("[%02d:%02d.%02d]%s", minutes, remaining/100, remaining%100, line.Text))
	}
	return strings.Join(formatted, "\n"), nil
}
