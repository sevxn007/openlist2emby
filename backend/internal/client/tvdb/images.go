package tvdb

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/metadata"
)

func (s *Service) Images(ctx context.Context, c metadata.Credentials, q domain.MediaImageQuery) ([]domain.MediaImage, error) {
	path := "/movies/"
	if q.Type == "tv" {
		path = "/series/"
	}
	var parent struct {
		ID       int64
		Artworks []artwork
		Seasons  []struct {
			ID     int64
			Number int
			Type   struct{ Type string }
		}
	}
	if _, err := s.get(ctx, c, path+strconv.FormatInt(q.ID, 10)+"/extended", &parent); err != nil {
		return nil, err
	}
	if parent.ID != q.ID {
		return nil, metadata.ErrUnavailable
	}
	if q.Season != nil {
		var seasonID int64
		for _, season := range parent.Seasons {
			if season.Number == *q.Season && season.Type.Type == "official" {
				seasonID = season.ID
				break
			}
		}
		if seasonID == 0 {
			return []domain.MediaImage{}, nil
		}
		var season struct {
			ID        int64
			Image     string
			ImageType int
			Artwork   []artwork
			Episodes  []struct {
				ID     int64
				Number int
			}
		}
		if _, err := s.get(ctx, c, fmt.Sprintf("/seasons/%d/extended", seasonID), &season); err != nil {
			return nil, err
		}
		if season.ID != seasonID {
			return nil, metadata.ErrUnavailable
		}
		if q.Episode != nil {
			var episodeID int64
			for _, episode := range season.Episodes {
				if episode.Number == *q.Episode {
					episodeID = episode.ID
					break
				}
			}
			if episodeID == 0 {
				return []domain.MediaImage{}, nil
			}
			var episode struct {
				ID    int64
				Image string
			}
			if _, err := s.get(ctx, c, fmt.Sprintf("/episodes/%d/extended", episodeID), &episode); err != nil {
				return nil, err
			}
			if episode.ID != episodeID {
				return nil, metadata.ErrUnavailable
			}
			if q.ImageType == "backdrop" || episode.Image == "" {
				return []domain.MediaImage{}, nil
			}
			return []domain.MediaImage{{URL: episode.Image, ThumbnailURL: episode.Image}}, nil
		}
		if season.Image != "" && season.ImageType != 0 {
			season.Artwork = append(season.Artwork, artwork{Type: season.ImageType, Image: season.Image})
		}
		return s.artworkImages(ctx, c, season.Artwork, q.ImageType)
	}
	return s.artworkImages(ctx, c, parent.Artworks, q.ImageType)
}

func (s *Service) artworkImages(ctx context.Context, c metadata.Credentials, artworks []artwork, imageType string) ([]domain.MediaImage, error) {
	types := []struct {
		ID   int
		Name string
	}{}
	if _, err := s.get(ctx, c, "/artwork/types", &types); err != nil {
		return nil, err
	}
	names := make(map[int]string, len(types))
	for _, kind := range types {
		names[kind.ID] = strings.ToLower(kind.Name)
	}
	out := []domain.MediaImage{}
	for _, artwork := range artworks {
		name := names[artwork.Type]
		matched := strings.Contains(name, "poster")
		if imageType == "backdrop" {
			matched = strings.Contains(name, "background") || strings.Contains(name, "fanart")
		}
		if matched {
			out = append(out, domain.MediaImage{URL: artwork.Image, ThumbnailURL: artwork.Thumbnail, Width: artwork.Width, Height: artwork.Height, Language: artwork.Language})
		}
	}
	return out, nil
}
