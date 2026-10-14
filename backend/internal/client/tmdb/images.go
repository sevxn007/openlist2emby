package tmdbclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/metadata"
)

func (s *Service) Images(ctx context.Context, key string, q domain.MediaImageQuery) ([]domain.MediaImage, error) {
	if strings.TrimSpace(key) == "" {
		return nil, metadata.ErrNotConfigured
	}
	if q.ImageType == "backdrop" && q.Season != nil {
		return []domain.MediaImage{}, nil
	}
	endpoint := fmt.Sprintf("/%s/%d", q.Type, q.ID)
	if q.Season != nil {
		endpoint += fmt.Sprintf("/season/%d", *q.Season)
	}
	if q.Episode != nil {
		endpoint += fmt.Sprintf("/episode/%d", *q.Episode)
	}
	base := s.imageAPIBaseURL
	if base == "" {
		base = "https://api.themoviedb.org/3"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+endpoint+"/images?api_key="+url.QueryEscape(key), nil)
	if err != nil {
		return nil, metadata.ErrUnavailable
	}
	resp, err := (&http.Client{Timeout: s.timeout}).Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, metadata.ErrUnavailable
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case 401, 403:
		return nil, metadata.ErrAuthentication
	case 404:
		return nil, domain.ErrNotFound
	case 429:
		return nil, metadata.ErrRateLimited
	}
	if resp.StatusCode != 200 {
		return nil, metadata.ErrUnavailable
	}
	type image struct {
		Path     string `json:"file_path"`
		Width    int    `json:"width"`
		Height   int    `json:"height"`
		Language string `json:"iso_639_1"`
	}
	var body struct{ Posters, Backdrops, Stills []image }
	payload, err := io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
	if err != nil || len(payload) > 16<<20 || json.Unmarshal(payload, &body) != nil {
		return nil, metadata.ErrUnavailable
	}
	images := body.Posters
	if q.ImageType == "backdrop" {
		images = body.Backdrops
	} else if q.Episode != nil {
		images = body.Stills
	}
	out := make([]domain.MediaImage, 0, len(images))
	for _, image := range images {
		if !strings.HasPrefix(image.Path, "/") || strings.HasPrefix(image.Path, "//") {
			continue
		}
		out = append(out, domain.MediaImage{URL: "https://image.tmdb.org/t/p/original" + image.Path, ThumbnailURL: imageBaseURL + image.Path, Width: image.Width, Height: image.Height, Language: image.Language})
	}
	return out, nil
}
