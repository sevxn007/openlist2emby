package emby

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
)

func (s *Server) handleViews(w http.ResponseWriter, r *http.Request, path []string, viewer viewer) {
	if len(path) != 3 || path[1] != viewer.idString() {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "resource not found")
		return
	}
	s.writeLibraryViews(w, r, viewer)
}

func (s *Server) writeLibraryViews(w http.ResponseWriter, r *http.Request, viewer viewer) {
	libraries, err := s.media.ListLibrariesForUser(r.Context(), viewer.mediaAccess())
	if err != nil {
		writeEmbyError(w, http.StatusInternalServerError, "ServerError", "could not list media libraries")
		return
	}
	items := make([]baseItemDTO, 0, len(libraries))
	for _, library := range libraries {
		items = append(items, s.libraryDTO(r.Context(), library, viewer))
	}
	writeEmbyJSON(w, http.StatusOK, queryResult{Items: items, TotalRecordCount: len(items), StartIndex: 0})
}

func (s *Server) handleUserItems(w http.ResponseWriter, r *http.Request, path []string, token string, viewer viewer) {
	if len(path) < 3 || path[1] != viewer.idString() {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "resource not found")
		return
	}
	if r.Method != http.MethodGet && !(r.Method == http.MethodHead && len(path) >= 6 && path[4] == "images") && !(len(path) == 5 && path[4] == "playbackinfo" && r.Method == http.MethodPost) {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "resource not found")
		return
	}
	if len(path) == 3 {
		s.handleItemQuery(w, r, token, medialibrary.ItemQuery{}, viewer)
		return
	}
	if libraryID, ok := parseLibraryViewID(path[3]); ok {
		if len(path) == 4 && r.Method == http.MethodGet {
			library, err := s.media.GetLibraryForUser(r.Context(), viewer.mediaAccess(), libraryID)
			if err != nil {
				writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "media library not found")
				return
			}
			writeEmbyJSON(w, http.StatusOK, s.libraryDTO(r.Context(), library, viewer))
			return
		}
		if len(path) == 5 && path[4] == "images" && r.Method == http.MethodGet {
			s.handleLibraryImageInfo(w, r, libraryID, viewer)
			return
		}
		if len(path) >= 6 && path[4] == "images" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			s.handleLibraryImage(w, r, libraryID, path[5], viewer)
			return
		}
		if len(path) == 5 && path[4] == "children" && r.Method == http.MethodGet {
			query, parsed := s.itemQueryFromRequest(w, r)
			if !parsed {
				return
			}
			query.LibraryID = &libraryID
			if !s.constrainBrowseHierarchy(w, r, &query, viewer) {
				return
			}
			s.writeItemPage(w, r, query, token, viewer)
			return
		}
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "resource not found")
		return
	}
	switch path[3] {
	case "root":
		writeEmbyJSON(w, http.StatusOK, baseItemDTO{
			Name: productName, ServerId: s.serverID, Id: "0", Etag: s.serverID, SortName: productName,
			ProviderIds: map[string]string{}, IsFolder: true, Type: "Folder", LocationType: "FileSystem",
			ImageTags: map[string]string{}, BackdropImageTags: []string{}, UserData: userItemDataDTO{},
		})
	case "latest":
		query, ok := s.itemQueryFromRequest(w, r)
		if !ok {
			return
		}
		query.Latest = true
		query.SkipTotal = true
		if !s.constrainBrowseHierarchy(w, r, &query, viewer) {
			return
		}
		if query.Limit <= 0 {
			query.Limit = 20
		}
		page, err := s.media.ListItemsForUser(r.Context(), viewer.mediaAccess(), query)
		if err != nil {
			writeEmbyError(w, http.StatusInternalServerError, "ServerError", "could not list latest items")
			return
		}
		writeEmbyJSON(w, http.StatusOK, s.mapItems(r.Context(), page.Items, false, false, token, viewer, embyFieldRequested(r, "People"), embyLanguage(r)))
	case "resume":
		s.handleContinueWatching(w, r, token, viewer)
	default:
		if len(path) < 4 {
			writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "resource not found")
			return
		}
		id, err := parsePositiveID(path[3])
		if err != nil {
			writeEmbyError(w, http.StatusBadRequest, "InvalidRequest", "invalid item ID")
			return
		}
		if len(path) == 5 && path[4] == "children" && r.Method == http.MethodGet {
			query, ok := s.itemQueryFromRequest(w, r)
			if !ok {
				return
			}
			query.ParentID, query.ParentSet = &id, true
			if !s.constrainBrowseHierarchy(w, r, &query, viewer) {
				return
			}
			s.writeItemPage(w, r, query, token, viewer)
			return
		}
		if len(path) == 5 && path[4] == "images" && r.Method == http.MethodGet {
			s.handleImageInfo(w, r, id, viewer)
			return
		}
		if len(path) >= 6 && path[4] == "images" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			s.handleImage(w, r, id, path[5], viewer)
			return
		}
		if len(path) == 5 && path[4] == "playbackinfo" && (r.Method == http.MethodGet || r.Method == http.MethodPost) {
			s.handlePlaybackInfo(w, r, id, token, viewer)
			return
		}
		if len(path) == 4 && r.Method == http.MethodGet {
			s.writeItem(w, r, id, token, viewer)
			return
		}
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "resource not found")
	}
}

