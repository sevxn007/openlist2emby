package metadata

import (
	"context"
	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

type TMDBClient interface {
	Search(context.Context, string, string, string) ([]domain.MediaSearchResult, error)
	Detail(context.Context, string, string, int64, *int, string) (domain.MediaDetail, error)
}
type TMDBAdapter struct{ Client TMDBClient }

func (p TMDBAdapter) Search(ctx context.Context, c Credentials, q, l string) ([]domain.MediaSearchResult, error) {
	return p.Client.Search(ctx, c.Key, q, l)
}
func (p TMDBAdapter) Detail(ctx context.Context, c Credentials, k string, id int64, n *int, l string) (domain.MediaDetail, error) {
	return p.Client.Detail(ctx, c.Key, k, id, n, l)
}
func (p TMDBAdapter) Credits(ctx context.Context, c Credentials, q domain.MediaCreditsQuery, l string) ([]domain.MediaPerson, error) {
	if q.ExternalID > 0 {
		q.TMDBID = q.ExternalID
	}
	q.Source = ""
	q.ExternalID = 0
	if provider, ok := p.Client.(interface {
		Credits(context.Context, string, domain.MediaCreditsQuery, string) ([]domain.MediaPerson, error)
	}); ok {
		return provider.Credits(ctx, c.Key, q, l)
	}
	return []domain.MediaPerson{}, nil
}

func (p TMDBAdapter) SeasonEpisodeCredits(ctx context.Context, c Credentials, q domain.MediaCreditsQuery, l string) (map[int][]domain.MediaPerson, error) {
	if q.ExternalID > 0 {
		q.TMDBID = q.ExternalID
	}
	q.Source = ""
	q.ExternalID = 0
	if provider, ok := p.Client.(interface {
		SeasonEpisodeCredits(context.Context, string, domain.MediaCreditsQuery, string) (map[int][]domain.MediaPerson, error)
	}); ok {
		return provider.SeasonEpisodeCredits(ctx, c.Key, q, l)
	}
	return map[int][]domain.MediaPerson{}, nil
}
