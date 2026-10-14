package emby

import (
	"context"
	"strconv"
	"testing"

	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
)

func TestMusicDTOIncludesEmbyAlbumArtworkAndArtistRelationships(t *testing.T) {
	artistID := int64(31)
	albumID := int64(32)
	server := &Server{serverID: "server"}
	parents := map[int64]medialibrary.Item{
		artistID: {ID: artistID, Kind: medialibrary.KindMusicArtist, Name: "Artist"},
		albumID: {
			ID: albumID, ParentID: &artistID, Kind: medialibrary.KindMusicAlbum,
			Name: "Album", ImageURL: "/album.jpg", ImageTag: "album-cover-tag",
		},
	}
	item := medialibrary.Item{
		ID: 33, ParentID: &albumID, Kind: medialibrary.KindAudio, Name: "Song",
		Album: "Album", AlbumArtist: "Artist", Artists: []string{"Artist"},
	}

	dto := server.itemDTOWithParentCache(context.Background(), item, false, "", viewer{}, false, parents)
	if dto.AlbumId != strconv.FormatInt(albumID, 10) || dto.AlbumPrimaryImageTag != "album-cover-tag" || dto.PrimaryImageItemId != dto.AlbumId || dto.PrimaryImageTag != "album-cover-tag" {
		t.Fatalf("album relationship = %#v", dto)
	}
	if len(dto.ArtistItems) != 1 || dto.ArtistItems[0].Id != strconv.FormatInt(artistID, 10) || len(dto.AlbumArtists) != 1 || dto.AlbumArtists[0].Name != "Artist" {
		t.Fatalf("artist relationships = %#v", dto)
	}
}

func TestMusicDTOKeepsTrackPrimaryImageWhenPresent(t *testing.T) {
	albumID := int64(32)
	server := &Server{serverID: "server"}
	parents := map[int64]medialibrary.Item{
		albumID: {ID: albumID, Kind: medialibrary.KindMusicAlbum, Name: "Album", ImageURL: "/album.jpg", ImageTag: "album-cover-tag"},
	}
	item := medialibrary.Item{
		ID: 33, ParentID: &albumID, Kind: medialibrary.KindAudio, Name: "Song",
		ImageURL: "/track.jpg", ImageTag: "track-cover-tag",
	}

	dto := server.itemDTOWithParentCache(context.Background(), item, false, "", viewer{}, false, parents)
	if dto.ImageTags["Primary"] != "track-cover-tag" || dto.AlbumPrimaryImageTag != "album-cover-tag" {
		t.Fatalf("image tags = %#v", dto)
	}
	if dto.PrimaryImageItemId != strconv.FormatInt(item.ID, 10) || dto.PrimaryImageTag != "track-cover-tag" {
		t.Fatalf("track primary image reference = %#v", dto)
	}
}

func TestEpisodeDTOUsesSeriesArtworkWhenEpisodeHasNoImage(t *testing.T) {
	seriesID := int64(21)
	seasonID := int64(22)
	server := &Server{serverID: "server"}
	parents := map[int64]medialibrary.Item{
		seriesID: {ID: seriesID, Kind: medialibrary.KindSeries, Name: "Series", ImageURL: "/series.jpg", ImageTag: "series-cover-tag"},
		seasonID: {ID: seasonID, ParentID: &seriesID, Kind: medialibrary.KindSeason, Name: "Season 1"},
	}
	episode := medialibrary.Item{ID: 23, ParentID: &seasonID, Kind: medialibrary.KindEpisode, Name: "Episode 1"}

	dto := server.itemDTOWithParentCache(context.Background(), episode, false, "", viewer{}, true, parents)
	if dto.ImageTags["Primary"] != "series-cover-tag" || dto.SeriesPrimaryImageTag != "series-cover-tag" || dto.PrimaryImageItemId != strconv.FormatInt(seriesID, 10) || dto.PrimaryImageTag != "series-cover-tag" || dto.ParentPrimaryImageItemId != strconv.FormatInt(seriesID, 10) || dto.ParentPrimaryImageTag != "series-cover-tag" {
		t.Fatalf("series artwork inheritance = %#v", dto)
	}
}