func (s *Server) handleItems(w http.ResponseWriter, r *http.Request, path []string, token string, viewer viewer) {
	if len(path) == 1 && r.Method == http.MethodGet {
		s.handleItemQuery(w, r, token, medialibrary.ItemQuery{}, viewer)
		return
	}
	if len(path) == 2 && path[1] == "counts" && r.Method == http.MethodGet {
		s.handleItemCounts(w, r, viewer)
		return
	}
	if len(path) < 2 {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "resource not found")
		return
	}
	if libraryID, ok := parseLibraryViewID(path[1]); ok {
		library, err := s.media.GetLibraryForUser(r.Context(), viewer.mediaAccess(), libraryID)
		if err != nil {
			writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "media library not found")
			return
		}
		if len(path) == 2 && r.Method == http.MethodGet {
			writeEmbyJSON(w, http.StatusOK, s.libraryDTO(r.Context(), library, viewer))
			return
		}
		if len(path) == 3 && path[2] == "images" && r.Method == http.MethodGet {
			s.handleLibraryImageInfo(w, r, libraryID, viewer)
			return
		}
		if len(path) >= 4 && path[2] == "images" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			s.handleLibraryImage(w, r, libraryID, path[3], viewer)
			return
		}
		if len(path) == 3 && path[2] == "children" && r.Method == http.MethodGet {
			query, parsed := s.itemQueryFromRequest(w, r)
			if !parsed {
				return
			}
			query.LibraryID = &libraryID
			if !s.constrainBrowseHierarchy(w, r, &query, viewer) {
				return
			}
			s.writeItemPage(w, r, query, token, viewer)
			return
		}
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "resource not found")
		return
	}
	id, err := parsePositiveID(path[1])
	if err != nil {
		writeEmbyError(w, http.StatusBadRequest, "InvalidRequest", "invalid item ID")
		return
	}
	if len(path) == 3 && path[2] == "children" && r.Method == http.MethodGet {
		query, parsed := s.itemQueryFromRequest(w, r)
		if !parsed {
			return
		}
		query.ParentID, query.ParentSet = &id, true
		if !s.constrainBrowseHierarchy(w, r, &query, viewer) {
			return
		}
		s.writeItemPage(w, r, query, token, viewer)
		return
	}
	if len(path) == 2 && r.Method == http.MethodGet {
		s.writeItem(w, r, id, token, viewer)
		return
	}
	if len(path) == 3 && path[2] == "playbackinfo" && (r.Method == http.MethodGet || r.Method == http.MethodPost) {
		s.handlePlaybackInfo(w, r, id, token, viewer)
		return
	}
	if len(path) == 3 && path[2] == "people" && r.Method == http.MethodGet {
		s.handleItemPeople(w, r, id, viewer)
		return
	}
	if len(path) == 3 && path[2] == "images" && r.Method == http.MethodGet {
		s.handleImageInfo(w, r, id, viewer)
		return
	}
	if len(path) >= 4 && path[2] == "images" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		s.handleImage(w, r, id, path[3], viewer)
		return
	}
	if len(path) == 3 && path[2] == "download" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		s.redirectItem(w, r, id, viewer)
		return
	}
	writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "resource not found")
}

