package openlist

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
)

type MediaLibraryClient struct {
	client  *Client
	baseURL string
	token   string
}

func NewMediaLibraryClient(client *Client, baseURL, token string) *MediaLibraryClient {
	if client == nil {
		client = NewClient(nil)
	}
	return &MediaLibraryClient{
		client:  client,
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:   token,
	}
}

func (c *MediaLibraryClient) ListDirectory(ctx context.Context, directory string, refresh bool) ([]medialibrary.RemoteEntry, error) {
	directory, err := normalizeOpenListPath(directory)
	if err != nil {
		return nil, err
	}
	var response struct {
		Content []struct {
			Name     string `json:"name"`
			Size     int64  `json:"size"`
			IsDir    bool   `json:"is_dir"`
			Modified string `json:"modified"`
			Thumb    string `json:"thumb"`
		} `json:"content"`
	}
	if err := c.post(ctx, "/api/fs/list", map[string]any{
		"path": directory, "page": 1, "per_page": 0, "refresh": refresh,
	}, &response, ""); err != nil {
		return nil, err
	}
	entries := make([]medialibrary.RemoteEntry, 0, len(response.Content))
	for _, entry := range response.Content {
		thumbnail := strings.TrimSpace(entry.Thumb)
		if thumbnail != "" {
			if resolved, err := resolveDownloadURL(c.baseURL, thumbnail); err == nil {
				thumbnail = resolved
			} else {
				thumbnail = ""
			}
		}
		entries = append(entries, medialibrary.RemoteEntry{
			Name:       entry.Name,
			IsDir:      entry.IsDir,
			Size:       entry.Size,
			ModifiedAt: parseOpenListTime(entry.Modified),
			Thumbnail:  thumbnail,
		})
	}
	return entries, nil
}

func (c *MediaLibraryClient) ResolveDownloadURL(ctx context.Context, objectPath, userAgent string) (string, error) {
	objectPath, err := normalizeObjectPath(objectPath)
	if err != nil {
		return "", err
	}
	var response struct {
		IsDir  bool   `json:"is_dir"`
		RawURL string `json:"raw_url"`
	}
	if err := c.post(ctx, "/api/fs/get", map[string]string{"path": objectPath}, &response, userAgent); err != nil {
		return "", err
	}
	if response.IsDir || strings.TrimSpace(response.RawURL) == "" {
		return "", &Error{status: http.StatusBadGateway, message: "invalid openlist response"}
	}
	return resolveDownloadURL(c.baseURL, response.RawURL)
}

func (c *MediaLibraryClient) post(ctx context.Context, endpoint string, payload any, result any, userAgent string) error {
	if c == nil || c.client == nil {
		return &Error{status: http.StatusBadGateway, message: "openlist media library client is not initialized"}
	}
	if c.baseURL == "" {
		return &Error{status: http.StatusBadGateway, message: "openlist URL is empty"}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return &Error{status: http.StatusBadGateway, message: "openlist request failed", err: err}
	}
	return c.client.requestJSON(ctx, http.MethodPost, c.baseURL, endpoint, c.token, body, result, userAgent)
}

func normalizeOpenListPath(value string) (string, error) {
	if value == "" || !strings.HasPrefix(value, "/") {
		return "", errors.New("OpenList path must be absolute")
	}
	return path.Clean(value), nil
}

func normalizeObjectPath(value string) (string, error) {
	value, err := normalizeOpenListPath(value)
	if err != nil {
		return "", err
	}
	if value == "/" {
		return "", errors.New("OpenList object path must not be the root")
	}
	return value, nil
}

func parseOpenListTime(value string) *time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return nil
	}
	return &parsed
}

var _ medialibrary.OpenList = (*MediaLibraryClient)(nil)
