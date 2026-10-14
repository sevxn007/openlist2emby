package metadata

import (
	"context"
	"errors"
	"testing"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

type imageProvider struct {
	fakeProvider
	images []domain.MediaImage
}

func (p *imageProvider) Images(context.Context, Credentials, domain.MediaImageQuery) ([]domain.MediaImage, error) {
	return p.images, p.err
}
func TestImagesFiltersInvalidAndDuplicateCandidates(t *testing.T) {
	r := NewRegistry()
	p := &imageProvider{images: []domain.MediaImage{
		{URL: "https://images.example/zh.jpg", Language: "zh", ThumbnailURL: "javascript:bad", Width: 800},
		{URL: "https://images.example/zh.jpg", Language: "zh"},
		{URL: "file:///tmp/image.jpg"},
		{URL: "https://user:secret@images.example/image.jpg"},
		{URL: "https://images.example/en.jpg", Language: "en"},
		{URL: "https://images.example/zh.jpg", Language: "en"},
	}}
	_ = r.Register(TMDB, p)
	q := domain.MediaImageQuery{Source: TMDB, Type: "movie", ID: 1, ImageType: "primary"}
	images, err := r.Images(t.Context(), domain.Settings{TMDBAPIKey: "synthetic"}, q)
	if err != nil || len(images) != 3 || images[0].ThumbnailURL != images[0].URL || images[1].Language != "en" || images[2].Language != "en" || images[0].Source != TMDB {
		t.Fatalf("images=%#v error=%v", images, err)
	}
	_, err = r.Images(t.Context(), domain.Settings{}, q)
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatal(err)
	}
	p.err = errors.New("synthetic-secret")
	_, err = r.Images(t.Context(), domain.Settings{TMDBAPIKey: "synthetic"}, q)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	p.err = nil
	p.images = nil
	images, err = r.Images(t.Context(), domain.Settings{TMDBAPIKey: "synthetic"}, q)
	if err != nil || images == nil || len(images) != 0 {
		t.Fatalf("empty=%#v err=%v", images, err)
	}
}