func (s *Server) handleArtists(w http.ResponseWriter, r *http.Request, token string, viewer viewer) {
	query, ok := s.itemQueryFromRequest(w, r)
	if !ok {
		return
	}
	query.Kinds = []medialibrary.ItemKind{medialibrary.KindMusicArtist}
	page, err := s.media.ListItemsForUser(r.Context(), viewer.mediaAccess(), query)
	if err != nil {
		writeEmbyError(w, http.StatusInternalServerError, "ServerError", "could not list music artists")
		return
	}
	items := s.mapItems(r.Context(), page.Items, embyFieldRequested(r, "MediaSources"), false, token, viewer, false, embyLanguage(r))
	// Artist counts require two aggregate lookups per artist. Run them in a
	// bounded batch so Emby clients do not wait for a long serial N+1 chain.
	var wait sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex
	semaphore := make(chan struct{}, 8)
	for index, artist := range page.Items {
		index, artist := index, artist
		wait.Add(1)
		go func() {
			defer wait.Done()
			select {
			case semaphore <- struct{}{}:
			case <-r.Context().Done():
				return
			}
			defer func() { <-semaphore }()
			if err := s.attachMusicArtistCounts(r.Context(), &items[index], artist.ID, viewer); err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
			}
		}()
	}
	wait.Wait()
	if firstErr != nil {
		writeEmbyError(w, http.StatusInternalServerError, "ServerError", "could not count music artist items")
		return
	}
	writeEmbyJSON(w, http.StatusOK, queryResult{Items: items, TotalRecordCount: page.Total, StartIndex: query.Start})
}

func (s *Server) attachMusicArtistCounts(ctx context.Context, dto *baseItemDTO, artistID int64, viewer viewer) error {
	albums, err := s.media.ListItemsForUser(ctx, viewer.mediaAccess(), medialibrary.ItemQuery{MusicArtistIDs: []int64{artistID}, Kinds: []medialibrary.ItemKind{medialibrary.KindMusicAlbum}, Limit: 1})
	if err != nil {
		return err
	}
	songs, err := s.media.ListItemsForUser(ctx, viewer.mediaAccess(), medialibrary.ItemQuery{MusicArtistIDs: []int64{artistID}, Kinds: []medialibrary.ItemKind{medialibrary.KindAudio}, Limit: 1})
	if err != nil {
		return err
	}
	dto.AlbumCount = albums.Total
	dto.ChildCount = albums.Total
	dto.SongCount = songs.Total
	return nil
}

func (s *Server) handleImageInfo(w http.ResponseWriter, r *http.Request, id int64, viewer viewer) {
	images, err := s.media.ListItemImagesForUser(r.Context(), viewer.mediaAccess(), id)
	if err != nil {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "media item not found")
		return
	}
	result := make([]imageInfoDTO, 0, len(images))
	for _, image := range images {
		imageType := "Primary"
		if image.ImageType == medialibrary.ImageBackdrop {
			imageType = "Backdrop"
		}
		result = append(result, imageInfoDTO{
			ImageType: imageType, ImageIndex: 0, Path: "", Filename: "",
			Height: image.Height, Width: image.Width, Size: image.SizeBytes,
		})
	}
	writeEmbyJSON(w, http.StatusOK, result)
}

func (s *Server) handleLibraryImageInfo(w http.ResponseWriter, r *http.Request, libraryID int64, viewer viewer) {
	if _, err := s.media.GetLibraryForUser(r.Context(), viewer.mediaAccess(), libraryID); err != nil {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "media library not found")
		return
	}
	writeEmbyJSON(w, http.StatusOK, []imageInfoDTO{
		{ImageType: "Primary", ImageIndex: 0, Width: 960, Height: 540},
		{ImageType: "Backdrop", ImageIndex: 0, Width: 960, Height: 540},
	})
}

func (s *Server) handleLibraryImage(w http.ResponseWriter, r *http.Request, libraryID int64, imageType string, viewer viewer) {
	if !strings.EqualFold(imageType, "Primary") && !strings.EqualFold(imageType, "Backdrop") {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "media image not found")
		return
	}
	library, err := s.media.GetLibraryForUser(r.Context(), viewer.mediaAccess(), libraryID)
	if err != nil {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "media library not found")
		return
	}
	cover, err := s.media.LibraryCoverForUser(r.Context(), viewer.mediaAccess(), library)
	if err != nil {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "media image not found")
		return
	}
	tag := queryValueFold(r, "tag")
	if tag == "" {
		tag = cover.Tag
	}
	content, err := s.media.GetPublicLibraryCover(r.Context(), libraryID, tag, embyImageVariant(r))
	if err != nil {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "media image not found")
		return
	}
	serveEmbyAsset(w, r, content)
}