func TestEpisodeDTOKeepsEpisodePrimaryImageWhenPresent(t *testing.T) {
	seriesID := int64(21)
	seasonID := int64(22)
	server := &Server{serverID: "server"}
	parents := map[int64]medialibrary.Item{
		seriesID: {ID: seriesID, Kind: medialibrary.KindSeries, Name: "Series", ImageURL: "/series.jpg", ImageTag: "series-cover-tag"},
		seasonID: {ID: seasonID, ParentID: &seriesID, Kind: medialibrary.KindSeason, Name: "Season 1"},
	}
	episode := medialibrary.Item{
		ID: 23, ParentID: &seasonID, Kind: medialibrary.KindEpisode, Name: "Episode 1",
		ImageURL: "/episode.jpg", ImageTag: "episode-image-tag",
	}

	dto := server.itemDTOWithParentCache(context.Background(), episode, false, "", viewer{}, true, parents)
	if dto.ImageTags["Primary"] != "episode-image-tag" || dto.SeriesPrimaryImageTag != "series-cover-tag" {
		t.Fatalf("episode artwork = %#v", dto)
	}
	if dto.PrimaryImageItemId != strconv.FormatInt(episode.ID, 10) || dto.PrimaryImageTag != "episode-image-tag" {
		t.Fatalf("episode primary image reference = %#v", dto)
	}
}

func TestMovieDTOExposesPrimaryImageForResumeClients(t *testing.T) {
	server := &Server{serverID: "server"}
	movie := medialibrary.Item{
		ID: 41, Kind: medialibrary.KindMovie, Name: "Movie",
		ImageURL: "/movie.jpg", ImageTag: "movie-cover-tag",
	}

	dto := server.itemDTO(movie, false, "")
	if dto.ImageTags["Primary"] != "movie-cover-tag" || dto.PrimaryImageItemId != strconv.FormatInt(movie.ID, 10) || dto.PrimaryImageTag != "movie-cover-tag" {
		t.Fatalf("movie primary image reference = %#v", dto)
	}
}

func TestSharedPeopleSourceIDUsesHierarchyForBatchResponses(t *testing.T) {
	seasonID := int64(22)
	seriesID := int64(21)
	tests := []struct {
		name      string
		items     []medialibrary.Item
		requested bool
		wantID    int64
		want      bool
	}{
		{name: "not requested", items: []medialibrary.Item{{ID: 23}}, requested: false},
		{name: "single item", items: []medialibrary.Item{{ID: 23, Kind: medialibrary.KindEpisode, ParentID: &seasonID}}, requested: true, wantID: 23, want: true},
		{name: "episodes share season", items: []medialibrary.Item{{ID: 23, Kind: medialibrary.KindEpisode, ParentID: &seasonID}, {ID: 24, Kind: medialibrary.KindEpisode, ParentID: &seasonID}}, requested: true, wantID: seasonID, want: true},
		{name: "seasons share series", items: []medialibrary.Item{{ID: 22, Kind: medialibrary.KindSeason, ParentID: &seriesID}, {ID: 25, Kind: medialibrary.KindSeason, ParentID: &seriesID}}, requested: true, wantID: seriesID, want: true},
		{name: "mixed catalog omits people", items: []medialibrary.Item{{ID: 1, Kind: medialibrary.KindMovie}, {ID: 2, Kind: medialibrary.KindMovie}}, requested: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotID, got := sharedPeopleSourceID(test.items, test.requested)
			if got != test.want || gotID != test.wantID {
				t.Fatalf("sharedPeopleSourceID() = (%d, %v), want (%d, %v)", gotID, got, test.wantID, test.want)
			}
		})
	}
}
