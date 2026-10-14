package emby

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
)

const libraryIDBase int64 = 8_000_000_000_000_000_000

func libraryViewID(id int64) string {
	return strconv.FormatInt(libraryIDBase+id, 10)
}

func parseLibraryViewID(value string) (int64, bool) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= libraryIDBase {
		return 0, false
	}
	return id - libraryIDBase, true
}

func (s *Server) libraryDTO(ctx context.Context, library medialibrary.Library, viewer viewer) baseItemDTO {
	dto := baseItemDTO{
		Name: library.Name, ServerId: s.serverID, Id: libraryViewID(library.ID), ParentId: "0",
		Etag: timestampTag(library.UpdatedAt), DateCreated: embyTime(library.CreatedAt), SortName: library.Name,
		ProviderIds: map[string]string{}, IsFolder: true, Type: "CollectionFolder", LocationType: "FileSystem",
		CanDelete: false, CanDownload: false, SupportsSync: false, ImageTags: map[string]string{},
		BackdropImageTags: []string{}, UserData: userItemDataDTO{}, CollectionType: string(library.ContentType),
	}
	if cover, err := s.media.LibraryCoverForUser(ctx, viewer.mediaAccess(), library); err == nil {
		dto.PrimaryImageItemId = libraryViewID(library.ID)
		dto.PrimaryImageTag = cover.Tag
		dto.ImageTags["Primary"] = cover.Tag
		// Emby clients commonly use the backdrop collection for the horizontal
		// library thumb. The same generated collage is served for both image
		// routes by handlePublicImage.
		dto.BackdropImageTags = []string{cover.Tag}
	}
	return dto
}

func (s *Server) itemDTO(item medialibrary.Item, includeMediaSources bool, token string) baseItemDTO {
	parentID := libraryViewID(item.LibraryID)
	if item.ParentID != nil {
		parentID = strconv.FormatInt(*item.ParentID, 10)
	}
	providerIDs := map[string]string{}
	if item.TMDBID != nil {
		providerIDs["Tmdb"] = strconv.FormatInt(*item.TMDBID, 10)
	}
	if item.TVDBID != nil {
		providerIDs["Tvdb"] = strconv.FormatInt(*item.TVDBID, 10)
	}
	if item.IMDbID != "" {
		providerIDs["Imdb"] = item.IMDbID
	}
	imageTags := map[string]string{}
	if item.ImageURL != "" {
		imageTags["Primary"] = item.ImageTag
	}
	backdropTags := []string{}
	if item.BackdropURL != "" {
		backdropTags = append(backdropTags, item.BackdropTag)
	}
	mediaType := ""
	if item.Kind == medialibrary.KindMovie || item.Kind == medialibrary.KindEpisode {
		mediaType = "Video"
	} else if item.Kind == medialibrary.KindAudio {
		mediaType = "Audio"
	}
	mediaPath := ""
	mediaSourceCount := 0
	if !item.Kind.Folder() {
		mediaPath = item.MediaPath
		mediaSourceCount = 1
	}
	dto := baseItemDTO{
		Name: item.Name, ServerId: s.serverID, Id: strconv.FormatInt(item.ID, 10), ParentId: parentID,
		Etag: timestampTag(item.UpdatedAt), DateCreated: embyTime(item.CreatedAt), SortName: item.SortName,
		Overview: item.Overview, Genres: item.Genres, PremiereDate: embyDate(item.PremiereDate), CommunityRating: item.CommunityRating,
		RunTimeTicks: item.RuntimeTicks, ProductionYear: item.ProductionYear, ProviderIds: providerIDs,
		IsFolder: item.Kind.Folder(), Type: embyItemType(item.Kind), MediaType: mediaType,
		Container: item.Container, Path: mediaPath, MediaSourceCount: mediaSourceCount,
		LocationType: "FileSystem", CanDelete: false,
		CanDownload: !item.Kind.Folder(), SupportsSync: false, ImageTags: imageTags,
		BackdropImageTags: backdropTags, UserData: mapUserData(item), Album: item.Album,
		AlbumArtist: item.AlbumArtist, Artists: item.Artists,
		HasLyrics: item.Lyrics != "" || item.TranslatedLyrics != "",
	}
	if item.ImageURL != "" {
		// Some Emby clients read only the top-level primary image reference on
		// resume cards instead of ImageTags. Keep both representations aligned.
		dto.PrimaryImageItemId = dto.Id
		dto.PrimaryImageTag = item.ImageTag
	}
	if item.Kind == medialibrary.KindSeason {
		dto.IndexNumber = item.SeasonNumber
	}
	if item.Kind == medialibrary.KindEpisode {
		dto.IndexNumber = item.EpisodeNumber
		dto.ParentIndexNumber = item.SeasonNumber
	}
	if item.Kind == medialibrary.KindAudio {
		dto.IndexNumber = item.TrackNumber
		dto.ParentIndexNumber = item.DiscNumber
	}
	if includeMediaSources && !item.Kind.Folder() {
		dto.MediaSources = []mediaSourceInfo{s.mediaSource(item, token)}
	}
	return dto
}