func (s *Server) handleShows(w http.ResponseWriter, r *http.Request, path []string, token string, viewer viewer) {
	if len(path) == 2 && path[1] == "nextup" && r.Method == http.MethodGet {
		s.handleNextUp(w, r, token, viewer)
		return
	}
	if len(path) != 3 || r.Method != http.MethodGet {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "resource not found")
		return
	}
	seriesID, err := parsePositiveID(path[1])
	if err != nil {
		writeEmbyError(w, http.StatusBadRequest, "InvalidRequest", "invalid series ID")
		return
	}
	query := medialibrary.ItemQuery{Limit: queryInt(r, 0, "Limit"), Start: queryInt(r, 0, "StartIndex")}
	switch path[2] {
	case "seasons":
		query.ParentID, query.ParentSet = &seriesID, true
		query.Kinds = []medialibrary.ItemKind{medialibrary.KindSeason}
	case "episodes":
		query.Kinds = []medialibrary.ItemKind{medialibrary.KindEpisode}
		if seasonID := queryValueFold(r, "SeasonId"); seasonID != "" {
			value, err := parsePositiveID(seasonID)
			if err != nil {
				writeEmbyError(w, http.StatusBadRequest, "InvalidRequest", "invalid season ID")
				return
			}
			query.ParentID, query.ParentSet = &value, true
			s.rememberVidHubSeason(r, viewer, token, seriesID, value)
		} else {
			query.Recursive, query.AncestorID = true, &seriesID
		}
	default:
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "resource not found")
		return
	}
	s.writeItemPage(w, r, query, token, viewer)
}

func (s *Server) handleItemQuery(w http.ResponseWriter, r *http.Request, token string, base medialibrary.ItemQuery, viewer viewer) {
	query, ok := s.itemQueryFromRequest(w, r)
	if !ok {
		return
	}
	// Unsupported types (such as Playlist) must not become an unfiltered browse.
	if strings.TrimSpace(queryValueFold(r, "IncludeItemTypes")) != "" && len(query.Kinds) == 0 {
		writeEmbyJSON(w, http.StatusOK, queryResult{Items: []baseItemDTO{}, StartIndex: max(0, query.Start)})
		return
	}
	if base.Resume {
		query.Resume = true
	}
	if isRootLibraryBrowse(r, query) {
		s.writeLibraryViews(w, r, viewer)
		return
	}
	if !s.constrainBrowseHierarchy(w, r, &query, viewer) {
		return
	}
	s.writeItemPage(w, r, query, token, viewer)
}

// Emby clients that do not use /Users/{id}/Views commonly enumerate the
// server root through /Items. Return real library views there so top-level
// series and artists are not mistaken for separate media libraries.
func isRootLibraryBrowse(r *http.Request, query medialibrary.ItemQuery) bool {
	parentID := queryValueFold(r, "ParentId")
	return (parentID == "" || parentID == "0") && query.Search == "" && !query.Recursive && !query.Resume &&
		!query.FavoriteOnly && len(query.Kinds) == 0 && len(query.PersonIDs) == 0 && len(query.MusicArtistIDs) == 0
}

func (s *Server) constrainBrowseHierarchy(w http.ResponseWriter, r *http.Request, query *medialibrary.ItemQuery, viewer viewer) bool {
	if query.Search != "" {
		return true
	}
	if query.LibraryID != nil {
		library, err := s.media.GetLibraryForUser(r.Context(), viewer.mediaAccess(), *query.LibraryID)
		if err != nil {
			writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "media library not found")
			return false
		}
		if library.ContentType == medialibrary.ContentMusic {
			if len(query.Kinds) == 0 {
				query.Kinds = []medialibrary.ItemKind{medialibrary.KindAudio}
			}
			query.ParentID, query.ParentSet = nil, false
			query.Recursive, query.AncestorID = false, nil
			return true
		}
		kind, ok := libraryRootKind(library.ContentType)
		if !ok {
			return true
		}
		query.Kinds = []medialibrary.ItemKind{kind}
		query.ParentID, query.ParentSet = nil, true
		query.Recursive, query.AncestorID = false, nil
		return true
	}
	if query.ParentID == nil {
		return true
	}
	parent, err := s.media.GetItemForUser(r.Context(), viewer.mediaAccess(), *query.ParentID)
	if err != nil {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "parent item not found")
		return false
	}
	kind, ok := directChildKind(parent.Kind)
	if !ok {
		return true
	}
	query.Kinds = []medialibrary.ItemKind{kind}
	query.ParentSet = true
	query.Recursive, query.AncestorID = false, nil
	return true
}

