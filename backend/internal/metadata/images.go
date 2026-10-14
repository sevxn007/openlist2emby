package metadata

import (
	"context"
	"net/url"
	"strings"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

// ImageProvider is an optional capability independent of metadata matching.
type ImageProvider interface {
	Images(context.Context, Credentials, domain.MediaImageQuery) ([]domain.MediaImage, error)
}

func (r *Registry) Images(ctx context.Context, settings domain.Settings, q domain.MediaImageQuery) ([]domain.MediaImage, error) {
	p := r.providers[q.Source]
	if p == nil {
		return nil, ErrInvalidSource
	}
	c := credentials(settings, q.Source)
	if c.Key == "" {
		return nil, ErrNotConfigured
	}
	provider, ok := p.(ImageProvider)
	if !ok {
		return nil, ErrUnavailable
	}
	images, err := provider.Images(ctx, c, q)
	if err != nil {
		return nil, safeProviderError(err)
	}
	result := make([]domain.MediaImage, 0, len(images))
	seen := map[string]bool{}
	for _, img := range images {
		key := img.URL + "\x00" + img.Language
		if !validImageURL(img.URL) || seen[key] {
			continue
		}
		seen[key] = true
		if !validImageURL(img.ThumbnailURL) {
			img.ThumbnailURL = img.URL
		}
		img.Source, img.ImageType = q.Source, q.ImageType
		if img.Width < 0 {
			img.Width = 0
		}
		if img.Height < 0 {
			img.Height = 0
		}
		result = append(result, img)
	}
	return result, nil
}

func validImageURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil && !strings.ContainsAny(value, "\r\n")
}

func (p TMDBAdapter) Images(ctx context.Context, c Credentials, q domain.MediaImageQuery) ([]domain.MediaImage, error) {
	client, ok := p.Client.(interface {
		Images(context.Context, string, domain.MediaImageQuery) ([]domain.MediaImage, error)
	})
	if !ok {
		return nil, ErrUnavailable
	}
	return client.Images(ctx, c.Key, q)
}