func (s *Server) itemDTOWithParents(ctx context.Context, item medialibrary.Item, includeMediaSources bool, token string, viewer viewer) baseItemDTO {
	return s.itemDTOWithParentCache(ctx, item, includeMediaSources, token, viewer, true, make(map[int64]medialibrary.Item))
}

func (s *Server) itemDTOWithParentCache(ctx context.Context, item medialibrary.Item, includeMediaSources bool, token string, viewer viewer, includeEpisodeParents bool, parents map[int64]medialibrary.Item) baseItemDTO {
	dto := s.itemDTO(item, includeMediaSources, token)
	if item.Kind == medialibrary.KindAudio || item.Kind == medialibrary.KindMusicAlbum {
		s.attachMusicParents(ctx, &dto, item, viewer, parents)
	}
	if item.Kind == medialibrary.KindSeason {
		s.attachSeasonSeries(ctx, &dto, item, viewer, parents)
		return dto
	}
	if !includeEpisodeParents || item.Kind != medialibrary.KindEpisode || item.ParentID == nil {
		return dto
	}
	season, ok := s.parentItem(ctx, viewer, *item.ParentID, parents)
	if !ok || season.Kind != medialibrary.KindSeason {
		return dto
	}
	dto.SeasonId = strconv.FormatInt(season.ID, 10)
	dto.SeasonName = season.Name
	if season.ImageURL != "" {
		dto.ParentPrimaryImageItemId = dto.SeasonId
		dto.ParentPrimaryImageTag = season.ImageTag
	}
	if season.ParentID == nil {
		return dto
	}
	series, ok := s.parentItem(ctx, viewer, *season.ParentID, parents)
	if !ok || series.Kind != medialibrary.KindSeries {
		return dto
	}
	dto.SeriesId = strconv.FormatInt(series.ID, 10)
	dto.SeriesName = series.Name
	if series.ImageURL != "" {
		dto.SeriesPrimaryImageTag = series.ImageTag
		if dto.ParentPrimaryImageTag == "" {
			dto.ParentPrimaryImageItemId = dto.SeriesId
			dto.ParentPrimaryImageTag = series.ImageTag
		}
		if dto.ImageTags["Primary"] == "" {
			dto.ImageTags["Primary"] = series.ImageTag
			dto.PrimaryImageItemId = dto.SeriesId
			dto.PrimaryImageTag = series.ImageTag
		}
	}
	return dto
}

func (s *Server) attachSeasonSeries(ctx context.Context, dto *baseItemDTO, season medialibrary.Item, viewer viewer, parents map[int64]medialibrary.Item) {
	if season.ParentID == nil {
		return
	}
	series, ok := s.parentItem(ctx, viewer, *season.ParentID, parents)
	if !ok || series.Kind != medialibrary.KindSeries {
		return
	}
	dto.SeriesId = strconv.FormatInt(series.ID, 10)
	dto.SeriesName = series.Name
	if series.ImageURL != "" {
		dto.SeriesPrimaryImageTag = series.ImageTag
	}
}

func (s *Server) attachMusicParents(ctx context.Context, dto *baseItemDTO, item medialibrary.Item, viewer viewer, parents map[int64]medialibrary.Item) {
	if item.ParentID == nil {
		return
	}
	parent, ok := s.parentItem(ctx, viewer, *item.ParentID, parents)
	if !ok {
		return
	}
	if item.Kind == medialibrary.KindMusicAlbum {
		if parent.Kind == medialibrary.KindMusicArtist {
			attachMusicArtist(dto, parent)
		}
		return
	}
	if item.Kind == medialibrary.KindAudio {
		if parent.Kind != medialibrary.KindMusicAlbum {
			return
		}
		dto.AlbumId = strconv.FormatInt(parent.ID, 10)
		if dto.Album == "" {
			dto.Album = parent.Name
		}
		if parent.ImageTag != "" {
			dto.AlbumPrimaryImageTag = parent.ImageTag
			if dto.ImageTags["Primary"] == "" {
				dto.PrimaryImageItemId = dto.AlbumId
				dto.PrimaryImageTag = parent.ImageTag
			}
		}
	}
	if parent.ParentID == nil {
		return
	}
	artist, ok := s.parentItem(ctx, viewer, *parent.ParentID, parents)
	if !ok || artist.Kind != medialibrary.KindMusicArtist {
		return
	}
	attachMusicArtist(dto, artist)
}