func libraryRootKind(contentType medialibrary.ContentType) (medialibrary.ItemKind, bool) {
	switch contentType {
	case medialibrary.ContentMovies:
		return medialibrary.KindMovie, true
	case medialibrary.ContentTVShows:
		return medialibrary.KindSeries, true
	case medialibrary.ContentMusic:
		return medialibrary.KindAudio, true
	default:
		return "", false
	}
}

func directChildKind(parent medialibrary.ItemKind) (medialibrary.ItemKind, bool) {
	switch parent {
	case medialibrary.KindSeries:
		return medialibrary.KindSeason, true
	case medialibrary.KindSeason:
		return medialibrary.KindEpisode, true
	case medialibrary.KindMusicArtist:
		return medialibrary.KindMusicAlbum, true
	case medialibrary.KindMusicAlbum:
		return medialibrary.KindAudio, true
	default:
		return "", false
	}
}

func (s *Server) itemQueryFromRequest(w http.ResponseWriter, r *http.Request) (medialibrary.ItemQuery, bool) {
	query := medialibrary.ItemQuery{
		Kinds: internalKinds(queryValueFold(r, "IncludeItemTypes")), Search: queryValueFold(r, "SearchTerm"),
		Recursive: queryBool(r, "Recursive"), Start: queryInt(r, 0, "StartIndex"), Limit: queryInt(r, 50, "Limit"),
		SortBy: queryValueFold(r, "SortBy"), SortDescending: strings.EqualFold(queryValueFold(r, "SortOrder"), "Descending"),
	}
	for _, filter := range strings.Split(queryValueFold(r, "Filters"), ",") {
		if strings.EqualFold(strings.TrimSpace(filter), "IsFavorite") {
			query.FavoriteOnly = true
			break
		}
	}
	if rawPersonIDs := queryValueFold(r, "PersonIds"); rawPersonIDs != "" {
		personIDs, err := parsePersonIDs(rawPersonIDs)
		if err != nil {
			writeEmbyError(w, http.StatusBadRequest, "InvalidRequest", "invalid person ID")
			return medialibrary.ItemQuery{}, false
		}
		query.PersonIDs = personIDs
	}
	if rawArtistIDs := firstNonEmpty(queryValueFold(r, "ArtistIds"), queryValueFold(r, "AlbumArtistIds")); rawArtistIDs != "" {
		artistIDs, err := parsePositiveIDs(rawArtistIDs)
		if err != nil {
			writeEmbyError(w, http.StatusBadRequest, "InvalidRequest", "invalid artist ID")
			return medialibrary.ItemQuery{}, false
		}
		query.MusicArtistIDs = artistIDs
	}
	parent := queryValueFold(r, "ParentId")
	if parent == "" || parent == "0" {
		if !query.Recursive {
			query.ParentSet = true
		}
		return query, true
	}
	if libraryID, ok := parseLibraryViewID(parent); ok {
		query.LibraryID = &libraryID
		if !query.Recursive {
			query.ParentSet = true
		}
		return query, true
	}
	parentID, err := parsePositiveID(parent)
	if err != nil {
		writeEmbyError(w, http.StatusBadRequest, "InvalidRequest", "invalid parent ID")
		return medialibrary.ItemQuery{}, false
	}
	query.ParentID, query.ParentSet = &parentID, true
	if query.Recursive {
		query.AncestorID = &parentID
	}
	return query, true
}

func (s *Server) handleItemPeople(w http.ResponseWriter, r *http.Request, id int64, viewer viewer) {
	people, err := s.media.ListItemPeopleForUser(r.Context(), viewer.mediaAccess(), id, embyLanguage(r))
	if errors.Is(err, domain.ErrNotFound) {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "media item not found")
		return
	}
	if err != nil {
		writeEmbyError(w, http.StatusInternalServerError, "ServerError", "could not load media people")
		return
	}
	result := make([]baseItemPerson, 0, len(people))
	for _, person := range people {
		result = append(result, mapPerson(person))
	}
	writeEmbyJSON(w, http.StatusOK, result)
}

