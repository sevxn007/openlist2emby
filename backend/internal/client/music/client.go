package music

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

const (
	maxJSONBodySize   = int64(2 << 20)
	maxCoverImageSize = int64(10 << 20)
	searchAttempts    = 3
	searchRetryDelay  = 150 * time.Millisecond
)

// Provider adapts one upstream music service to the Manager music contract.
// Implementations own source-specific requests and response mapping; Client
// owns routing, retries, concurrent detail resolution, and cover downloads.
type Provider interface {
	Source() string
	Search(context.Context, string, int) ([]domain.MusicTrack, error)
	CoverURL(context.Context, string) (string, error)
	Lyrics(context.Context, string) (string, string, error)
}

type Client struct {
	httpClient  *http.Client
	providers   map[string]Provider
	sourceOrder []string
}

type Error struct {
	status  int
	message string
	err     error
}

func (e *Error) Error() string         { return e.message }
func (e *Error) Unwrap() error         { return e.err }
func (e *Error) StatusCode() int       { return e.status }
func (e *Error) PublicMessage() string { return e.message }

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	client := &Client{
		httpClient: httpClient,
		providers:  make(map[string]Provider),
	}
	client.register(newNetEaseProvider(httpClient))
	client.register(newQQMusicProvider(httpClient))
	client.register(newKuwoProvider(httpClient))
	return client
}

func (c *Client) register(provider Provider) {
	source := strings.ToLower(strings.TrimSpace(provider.Source()))
	if source == "" {
		panic("music provider source is empty")
	}
	if _, exists := c.providers[source]; !exists {
		c.sourceOrder = append(c.sourceOrder, source)
	}
	c.providers[source] = provider
}

// Sources returns registered sources in stable preference order. The first
// source is the default and wins equal-score smart matches.
func (c *Client) Sources() []string {
	return append([]string(nil), c.sourceOrder...)
}

func (c *Client) Search(ctx context.Context, query, source string, limit int) ([]domain.MusicTrack, error) {
	provider, err := c.provider(source)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	for attempt := 0; attempt < searchAttempts; attempt++ {
		tracks, searchErr := provider.Search(ctx, strings.TrimSpace(query), limit)
		if searchErr != nil || len(tracks) > 0 || attempt == searchAttempts-1 {
			return tracks, searchErr
		}
		select {
		case <-ctx.Done():
			return nil, newError(http.StatusBadGateway, "music search failed", ctx.Err())
		case <-time.After(searchRetryDelay * time.Duration(attempt+1)):
		}
	}
	return nil, nil
}

func (c *Client) Resolve(ctx context.Context, source, pictureID, lyricID string) (domain.MusicAssets, error) {
	detail, err := c.Detail(ctx, source, pictureID, lyricID)
	if err != nil {
		return domain.MusicAssets{}, err
	}
	assets := domain.MusicAssets{Detail: detail}
	if pictureID == "" {
		return assets, nil
	}
	assets.CoverData, assets.CoverMIME, err = c.downloadCover(ctx, detail.CoverURL)
	if err != nil {
		return domain.MusicAssets{}, err
	}
	return assets, nil
}

func (c *Client) Detail(ctx context.Context, source, pictureID, lyricID string) (domain.MusicDetail, error) {
	provider, err := c.provider(source)
	if err != nil {
		return domain.MusicDetail{}, err
	}
	var detail domain.MusicDetail
	var coverErr error
	var lyricErr error
	var wait sync.WaitGroup
	if pictureID != "" {
		wait.Add(1)
		go func() {
			defer wait.Done()
			detail.CoverURL, coverErr = provider.CoverURL(ctx, pictureID)
		}()
	}
	if lyricID != "" {
		wait.Add(1)
		go func() {
			defer wait.Done()
			detail.Lyrics, detail.TranslatedLyrics, lyricErr = provider.Lyrics(ctx, lyricID)
		}()
	}
	wait.Wait()
	if coverErr != nil {
		return domain.MusicDetail{}, coverErr
	}
	if lyricErr != nil {
		return domain.MusicDetail{}, lyricErr
	}
	return detail, nil
}

func (c *Client) provider(source string) (Provider, error) {
	source = strings.ToLower(strings.TrimSpace(source))
	if source == "" && len(c.sourceOrder) > 0 {
		source = c.sourceOrder[0]
	}
	provider, ok := c.providers[source]
	if !ok {
		return nil, newError(http.StatusBadRequest, "music source is not supported", nil)
	}
	return provider, nil
}

func (c *Client) downloadCover(ctx context.Context, coverURL string) ([]byte, string, error) {
	coverURL, err := validateCoverURL(coverURL)
	if err != nil {
		return nil, "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, coverURL, nil)
	if err != nil {
		return nil, "", newError(http.StatusBadGateway, "music cover download failed", err)
	}
	request.Header.Set("Accept", "image/*")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, "", newError(http.StatusBadGateway, "music cover download failed", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, "", newError(mapStatus(response.StatusCode), "music cover download failed", nil)
	}
	data, err := readBody(response.Body, maxCoverImageSize)
	if err != nil {
		return nil, "", newError(http.StatusBadGateway, "music cover download failed", err)
	}
	contentType := http.DetectContentType(data)
	if !strings.HasPrefix(contentType, "image/") {
		return nil, "", newError(http.StatusBadGateway, "music service returned an invalid cover image", nil)
	}
	return data, contentType, nil
}

func doJSON(httpClient *http.Client, request *http.Request) ([]byte, error) {
	request.Header.Set("Accept", "application/json")
	response, err := httpClient.Do(request)
	if err != nil {
		return nil, newError(http.StatusBadGateway, "music service is unavailable", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, newError(mapStatus(response.StatusCode), "music service request failed", nil)
	}
	body, err := readBody(response.Body, maxJSONBodySize)
	if err != nil {
		return nil, newError(http.StatusBadGateway, "music service response is invalid", err)
	}
	return body, nil
}

func serviceURL(baseURL, route string, values url.Values) string {
	target := strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(route, "/")
	if encoded := values.Encode(); encoded != "" {
		target += "?" + encoded
	}
	return target
}

func validateCoverURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", newError(http.StatusBadGateway, "music service returned an invalid cover URL", err)
	}
	return parsed.String(), nil
}

func readBody(reader io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errors.New("response exceeds size limit")
	}
	return body, nil
}

func compactStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func mapStatus(status int) int {
	if status == http.StatusTooManyRequests {
		return status
	}
	return http.StatusBadGateway
}

func newError(status int, message string, err error) error {
	return &Error{status: status, message: message, err: err}
}
