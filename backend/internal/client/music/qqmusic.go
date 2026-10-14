package music

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

type qqMusicProvider struct {
	httpClient *http.Client
}

func newQQMusicProvider(httpClient *http.Client) Provider {
	return &qqMusicProvider{httpClient: httpClient}
}

func (*qqMusicProvider) Source() string { return "qmusic" }

func (p *qqMusicProvider) Search(ctx context.Context, query string, limit int) ([]domain.MusicTrack, error) {
	target := "https://c.y.qq.com/soso/fcgi-bin/client_search_cp?" + url.Values{
		"p": {"1"}, "n": {strconv.Itoa(limit)}, "w": {query}, "format": {"json"},
	}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, newError(http.StatusBadGateway, "music search failed", err)
	}
	request.Header.Set("Referer", "https://y.qq.com/")
	responseBody, err := doJSON(p.httpClient, request)
	if err != nil {
		return nil, err
	}
	var response qqSearchEnvelope
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, newError(http.StatusBadGateway, "invalid music search response", err)
	}
	if response.Code != 0 {
		return nil, newError(http.StatusBadGateway, "music search failed", nil)
	}
	tracks := make([]domain.MusicTrack, 0, len(response.Data.Songs.List))
	for _, item := range response.Data.Songs.List {
		if strings.TrimSpace(item.MID) == "" || strings.TrimSpace(item.Name) == "" {
			continue
		}
		tracks = append(tracks, domain.MusicTrack{
			ID: item.MID, Name: strings.TrimSpace(item.Name), Artists: qqArtistNames(item.Singers),
			Album: strings.TrimSpace(item.AlbumName), PictureID: strings.TrimSpace(item.AlbumMID),
			LyricID: item.MID, Source: p.Source(), DurationMS: item.Interval * 1000,
			TrackNumber: item.TrackNumber, DiscNumber: item.DiscNumber,
		})
	}
	return tracks, nil
}

func (*qqMusicProvider) CoverURL(_ context.Context, id string) (string, error) {
	return validateCoverURL("https://y.qq.com/music/photo_new/T002R500x500M000" + url.PathEscape(id) + ".jpg")
}

func (p *qqMusicProvider) Lyrics(ctx context.Context, id string) (string, string, error) {
	target := "https://c.y.qq.com/lyric/fcgi-bin/fcg_query_lyric_new.fcg?" + url.Values{
		"songmid": {id}, "format": {"json"}, "nobase64": {"0"}, "g_tk": {"5381"},
	}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", "", newError(http.StatusBadGateway, "music lyrics request failed", err)
	}
	request.Header.Set("Referer", "https://y.qq.com/")
	responseBody, err := doJSON(p.httpClient, request)
	if err != nil {
		return "", "", err
	}
	var response struct {
		Code  int    `json:"code"`
		Lyric string `json:"lyric"`
		Trans string `json:"trans"`
	}
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return "", "", newError(http.StatusBadGateway, "invalid music lyrics response", err)
	}
	if response.Code != 0 {
		return "", "", newError(http.StatusBadGateway, "music lyrics request failed", nil)
	}
	lyrics, err := decodeQQMusicText(response.Lyric)
	if err != nil {
		return "", "", newError(http.StatusBadGateway, "invalid music lyrics response", err)
	}
	translated, err := decodeQQMusicText(response.Trans)
	if err != nil {
		return "", "", newError(http.StatusBadGateway, "invalid music lyrics response", err)
	}
	return lyrics, translated, nil
}

type qqSearchEnvelope struct {
	Code int `json:"code"`
	Data struct {
		Songs struct {
			List []qqSearchItem `json:"list"`
		} `json:"song"`
	} `json:"data"`
}

type qqSearchItem struct {
	MID         string     `json:"songmid"`
	Name        string     `json:"songname"`
	Singers     []qqArtist `json:"singer"`
	AlbumName   string     `json:"albumname"`
	AlbumMID    string     `json:"albummid"`
	Interval    int64      `json:"interval"`
	TrackNumber int        `json:"albumindex"`
	DiscNumber  int        `json:"disc"`
}

type qqArtist struct {
	Name string `json:"name"`
}

func qqArtistNames(values []qqArtist) []string {
	names := make([]string, 0, len(values))
	for _, value := range values {
		names = append(names, value.Name)
	}
	return compactStrings(names)
}

func decodeQQMusicText(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}