func parsePersonIDs(value string) ([]int64, error) {
	return parsePositiveIDs(value)
}

func parsePositiveIDs(value string) ([]int64, error) {
	values := strings.Split(value, ",")
	result := make([]int64, 0, len(values))
	seen := make(map[int64]struct{}, len(values))
	for _, value := range values {
		id, err := parsePositiveID(value)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result, nil
}

func (s *Server) writeItemPage(w http.ResponseWriter, r *http.Request, query medialibrary.ItemQuery, token string, viewer viewer) {
	page, err := s.media.ListItemsForUser(r.Context(), viewer.mediaAccess(), query)
	if err != nil {
		writeEmbyError(w, http.StatusInternalServerError, "ServerError", "could not list media items")
		return
	}
	writeEmbyJSON(w, http.StatusOK, queryResult{Items: s.mapItems(r.Context(), page.Items, embyFieldRequested(r, "MediaSources"), query.Resume, token, viewer, embyFieldRequested(r, "People"), embyLanguage(r)), TotalRecordCount: page.Total, StartIndex: query.Start})
}

func (s *Server) writeItem(w http.ResponseWriter, r *http.Request, id int64, token string, viewer viewer) {
	item, err := s.media.GetItemForUser(r.Context(), viewer.mediaAccess(), id)
	if err != nil {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "media item not found")
		return
	}
	dto := s.itemDTOWithParents(r.Context(), item, true, token, viewer)
	if item.Kind == medialibrary.KindMusicArtist {
		if err := s.attachMusicArtistCounts(r.Context(), &dto, item.ID, viewer); err != nil {
			writeEmbyError(w, http.StatusInternalServerError, "ServerError", "could not count music artist items")
			return
		}
	}
	dto.People = s.itemPeopleDTO(r.Context(), item.ID, viewer, embyLanguage(r))
	writeEmbyJSON(w, http.StatusOK, dto)
}

func (s *Server) mapItems(ctx context.Context, items []medialibrary.Item, includeMediaSources, includeEpisodeParents bool, token string, viewer viewer, includePeople bool, language string) []baseItemDTO {
	result := make([]baseItemDTO, 0, len(items))
	parents := make(map[int64]medialibrary.Item)
	sharedItems := items
	peopleSourceID, attachPeople := sharedPeopleSourceID(sharedItems, includePeople)
	var sharedPeople []domain.MediaPerson
	if attachPeople {
		sharedPeople = s.mediaPeople(ctx, peopleSourceID, viewer, language)
	}
	appendEpisodePeople := episodePeopleBatch(sharedItems, includePeople)
	episodePeopleByID := map[int64][]domain.MediaPerson{}
	if appendEpisodePeople {
		itemIDs := make([]int64, 0, len(sharedItems))
		for _, item := range sharedItems {
			itemIDs = append(itemIDs, item.ID)
		}
		if people, err := s.media.ListEpisodePeopleBatchForUser(ctx, viewer.mediaAccess(), itemIDs, language); err == nil {
			episodePeopleByID = people
		}
	}
	for _, item := range items {
		var dto baseItemDTO
		if item.Kind == medialibrary.KindSeason || item.Kind == medialibrary.KindEpisode || item.Kind == medialibrary.KindAudio || item.Kind == medialibrary.KindMusicAlbum {
			dto = s.itemDTOWithParentCache(ctx, item, includeMediaSources, token, viewer, includeEpisodeParents || item.Kind == medialibrary.KindEpisode, parents)
		} else {
			dto = s.itemDTO(item, includeMediaSources, token)
		}
		if attachPeople {
			people := sharedPeople
			if appendEpisodePeople {
				people = medialibrary.MergeMediaPeople(sharedPeople, episodePeopleByID[item.ID])
			}
			dto.People = mapPeople(people)
		}
		result = append(result, dto)
	}
	return result
}

// sharedPeopleSourceID prevents Fields=People list requests from multiplying
// TMDB calls. A season list shares series credits, and an episode list shares
// season credits; a single-item response keeps the exact item as its source.
func sharedPeopleSourceID(items []medialibrary.Item, requested bool) (int64, bool) {
	if !requested || len(items) == 0 {
		return 0, false
	}
	if len(items) == 1 {
		return items[0].ID, true
	}
	first := items[0]
	if first.ParentID == nil || (first.Kind != medialibrary.KindSeason && first.Kind != medialibrary.KindEpisode) {
		return 0, false
	}
	for _, item := range items[1:] {
		if item.Kind != first.Kind || item.ParentID == nil || *item.ParentID != *first.ParentID {
			return 0, false
		}
	}
	return *first.ParentID, true
}

