package medialibrary

import (
	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"testing"
)

func TestTMDBRefreshReplacesPreviousArtworkSources(t *testing.T) {
	for _, kind := range []string{"series", "season", "episode"} {
		t.Run(kind, func(t *testing.T) {
			input := metadataInputFromItem(Item{Name: "Old title", ImageSourceURL: "https://image.example/old.jpg", BackdropSourceURL: "https://image.example/old-backdrop.jpg"})
			detail := domain.MediaDetail{PosterURL: "https://image.example/new.jpg", BackdropURL: "https://image.example/new-backdrop.jpg"}
			switch kind {
			case "series":
				applyTMDBDetail(&input, detail, 123)
			case "season":
				applyTMDBSeasonDetail(&input, detail)
			case "episode":
				applyTMDBEpisodeDetail(&input, domain.MediaEpisode{StillURL: detail.PosterURL})
			}
			got, err := normalizeMetadata(input)
			if err != nil {
				t.Fatal(err)
			}
			if got.ImageURL != detail.PosterURL {
				t.Fatalf("image = %q", got.ImageURL)
			}
			if kind != "episode" && got.BackdropURL != detail.BackdropURL {
				t.Fatalf("backdrop = %q", got.BackdropURL)
			}
		})
	}
}

func TestTMDBRefreshDoesNotRestoreMissingArtworkFromOldSource(t *testing.T) {
	for _, apply := range []func(*MetadataInput, domain.MediaDetail){
		func(input *MetadataInput, detail domain.MediaDetail) { applyTMDBDetail(input, detail, 123) },
		applyTMDBSeasonDetail,
	} {
		input := metadataInputFromItem(Item{Name: "Title", ImageSourceURL: "https://image.example/old.jpg", BackdropSourceURL: "https://image.example/old-backdrop.jpg"})
		apply(&input, domain.MediaDetail{})
		got, err := normalizeMetadata(input)
		if err != nil {
			t.Fatal(err)
		}
		if got.ImageURL != "" || got.BackdropURL != "" {
			t.Fatalf("old sources restored: %#v", got)
		}
	}
}