func attachMusicArtist(dto *baseItemDTO, artist medialibrary.Item) {
	artistPair := nameIDPair{Name: artist.Name, Id: strconv.FormatInt(artist.ID, 10)}
	dto.ArtistItems = []nameIDPair{artistPair}
	dto.AlbumArtists = []nameIDPair{artistPair}
	if dto.AlbumArtist == "" {
		dto.AlbumArtist = artist.Name
	}
}

func (s *Server) parentItem(ctx context.Context, viewer viewer, id int64, cache map[int64]medialibrary.Item) (medialibrary.Item, bool) {
	if item, ok := cache[id]; ok {
		return item, true
	}
	item, err := s.media.GetItemForUser(ctx, viewer.mediaAccess(), id)
	if err != nil {
		return medialibrary.Item{}, false
	}
	cache[id] = item
	return item, true
}

func (s *Server) mediaSource(item medialibrary.Item, token string) mediaSourceInfo {
	mediaPrefix := "Videos"
	streamType := "Video"
	if item.Kind == medialibrary.KindAudio {
		mediaPrefix = "Audio"
		streamType = "Audio"
	}
	streamURL := fmt.Sprintf("/%s/%d/stream", mediaPrefix, item.ID)
	if item.Container != "" {
		streamURL += "." + url.PathEscape(item.Container)
	}
	values := url.Values{"Static": []string{"true"}}
	if token != "" {
		values.Set("api_key", token)
	}
	streamURL += "?" + values.Encode()
	return mediaSourceInfo{
		Protocol: "Http", Id: strconv.FormatInt(item.ID, 10), Path: streamURL, Type: "Default",
		Container: item.Container, Size: item.SizeBytes, Name: item.Name, IsRemote: true,
		RunTimeTicks: item.RuntimeTicks, SupportsDirectPlay: true, SupportsDirectStream: true,
		SupportsTranscoding: false, DirectStreamUrl: streamURL,
		MediaStreams:        []mediaStream{{Type: streamType, Index: 0, IsDefault: true, IsExternal: false}},
		RequiredHttpHeaders: map[string]string{}, SupportsProbing: false,
	}
}

func mapUserData(item medialibrary.Item) userItemDataDTO {
	return mapUserDataValue(item.ID, item.RuntimeTicks, item.UserData)
}

func mapUserDataValue(itemID, runtimeTicks int64, data medialibrary.UserData) userItemDataDTO {
	playedPercentage := float64(0)
	if runtimeTicks > 0 && data.PlaybackPositionTicks > 0 {
		playedPercentage = float64(data.PlaybackPositionTicks) / float64(runtimeTicks) * 100
		if playedPercentage > 100 {
			playedPercentage = 100
		}
	}
	lastPlayedDate := ""
	if data.LastPlayedAt != nil {
		lastPlayedDate = embyTime(*data.LastPlayedAt)
	}
	return userItemDataDTO{
		ItemId:                strconv.FormatInt(itemID, 10),
		PlaybackPositionTicks: data.PlaybackPositionTicks, PlayCount: data.PlayCount,
		IsFavorite: data.Favorite, Played: data.Played, PlayedPercentage: playedPercentage,
		LastPlayedDate:    lastPlayedDate,
		UnplayedItemCount: data.UnplayedItemCount,
	}
}

func embyItemType(kind medialibrary.ItemKind) string {
	switch kind {
	case medialibrary.KindMovie:
		return "Movie"
	case medialibrary.KindSeries:
		return "Series"
	case medialibrary.KindSeason:
		return "Season"
	case medialibrary.KindEpisode:
		return "Episode"
	case medialibrary.KindMusicArtist:
		return "MusicArtist"
	case medialibrary.KindMusicAlbum:
		return "MusicAlbum"
	case medialibrary.KindAudio:
		return "Audio"
	default:
		return "Folder"
	}
}

func internalKinds(value string) []medialibrary.ItemKind {
	result := []medialibrary.ItemKind{}
	for _, itemType := range strings.Split(value, ",") {
		switch strings.ToLower(strings.TrimSpace(itemType)) {
		case "movie":
			result = append(result, medialibrary.KindMovie)
		case "series":
			result = append(result, medialibrary.KindSeries)
		case "season":
			result = append(result, medialibrary.KindSeason)
		case "episode":
			result = append(result, medialibrary.KindEpisode)
		case "musicartist":
			result = append(result, medialibrary.KindMusicArtist)
		case "musicalbum":
			result = append(result, medialibrary.KindMusicAlbum)
		case "audio":
			result = append(result, medialibrary.KindAudio)
		}
	}
	return result
}

func timestampTag(value time.Time) string {
	return strconv.FormatInt(value.UTC().UnixNano(), 16)
}

func embyTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func embyDate(value string) string {
	if value == "" {
		return ""
	}
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		return parsed.UTC().Format(time.RFC3339)
	}
	return value
}