func episodePeopleBatch(items []medialibrary.Item, requested bool) bool {
	if !requested || len(items) < 2 || items[0].Kind != medialibrary.KindEpisode || items[0].ParentID == nil {
		return false
	}
	parentID := *items[0].ParentID
	for _, item := range items[1:] {
		if item.Kind != medialibrary.KindEpisode || item.ParentID == nil || *item.ParentID != parentID {
			return false
		}
	}
	return true
}

func (s *Server) mediaPeople(ctx context.Context, itemID int64, viewer viewer, language string) []domain.MediaPerson {
	people, err := s.media.ListItemPeopleForUser(ctx, viewer.mediaAccess(), itemID, language)
	if err != nil {
		return []domain.MediaPerson{}
	}
	return people
}

func (s *Server) itemPeopleDTO(ctx context.Context, itemID int64, viewer viewer, language string) []baseItemPerson {
	return mapPeople(s.mediaPeople(ctx, itemID, viewer, language))
}

func mapPeople(people []domain.MediaPerson) []baseItemPerson {
	result := make([]baseItemPerson, 0, len(people))
	for _, person := range people {
		result = append(result, mapPerson(person))
	}
	return result
}

func mapPerson(person domain.MediaPerson) baseItemPerson {
	role := person.Role
	if person.DisplayRole != "" {
		role = person.DisplayRole
	}
	result := baseItemPerson{
		Name: person.Name, Id: strconv.FormatInt(person.ID, 10), Role: role, Type: person.Type,
	}
	if person.ProfileTag != "" {
		result.PrimaryImageTag = person.ProfileTag
		result.PrimaryImageAspectRatio = 0.6667
	} else if person.ProfileURL != "" {
		result.PrimaryImageTag = "person:" + strconv.FormatInt(person.ID, 10)
		result.PrimaryImageAspectRatio = 0.6667
	}
	return result
}

func (s *Server) handlePersonImages(w http.ResponseWriter, r *http.Request, path []string, viewer viewer) {
	if len(path) != 4 || path[2] != "images" || !strings.EqualFold(path[3], "primary") || r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "person image not found")
		return
	}
	personID, err := parsePositiveID(path[1])
	if err != nil {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "person image not found")
		return
	}
	if s.media.AssetFiles() == nil {
		target, profileErr := s.media.GetPersonProfileForUser(r.Context(), viewer.mediaAccess(), personID, embyLanguage(r))
		if profileErr != nil || target == "" {
			writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "person image not found")
			return
		}
		http.Redirect(w, r, target, http.StatusFound)
		return
	}
	content, err := s.media.GetPersonImageForUser(r.Context(), viewer.mediaAccess(), personID, embyImageVariant(r))
	if err != nil {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "person image not found")
		return
	}
	serveEmbyAsset(w, r, content)
}

func embyFieldRequested(r *http.Request, field string) bool {
	for _, value := range strings.Split(queryValueFold(r, "Fields"), ",") {
		if strings.EqualFold(strings.TrimSpace(value), field) {
			return true
		}
	}
	return false
}

func (s *Server) handleItemCounts(w http.ResponseWriter, r *http.Request, viewer viewer) {
	counts := map[string]int{"MovieCount": 0, "SeriesCount": 0, "EpisodeCount": 0, "SongCount": 0, "AlbumCount": 0, "ArtistCount": 0}
	for key, kind := range map[string]medialibrary.ItemKind{
		"MovieCount": medialibrary.KindMovie, "SeriesCount": medialibrary.KindSeries,
		"EpisodeCount": medialibrary.KindEpisode, "SongCount": medialibrary.KindAudio,
		"AlbumCount": medialibrary.KindMusicAlbum, "ArtistCount": medialibrary.KindMusicArtist,
	} {
		page, err := s.media.ListItemsForUser(r.Context(), viewer.mediaAccess(), medialibrary.ItemQuery{Kinds: []medialibrary.ItemKind{kind}, Limit: 1})
		if err != nil {
			writeEmbyError(w, http.StatusInternalServerError, "ServerError", "could not count media items")
			return
		}
		counts[key] = page.Total
	}
	writeEmbyJSON(w, http.StatusOK, counts)
}

func (s *Server) handleImage(w http.ResponseWriter, r *http.Request, id int64, imageType string, viewer viewer) {
	if tag := queryValueFold(r, "tag"); strings.HasPrefix(tag, "person:") {
		personID, parseErr := parsePositiveID(strings.TrimPrefix(tag, "person:"))
		if parseErr == nil {
			if s.media.AssetFiles() == nil {
				target, profileErr := s.media.GetPersonProfileForUser(r.Context(), viewer.mediaAccess(), personID, embyLanguage(r))
				if profileErr == nil && target != "" {
					http.Redirect(w, r, target, http.StatusFound)
					return
				}
			}
			content, profileErr := s.media.GetPersonImageForUser(r.Context(), viewer.mediaAccess(), personID, embyImageVariant(r))
			if profileErr == nil {
				serveEmbyAsset(w, r, content)
				return
			}
		}
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "person image not found")
		return
	}
	typeValue := medialibrary.ImagePrimary
	if strings.EqualFold(imageType, "Backdrop") {
		typeValue = medialibrary.ImageBackdrop
	} else if !strings.EqualFold(imageType, "Primary") {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "media image not found")
		return
	}
	content, err := s.media.GetItemImageForUser(r.Context(), viewer.mediaAccess(), id, typeValue, embyImageVariant(r))
	if err != nil {
		if person, profileErr := s.media.GetPersonImageForUser(r.Context(), viewer.mediaAccess(), id, embyImageVariant(r)); profileErr == nil {
			serveEmbyAsset(w, r, person)
			return
		}
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "media image not found")
		return
	}
	serveEmbyAsset(w, r, content)
}

func embyImageVariant(r *http.Request) medialibrary.ImageVariant {
	return medialibrary.ImageVariant{
		MaxWidth: queryInt(r, 0, "MaxWidth"), MaxHeight: queryInt(r, 0, "MaxHeight"),
		Width: queryInt(r, 0, "Width"), Height: queryInt(r, 0, "Height"), Quality: queryInt(r, 0, "Quality"),
		Format: queryValueFold(r, "Format"),
	}
}

func serveEmbyAsset(w http.ResponseWriter, r *http.Request, content medialibrary.AssetContent) {
	etag := `"` + content.ETag + `"`
	w.Header().Set("Content-Type", content.MIMEType)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	if strings.TrimSpace(r.Header.Get("If-None-Match")) == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	file, err := os.Open(content.Path)
	if err != nil {
		writeEmbyError(w, http.StatusNotFound, "ResourceNotFound", "media image not found")
		return
	}
	defer file.Close()
	http.ServeContent(w, r, "", content.LastModified, file)
}

func embyLanguage(r *http.Request) string {
	if language := queryValueFold(r, "Language"); language != "" {
		return language
	}
	language, _, _ := strings.Cut(r.Header.Get("Accept-Language"), ",")
	return strings.TrimSpace(language)
}

func (s *Server) handleSearchHints(w http.ResponseWriter, r *http.Request, viewer viewer) {
	query := medialibrary.ItemQuery{Search: queryValueFold(r, "SearchTerm"), Recursive: true, Limit: queryInt(r, 20, "Limit"), Start: queryInt(r, 0, "StartIndex")}
	page, err := s.media.ListItemsForUser(r.Context(), viewer.mediaAccess(), query)
	if err != nil {
		writeEmbyError(w, http.StatusInternalServerError, "ServerError", "could not search media items")
		return
	}
	hints := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		hints = append(hints, map[string]any{
			"ItemId": strconv.FormatInt(item.ID, 10), "Id": strconv.FormatInt(item.ID, 10), "Name": item.Name,
			"MatchedTerm": item.Name, "IndexableText": item.Name, "ProductionYear": item.ProductionYear,
			"PrimaryImageTag": item.ImageTag, "Type": embyItemType(item.Kind), "MediaType": s.itemDTO(item, false, "").MediaType,
		})
	}
	writeEmbyJSON(w, http.StatusOK, map[string]any{"SearchHints": hints, "TotalRecordCount": page.Total})
}

func queryBool(r *http.Request, name string) bool {
	value := queryValueFold(r, name)
	parsed, _ := strconv.ParseBool(value)
	return parsed
}

func queryInt(r *http.Request, fallback int, name string) int {
	value := strings.TrimSpace(queryValueFold(r, name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}
