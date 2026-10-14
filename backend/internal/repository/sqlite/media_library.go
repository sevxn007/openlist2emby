package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Store) ListLibraries(ctx context.Context) ([]medialibrary.Library, error) {
	var records []mediaLibraryModel
	if err := withMediaLibraryRoots(s.db.WithContext(ctx)).
		Order("sort_order").
		Order("name COLLATE NOCASE").
		Order("id").
		Find(&records).Error; err != nil {
		return nil, err
	}
	libraries := make([]medialibrary.Library, 0, len(records))
	for _, record := range records {
		libraries = append(libraries, mediaLibraryFromModel(record))
	}
	return libraries, nil
}

func (s *Store) GetLibrary(ctx context.Context, id int64) (medialibrary.Library, error) {
	var record mediaLibraryModel
	err := withMediaLibraryRoots(s.db.WithContext(ctx)).Where("id = ?", id).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return medialibrary.Library{}, domain.ErrNotFound
	}
	if err != nil {
		return medialibrary.Library{}, err
	}
	return mediaLibraryFromModel(record), nil
}

func (s *Store) CreateLibrary(ctx context.Context, input medialibrary.LibraryInput) (medialibrary.Library, error) {
	rootPaths, err := libraryRootPaths(input)
	if err != nil {
		return medialibrary.Library{}, err
	}
	now := newSQLiteTimestamp(s.now())
	record := mediaLibraryModel{
		Name: input.Name, ContentType: string(input.ContentType), RootPath: rootPaths[0],
		VideoScreenshotEnabled: libraryScreenshotEnabled(input), VideoScreenshotPercent: libraryScreenshotPercent(input),
		PlaybackCompletionPercent: libraryPlaybackCompletionPercent(input),
		HidePeopleWithoutImages:   hidePeopleWithoutImages(input),
		CreatedAt:                 now, UpdatedAt: now,
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var maxSortOrder int
		if err := tx.Model(&mediaLibraryModel{}).Select("COALESCE(MAX(sort_order), -1)").Scan(&maxSortOrder).Error; err != nil {
			return err
		}
		record.SortOrder = maxSortOrder + 1
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		if err := tx.Create(mediaLibraryRootRecords(record.ID, rootPaths, now)).Error; err != nil {
			return err
		}
		return addLibraryToConfiguredAdministrators(tx, record.ID, now)
	})
	if err != nil {
		if isUniqueConstraint(err) {
			return medialibrary.Library{}, fmt.Errorf("%w: root path already belongs to a media library", medialibrary.ErrInvalidLibrary)
		}
		return medialibrary.Library{}, err
	}
	return s.GetLibrary(ctx, record.ID)
}

func (s *Store) UpdateLibrary(ctx context.Context, id int64, input medialibrary.LibraryInput) (medialibrary.Library, error) {
	rootPaths, err := libraryRootPaths(input)
	if err != nil {
		return medialibrary.Library{}, err
	}
	now := newSQLiteTimestamp(s.now())
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing mediaLibraryModel
		if err := tx.Select("id", "hide_people_without_images").Where("id = ?", id).Take(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrNotFound
			}
			return err
		}
		updates := map[string]any{
			"name": input.Name, "content_type": string(input.ContentType), "root_path": rootPaths[0],
			"video_screenshot_enabled": libraryScreenshotEnabled(input), "video_screenshot_percent": libraryScreenshotPercent(input),
			"playback_completion_percent": libraryPlaybackCompletionPercent(input),
			"updated_at":                  now,
		}
		if input.HidePeopleWithoutImages != nil {
			updates["hide_people_without_images"] = *input.HidePeopleWithoutImages
		}
		result := tx.
			Model(&mediaLibraryModel{}).
			Where("id = ?", id).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return domain.ErrNotFound
		}
		if err := tx.Where("library_id = ?", id).Delete(&mediaLibraryRootModel{}).Error; err != nil {
			return err
		}
		return tx.Create(mediaLibraryRootRecords(id, rootPaths, now)).Error
	})
	if isUniqueConstraint(err) {
		return medialibrary.Library{}, fmt.Errorf("%w: root path already belongs to a media library", medialibrary.ErrInvalidLibrary)
	}
	if err != nil {
		return medialibrary.Library{}, err
	}
	return s.GetLibrary(ctx, id)
}

func (s *Store) ReorderLibraries(ctx context.Context, libraryIDs []int64) error {
	seen := make(map[int64]struct{}, len(libraryIDs))
	for _, id := range libraryIDs {
		if id <= 0 {
			return fmt.Errorf("%w: library IDs must be positive", medialibrary.ErrInvalidLibrary)
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("%w: library IDs must not repeat", medialibrary.ErrInvalidLibrary)
		}
		seen[id] = struct{}{}
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existingIDs []int64
		if err := tx.Model(&mediaLibraryModel{}).Order("id").Pluck("id", &existingIDs).Error; err != nil {
			return err
		}
		if len(existingIDs) != len(libraryIDs) {
			return fmt.Errorf("%w: order must include every media library", medialibrary.ErrInvalidLibrary)
		}
		for _, id := range existingIDs {
			if _, exists := seen[id]; !exists {
				return fmt.Errorf("%w: order must include every media library", medialibrary.ErrInvalidLibrary)
			}
		}
		now := newSQLiteTimestamp(s.now())
		for position, id := range libraryIDs {
			result := tx.Model(&mediaLibraryModel{}).Where("id = ?", id).Updates(map[string]any{
				"sort_order": position,
				"updated_at": now,
			})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return domain.ErrNotFound
			}
		}
		return nil
	})
}

func withMediaLibraryRoots(db *gorm.DB) *gorm.DB {
	return db.Preload("Roots", func(tx *gorm.DB) *gorm.DB {
		return tx.Order("media_library_roots.position")
	})
}

func libraryRootPaths(input medialibrary.LibraryInput) ([]string, error) {
	if len(input.RootPaths) > 0 {
		return input.RootPaths, nil
	}
	if input.RootPath != "" {
		return []string{input.RootPath}, nil
	}
	return nil, fmt.Errorf("%w: at least one root path is required", medialibrary.ErrInvalidLibrary)
}

func libraryScreenshotEnabled(input medialibrary.LibraryInput) bool {
	return input.VideoScreenshotEnabled != nil && *input.VideoScreenshotEnabled
}

func libraryScreenshotPercent(input medialibrary.LibraryInput) int {
	if input.VideoScreenshotPercent == nil {
		return 10
	}
	return *input.VideoScreenshotPercent
}

func libraryPlaybackCompletionPercent(input medialibrary.LibraryInput) int {
	if input.PlaybackCompletionPercent == nil {
		return medialibrary.DefaultPlaybackCompletionPercent
	}
	return *input.PlaybackCompletionPercent
}

func hidePeopleWithoutImages(input medialibrary.LibraryInput) bool {
	return input.HidePeopleWithoutImages != nil && *input.HidePeopleWithoutImages
}

func mediaLibraryRootRecords(libraryID int64, rootPaths []string, now sqliteTimestamp) []mediaLibraryRootModel {
	records := make([]mediaLibraryRootModel, 0, len(rootPaths))
	for position, rootPath := range rootPaths {
		records = append(records, mediaLibraryRootModel{
			LibraryID: libraryID, Position: position, RootPath: rootPath,
			CreatedAt: now, UpdatedAt: now,
		})
	}
	return records
}

func addLibraryToConfiguredAdministrators(tx *gorm.DB, libraryID int64, now sqliteTimestamp) error {
	var administratorIDs []int64
	if err := tx.Model(&localUserModel{}).
		Where("role = ? AND library_grants_configured = ?", domain.LocalUserRoleAdmin, true).
		Pluck("id", &administratorIDs).Error; err != nil {
		return err
	}
	grants := make([]userLibraryGrantModel, 0, len(administratorIDs))
	for _, userID := range administratorIDs {
		grants = append(grants, userLibraryGrantModel{
			UserID: userID, LibraryID: libraryID, AccessLevel: string(domain.LibraryAccessView), CreatedAt: now,
		})
	}
	if len(grants) == 0 {
		return nil
	}
	return tx.Create(&grants).Error
}

func (s *Store) DeleteLibrary(ctx context.Context, id int64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("library_id = ?", id).Delete(&userLibraryGrantModel{}).Error; err != nil {
			return err
		}
		result := tx.Delete(&mediaLibraryModel{}, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
}

func (s *Store) BeginLibraryScan(ctx context.Context, id int64) (int64, error) {
	var generation int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.
			Model(&mediaLibraryModel{}).
			Where("id = ?", id).
			Updates(map[string]any{
				"scan_generation": gorm.Expr("scan_generation + ?", 1),
				"updated_at":      newSQLiteTimestamp(s.now()),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return domain.ErrNotFound
		}
		var record mediaLibraryModel
		if err := tx.Select("scan_generation").Where("id = ?", id).Take(&record).Error; err != nil {
			return err
		}
		generation = record.ScanGeneration
		return nil
	})
	return generation, err
}

func (s *Store) UpsertMediaItem(ctx context.Context, input medialibrary.ItemInput) (medialibrary.Item, bool, error) {
	genres, err := marshalStringSlice(input.Genres)
	if err != nil {
		return medialibrary.Item{}, false, err
	}
	artists, err := marshalStringSlice(input.Artists)
	if err != nil {
		return medialibrary.Item{}, false, err
	}
	sortName := strings.TrimSpace(input.SortName)
	if sortName == "" {
		sortName = input.Name
	}
	now := newSQLiteTimestamp(s.now())
	incoming := mediaItemModel{
		LibraryID: input.LibraryID, ParentID: input.ParentID, Kind: string(input.Kind), Path: input.Path,
		MediaPath: input.MediaPath, Name: input.Name, SortName: sortName, SortKey: mediaSortKey(sortName), Container: input.Container,
		SizeBytes: input.SizeBytes, ModifiedAt: newNullableSQLiteTimestamp(input.ModifiedAt), MetadataPriorityKey: input.MetadataPriorityKey, TVDBID: input.TVDBID, MetadataSource: input.MetadataSource, ResolvedMetadataSource: input.ResolvedMetadataSource, TMDBID: input.TMDBID, IMDbID: input.IMDbID,
		GenresJSON: genres, SeasonNumber: input.SeasonNumber, EpisodeNumber: input.EpisodeNumber,
		ProductionYear: input.ProductionYear, PremiereDate: input.PremiereDate, Overview: input.Overview,
		CommunityRating: input.CommunityRating, RuntimeTicks: input.RuntimeTicks, ImageURL: input.ImageURL,
		BackdropURL: input.BackdropURL, Album: input.Album, AlbumArtist: input.AlbumArtist,
		ArtistsJSON: artists, TrackNumber: input.TrackNumber, DiscNumber: input.DiscNumber,
		MusicSource: input.MusicSource, MusicTrackID: input.MusicTrackID, MusicPictureID: input.MusicPictureID,
		MusicLyricID: input.MusicLyricID, MusicMetadataKey: input.MusicMetadataKey, Lyrics: input.Lyrics,
		TranslatedLyrics: input.TranslatedLyrics,
		ScanGeneration:   input.ScanGeneration, CreatedAt: now, UpdatedAt: now,
	}
	added := false
	metadataRefreshNeeded := false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing mediaItemModel
		err := tx.Where("library_id = ? AND path = ? AND kind = ?", input.LibraryID, input.Path, input.Kind).Take(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := tx.Create(&incoming).Error; err != nil {
				return err
			}
			added = true
			metadataRefreshNeeded = true
			return nil
		}
		if err != nil {
			return err
		}
		if input.Kind == medialibrary.KindAudio {
			metadataRefreshNeeded = !existing.MetadataLocked &&
				(existing.MusicTrackID == "" || existing.MusicMetadataKey != incoming.MusicMetadataKey ||
					mediaItemModifiedAtChanged(existing.ModifiedAt, incoming.ModifiedAt) || !existing.MetadataLocalized)
		} else {
			metadataRefreshNeeded = !existing.MetadataLocked &&
				(existing.TMDBID == nil && existing.TVDBID == nil || mediaItemModifiedAtChanged(existing.ModifiedAt, incoming.ModifiedAt))
		}
		mergeMediaItem(&existing, incoming)
		incoming.ID = existing.ID
		if err := tx.Select("*").Omit("Library", "Parent").Save(&existing).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return medialibrary.Item{}, false, err
	}
	item, err := s.GetMediaItem(ctx, incoming.ID)
	item.MetadataRefreshNeeded = metadataRefreshNeeded
	return item, added, err
}

// ReassociateMediaItem moves a newly indexed temporary item onto a matching
// item from an earlier scan while retaining the earlier row's identity and
// related user data. Ambiguous or conflicting candidates are intentionally
// ignored so a scan can fall back to normal path-based indexing.
func (s *Store) ReassociateMediaItem(ctx context.Context, itemID int64, input medialibrary.ItemInput) (medialibrary.Item, bool, error) {
	if itemID <= 0 || input.LibraryID <= 0 || input.ScanGeneration <= 0 {
		return medialibrary.Item{}, false, nil
	}
	var reassociatedID int64
	reassociated := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var temporary mediaItemModel
		if err := tx.Where("id = ? AND library_id = ? AND kind = ? AND path = ? AND scan_generation = ?", itemID, input.LibraryID, input.Kind, input.Path, input.ScanGeneration).Take(&temporary).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		candidate, found, err := findReassociationCandidate(tx, temporary, input)
		if err != nil || !found {
			return err
		}
		if mediaItemHasAttachedState(tx, temporary.ID) {
			return nil
		}
		mergeMediaItem(&candidate, temporary)
		candidate.Path = temporary.Path
		candidate.MediaPath = temporary.MediaPath
		candidate.ParentID = temporary.ParentID
		candidate.ScanGeneration = temporary.ScanGeneration
		candidate.UpdatedAt = temporary.UpdatedAt
		// Remove the temporary row first so the unique (library, path, kind)
		// index does not reject the candidate's new path.
		result := tx.Delete(&mediaItemModel{}, temporary.ID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return nil
		}
		if err := tx.Select("*").Omit("Library", "Parent").Save(&candidate).Error; err != nil {
			return err
		}
		reassociatedID = candidate.ID
		reassociated = true
		return nil
	})
	if err != nil || !reassociated {
		return medialibrary.Item{}, false, err
	}
	item, err := s.GetMediaItem(ctx, reassociatedID)
	return item, true, err
}

func findReassociationCandidate(tx *gorm.DB, temporary mediaItemModel, input medialibrary.ItemInput) (mediaItemModel, bool, error) {
	if temporary.ID <= 0 || input.Kind != medialibrary.KindMovie && input.Kind != medialibrary.KindSeries && input.Kind != medialibrary.KindSeason && input.Kind != medialibrary.KindEpisode {
		return mediaItemModel{}, false, nil
	}
	query := tx.Where("library_id = ? AND kind = ? AND scan_generation <> ? AND id <> ?", input.LibraryID, input.Kind, input.ScanGeneration, temporary.ID)
	var candidates []mediaItemModel
	switch input.Kind {
	case medialibrary.KindMovie, medialibrary.KindSeries:
		if (input.TMDBID == nil || *input.TMDBID <= 0) && (input.TVDBID == nil || *input.TVDBID <= 0) {
			return mediaItemModel{}, false, nil
		}
		query = sourceIdentityQuery(query, input)
	case medialibrary.KindSeason:
		if input.ParentID == nil || input.SeasonNumber == nil {
			return mediaItemModel{}, false, nil
		}
		query = query.Where("parent_id = ? AND season_number = ?", *input.ParentID, *input.SeasonNumber)
	case medialibrary.KindEpisode:
		if input.ParentID == nil || input.SeasonNumber == nil || input.EpisodeNumber == nil {
			return mediaItemModel{}, false, nil
		}
		query = query.Where("parent_id = ? AND season_number = ? AND episode_number = ?", *input.ParentID, *input.SeasonNumber, *input.EpisodeNumber)
	}
	if err := query.Order("id").Find(&candidates).Error; err != nil {
		return mediaItemModel{}, false, err
	}
	if len(candidates) != 1 {
		return mediaItemModel{}, false, nil
	}
	candidate := candidates[0]
	occupiedQuery := tx.Model(&mediaItemModel{}).Where("library_id = ? AND kind = ? AND scan_generation = ? AND id <> ?", input.LibraryID, input.Kind, input.ScanGeneration, temporary.ID)
	switch input.Kind {
	case medialibrary.KindMovie, medialibrary.KindSeries:
		occupiedQuery = sourceIdentityQuery(occupiedQuery, input)
	case medialibrary.KindSeason:
		occupiedQuery = occupiedQuery.Where("parent_id = ? AND season_number = ?", *input.ParentID, *input.SeasonNumber)
	case medialibrary.KindEpisode:
		occupiedQuery = occupiedQuery.Where("parent_id = ? AND season_number = ? AND episode_number = ?", *input.ParentID, *input.SeasonNumber, *input.EpisodeNumber)
	}
	var occupied int64
	if err := occupiedQuery.Count(&occupied).Error; err != nil {
		return mediaItemModel{}, false, err
	}
	if occupied != 0 {
		return mediaItemModel{}, false, nil
	}
	return candidate, true, nil
}

func mediaItemHasAttachedState(tx *gorm.DB, itemID int64) bool {
	for _, table := range []string{"media_user_data", "media_item_images", "media_item_people", "media_metadata_bindings"} {
		var count int64
		if tx.Table(table).Where("item_id = ?", itemID).Count(&count).Error != nil || count != 0 {
			return true
		}
	}
	return false
}

func reassociationTitleMatches(left, right string) bool {
	left = normalizeReassociationTitle(left)
	right = normalizeReassociationTitle(right)
	return left != "" && left == right
}

func normalizeReassociationTitle(value string) string {
	var normalized strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			normalized.WriteRune(r)
		}
	}
	return normalized.String()
}

func reassociationYearsMatch(left, right *int) bool {
	return left == nil || right == nil || *left == *right
}

func mediaItemModifiedAtChanged(existing, incoming *sqliteTimestamp) bool {
	if existing == nil || incoming == nil {
		return existing != nil || incoming != nil
	}
	return !existing.Time().Equal(incoming.Time())
}

func mergeMediaItem(existing *mediaItemModel, incoming mediaItemModel) {
	existing.ParentID = incoming.ParentID
	existing.MediaPath = incoming.MediaPath
	existing.Container = incoming.Container
	existing.SizeBytes = incoming.SizeBytes
	existing.ModifiedAt = incoming.ModifiedAt
	preserveMusicMetadata := existing.Kind == string(medialibrary.KindAudio) && existing.MusicTrackID != "" && incoming.MusicTrackID == ""
	if !existing.MetadataLocked && !preserveMusicMetadata {
		// Discovery-only updates must not erase a previously resolved title. A
		// validated provider result replaces its whole presentation, including blanks.
		if incoming.ResolvedMetadataSource != "" {
			if incoming.TVDBID != nil {
				existing.TVDBID = incoming.TVDBID
			}
			existing.ResolvedMetadataSource = incoming.ResolvedMetadataSource
			existing.MetadataPriorityKey = incoming.MetadataPriorityKey
			existing.IMDbID = incoming.IMDbID
			existing.GenresJSON = incoming.GenresJSON
			existing.Overview = incoming.Overview
			existing.CommunityRating = incoming.CommunityRating
			existing.RuntimeTicks = incoming.RuntimeTicks
			existing.PremiereDate = incoming.PremiereDate
			existing.ImageURL = incoming.ImageURL
			existing.BackdropURL = incoming.BackdropURL
			existing.MetadataLocalized = false
		} else if existing.TMDBID != nil || existing.TVDBID != nil {
			incoming.Name = existing.Name
			incoming.SortName = existing.SortName
			incoming.SortKey = existing.SortKey
		}

		existing.Name = incoming.Name
		existing.SortName = incoming.SortName
		existing.SortKey = incoming.SortKey
		if incoming.TMDBID != nil {
			existing.TMDBID = incoming.TMDBID
		}
		if incoming.IMDbID != "" {
			existing.IMDbID = incoming.IMDbID
		}
		if incoming.GenresJSON != "[]" {
			existing.GenresJSON = incoming.GenresJSON
		}
		existing.SeasonNumber = incoming.SeasonNumber
		existing.EpisodeNumber = incoming.EpisodeNumber
		if incoming.ProductionYear != nil {
			existing.ProductionYear = incoming.ProductionYear
		}
		if incoming.PremiereDate != "" {
			existing.PremiereDate = incoming.PremiereDate
		}
		if incoming.Overview != "" {
			existing.Overview = incoming.Overview
		}
		if incoming.CommunityRating != 0 {
			existing.CommunityRating = incoming.CommunityRating
		}
		if incoming.RuntimeTicks != 0 {
			existing.RuntimeTicks = incoming.RuntimeTicks
		}
		if incoming.ImageURL != "" {
			existing.ImageURL = incoming.ImageURL
		}
		if incoming.BackdropURL != "" {
			existing.BackdropURL = incoming.BackdropURL
		}
		existing.Album = incoming.Album
		existing.AlbumArtist = incoming.AlbumArtist
		existing.ArtistsJSON = incoming.ArtistsJSON
		existing.TrackNumber = incoming.TrackNumber
		existing.DiscNumber = incoming.DiscNumber
		existing.MusicSource = incoming.MusicSource
		existing.MusicTrackID = incoming.MusicTrackID
		existing.MusicPictureID = incoming.MusicPictureID
		existing.MusicLyricID = incoming.MusicLyricID
		existing.MusicMetadataKey = incoming.MusicMetadataKey
		if incoming.Lyrics != "" {
			existing.Lyrics = incoming.Lyrics
		}
		if incoming.TranslatedLyrics != "" {
			existing.TranslatedLyrics = incoming.TranslatedLyrics
		}
	}
	existing.ScanGeneration = incoming.ScanGeneration
	existing.UpdatedAt = incoming.UpdatedAt
}

func (s *Store) FinishLibraryScan(ctx context.Context, libraryID, generation int64) (int, error) {
	removed := int64(0)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var library mediaLibraryModel
		if err := tx.Select("id", "scan_generation").Where("id = ?", libraryID).Take(&library).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.ErrNotFound
		} else if err != nil {
			return err
		}
		if library.ScanGeneration != generation {
			return fmt.Errorf("media library scan was superseded")
		}
		result := tx.Where("library_id = ? AND scan_generation <> ?", libraryID, generation).Delete(&mediaItemModel{})
		if result.Error != nil {
			return result.Error
		}
		removed = result.RowsAffected
		now := newSQLiteTimestamp(s.now())
		result = tx.Model(&mediaLibraryModel{}).
			Where("id = ? AND scan_generation = ?", libraryID, generation).
			Updates(map[string]any{"last_scanned_at": now, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
	return int(removed), err
}

func (s *Store) GetMediaItem(ctx context.Context, id int64) (medialibrary.Item, error) {
	return s.GetMediaItemForUser(ctx, 1, id)
}

func (s *Store) UpdateMediaItemMetadata(ctx context.Context, id int64, input medialibrary.MetadataInput) error {
	if input.MetadataBindingSet {
		return fmt.Errorf("%w: episode movie bindings are no longer supported", medialibrary.ErrInvalidLibrary)
	}
	if input.MetadataSource == "" {
		input.MetadataSource = "auto"
	}

	genres, err := marshalStringSlice(input.Genres)
	if err != nil {
		return err
	}
	artists, err := marshalStringSlice(input.Artists)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&mediaItemModel{}).Where("id = ?", id).Updates(map[string]any{
			"name": input.Name, "sort_name": input.SortName, "sort_key": mediaSortKey(input.SortName), "tmdb_id": input.TMDBID, "tvdb_id": input.TVDBID, "metadata_source": input.MetadataSource, "resolved_metadata_source": input.ResolvedMetadataSource, "imdb_id": input.IMDbID,
			"genres_json": genres, "season_number": input.SeasonNumber, "episode_number": input.EpisodeNumber,
			"production_year": input.ProductionYear, "premiere_date": input.PremiereDate, "overview": input.Overview,
			"community_rating": input.CommunityRating, "runtime_ticks": input.RuntimeTicks, "image_url": input.ImageURL,
			"backdrop_url": input.BackdropURL, "album": input.Album, "album_artist": input.AlbumArtist,
			"artists_json": artists, "track_number": input.TrackNumber, "disc_number": input.DiscNumber,
			"lyrics": input.Lyrics, "translated_lyrics": input.TranslatedLyrics,
			"metadata_locked": gorm.Expr("CASE WHEN ? THEN metadata_locked ELSE 1 END", input.PreserveLock), "metadata_priority_key": input.MetadataPriorityKey, "metadata_localized": false, "updated_at": newSQLiteTimestamp(s.now()),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
}

// UpdateMediaItemImageURL replaces one source without changing metadata or locks.
func (s *Store) UpdateMediaItemImageURL(ctx context.Context, id int64, imageType medialibrary.ImageType, imageURL string) error {
	column := "image_url"
	if imageType == medialibrary.ImageBackdrop {
		column = "backdrop_url"
	} else if imageType != medialibrary.ImagePrimary {
		return fmt.Errorf("%w: unsupported image type", medialibrary.ErrInvalidLibrary)
	}
	result := s.db.WithContext(ctx).Model(&mediaItemModel{}).Where("id = ?", id).Updates(map[string]any{
		column: imageURL, "updated_at": newSQLiteTimestamp(s.now()),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) GetMediaItemForUser(ctx context.Context, userID, id int64) (medialibrary.Item, error) {
	return getMediaItemForUser(ctx, s.db, userID, id)
}

func getMediaItemForUser(ctx context.Context, db *gorm.DB, userID, id int64) (medialibrary.Item, error) {
	var record mediaItemModel
	err := db.WithContext(ctx).Where("id = ?", id).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return medialibrary.Item{}, domain.ErrNotFound
	}
	if err != nil {
		return medialibrary.Item{}, err
	}
	var userData mediaUserDataModel
	err = db.WithContext(ctx).Where("user_id = ? AND item_id = ?", userID, id).Take(&userData).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return medialibrary.Item{}, err
	}
	item, err := mediaItemFromModel(record, userData)
	if err != nil {
		return medialibrary.Item{}, err
	}
	bindings, err := mediaMetadataBindingsByItem(ctx, db, []mediaItemModel{record})
	if err != nil {
		return medialibrary.Item{}, err
	}
	item.MetadataBinding = bindings[record.ID]
	counts, err := mediaUnplayedCounts(ctx, db, userID, []mediaItemModel{record})
	if err != nil {
		return medialibrary.Item{}, err
	}
	if count, ok := counts[record.ID]; ok {
		item.UserData.UnplayedItemCount = &count
	}
	return item, nil
}

func (s *Store) ListMediaItems(ctx context.Context, query medialibrary.ItemQuery) (medialibrary.ItemPage, error) {
	return s.ListMediaItemsForUser(ctx, 1, query)
}

func (s *Store) ListMediaItemsForUser(ctx context.Context, userID int64, query medialibrary.ItemQuery) (medialibrary.ItemPage, error) {
	db := s.db.WithContext(ctx).Model(&mediaItemModel{})
	if query.AllowedLibraryIDs != nil {
		if len(query.AllowedLibraryIDs) == 0 {
			return medialibrary.ItemPage{Items: []medialibrary.Item{}, Total: 0}, nil
		}
		db = db.Where("media_items.library_id IN ?", query.AllowedLibraryIDs)
	}
	if query.LibraryID != nil {
		db = db.Where("media_items.library_id = ?", *query.LibraryID)
	}
	if query.ParentSet && !query.Recursive {
		if query.ParentID == nil {
			db = db.Where("media_items.parent_id IS NULL")
		} else {
			db = db.Where("media_items.parent_id = ?", *query.ParentID)
		}
	}
	if query.Recursive && query.AncestorID != nil {
		descendants := s.db.Raw(`WITH RECURSIVE descendants(id) AS (
			SELECT id FROM media_items WHERE parent_id = ?
			UNION ALL
			SELECT child.id FROM media_items child JOIN descendants parent ON child.parent_id = parent.id
		) SELECT id FROM descendants`, *query.AncestorID)
		db = db.Where("media_items.id IN (?)", descendants)
	}
	if len(query.Kinds) > 0 {
		kinds := make([]string, len(query.Kinds))
		for index, kind := range query.Kinds {
			kinds[index] = string(kind)
		}
		db = db.Where("media_items.kind IN ?", kinds)
	}
	if len(query.MusicArtistIDs) > 0 {
		db = db.Where(`(
			(media_items.kind = ? AND media_items.id IN ?) OR
			(media_items.kind = ? AND media_items.parent_id IN ?) OR
			(media_items.kind = ? AND EXISTS (
				SELECT 1 FROM media_items music_album
				WHERE music_album.id = media_items.parent_id AND music_album.parent_id IN ?
			))
		)`,
			string(medialibrary.KindMusicArtist), query.MusicArtistIDs,
			string(medialibrary.KindMusicAlbum), query.MusicArtistIDs,
			string(medialibrary.KindAudio), query.MusicArtistIDs,
		)
	}
	if len(query.PersonIDs) > 0 {
		// Only direct episode credits identify guest appearances. Parent cast
		// suppresses an episode for that same person, never for other matches.
		db = db.Where(`EXISTS (
			SELECT 1 FROM media_item_people credit
			JOIN media_items related ON related.id = credit.item_id
			WHERE credit.person_id IN ? AND related.library_id = media_items.library_id AND (
				(media_items.kind = 'movie' AND related.id = media_items.id) OR
				(media_items.kind = 'series' AND (related.id = media_items.id OR
					(related.kind = 'season' AND related.parent_id = media_items.id))) OR
				(media_items.kind = 'episode' AND related.id = media_items.id AND (
					credit.type <> 'Actor' OR NOT EXISTS (
						SELECT 1 FROM media_items parent
						JOIN media_items series ON series.id = CASE WHEN parent.kind = 'season' THEN parent.parent_id ELSE parent.id END
						JOIN media_items regular_item ON regular_item.id = series.id OR
							(regular_item.kind = 'season' AND regular_item.parent_id = series.id)
						JOIN media_item_people regular ON regular.item_id = regular_item.id
						WHERE parent.id = media_items.parent_id AND series.kind = 'series'
							AND series.library_id = media_items.library_id
							AND regular_item.library_id = media_items.library_id
							AND regular.person_id = credit.person_id AND regular.type = 'Actor'
					)
				))
			)
		)`, query.PersonIDs)
	}
	if search := strings.TrimSpace(query.Search); search != "" {
		db = db.Where("media_items.name LIKE ? ESCAPE '\\'", "%"+escapeLike(search)+"%")
	}
	if query.Resume || query.FavoriteOnly {
		db = db.Joins("LEFT JOIN media_user_data ON media_user_data.item_id = media_items.id AND media_user_data.user_id = ?", userID)
	}
	if query.Resume {
		db = db.Where("COALESCE(media_user_data.playback_position_ticks, 0) > 0").
			Where("COALESCE(media_user_data.played, 0) = 0").
			Where("COALESCE(media_user_data.hide_from_resume, 0) = 0").
			Where("media_items.kind <> ?", string(medialibrary.KindAudio))
	}
	if query.FavoriteOnly {
		db = db.Where("COALESCE(media_user_data.favorite, 0) = 1")
	}
	var total int64
	if !query.SkipTotal {
		if err := db.Session(&gorm.Session{}).Count(&total).Error; err != nil {
			return medialibrary.ItemPage{}, err
		}
	}
	limit := query.Limit
	if limit > 500 {
		limit = 100
	}
	start := query.Start
	if start < 0 {
		start = 0
	}
	if query.SkipTotal && limit > 0 {
		total = int64(start + limit)
	}
	order := mediaItemOrder(query)
	if hasItemKind(query.Kinds, medialibrary.KindSeries) {
		if direction, ok := latestSeriesSortDirection(query); ok {
			db = withLatestSeriesImports(db)
			order = "COALESCE(latest_series_imports.latest_created_at, media_items.created_at) " + direction +
				", media_items.id " + direction
		}
	}
	if order == "" && query.Latest {
		if !hasItemKind(query.Kinds, medialibrary.KindSeries) {
			order = "media_items.created_at DESC, media_items.id DESC"
		}
	} else if order == "" && query.Resume {
		order = "media_user_data.last_played_at DESC, media_items.id DESC"
	}
	if order == "" {
		order = "media_items.sort_key COLLATE NOCASE, media_items.sort_name COLLATE NOCASE, media_items.id"
	}
	session := db.Session(&gorm.Session{}).Order(order).Offset(start)
	if limit > 0 {
		session = session.Limit(limit)
	}
	var records []mediaItemModel
	if err := session.Find(&records).Error; err != nil {
		return medialibrary.ItemPage{}, err
	}
	if query.SkipTotal && limit <= 0 {
		total = int64(start + len(records))
	}
	userData, err := mediaUserDataByItem(ctx, s.db, userID, records)
	if err != nil {
		return medialibrary.ItemPage{}, err
	}
	unplayedCounts, err := mediaUnplayedCounts(ctx, s.db, userID, records)
	if err != nil {
		return medialibrary.ItemPage{}, err
	}
	bindings, err := mediaMetadataBindingsByItem(ctx, s.db, records)
	if err != nil {
		return medialibrary.ItemPage{}, err
	}
	items := make([]medialibrary.Item, 0, len(records))
	for _, record := range records {
		item, err := mediaItemFromModel(record, userData[record.ID])
		if err != nil {
			return medialibrary.ItemPage{}, err
		}
		if count, ok := unplayedCounts[record.ID]; ok {
			item.UserData.UnplayedItemCount = &count
		}
		item.MetadataBinding = bindings[record.ID]
		items = append(items, item)
	}
	if err := s.populateEpisodeSeries(ctx, items); err != nil {
		return medialibrary.ItemPage{}, err
	}
	return medialibrary.ItemPage{Items: items, Total: int(total)}, nil
}

// populateEpisodeSeries resolves a page's parent titles without per-item queries.
func (s *Store) populateEpisodeSeries(ctx context.Context, items []medialibrary.Item) error {
	ids := make([]int64, 0)
	for _, item := range items {
		if item.Kind == medialibrary.KindEpisode {
			ids = append(ids, item.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var rows []struct {
		ID         int64
		SeriesID   int64
		SeriesName string
	}
	if err := s.db.WithContext(ctx).Table("media_items AS episode").
		Select("episode.id, series.id AS series_id, series.name AS series_name").
		Joins("JOIN media_items parent ON parent.id = episode.parent_id AND parent.library_id = episode.library_id").
		Joins("JOIN media_items series ON series.id = CASE WHEN parent.kind = 'season' THEN parent.parent_id ELSE parent.id END AND series.kind = 'series' AND series.library_id = episode.library_id").
		Where("episode.id IN ?", ids).Scan(&rows).Error; err != nil {
		return err
	}
	byID := make(map[int64]int, len(items))
	for index := range items {
		byID[items[index].ID] = index
	}
	for _, row := range rows {
		item := &items[byID[row.ID]]
		item.SeriesID = &row.SeriesID
		item.SeriesName = row.SeriesName
	}
	return nil
}

func latestSeriesSortDirection(query medialibrary.ItemQuery) (string, bool) {
	if query.Latest && strings.TrimSpace(query.SortBy) == "" {
		return "DESC", true
	}
	fields := strings.Split(query.SortBy, ",")
	if len(fields) != 1 {
		return "", false
	}
	switch strings.ToLower(strings.TrimSpace(fields[0])) {
	case "datelastcontentadded", "latestcontentadded", "latest_content_added":
		direction := "ASC"
		if query.SortDescending {
			direction = "DESC"
		}
		return direction, true
	default:
		return "", false
	}
}

func withLatestSeriesImports(db *gorm.DB) *gorm.DB {
	return db.Joins(`LEFT JOIN (
		SELECT episode_imports.series_id, MAX(episode_imports.created_at) AS latest_created_at
		FROM (
			SELECT seasons.parent_id AS series_id, episodes.created_at
			FROM media_items seasons
			JOIN media_items episodes ON episodes.parent_id = seasons.id AND episodes.kind = ?
			WHERE seasons.kind = ?
			UNION ALL
			SELECT episodes.parent_id AS series_id, episodes.created_at
			FROM media_items episodes
			JOIN media_items direct_series ON direct_series.id = episodes.parent_id AND direct_series.kind = ?
			WHERE episodes.kind = ?
		) episode_imports
		GROUP BY episode_imports.series_id
	) latest_series_imports ON latest_series_imports.series_id = media_items.id`,
		string(medialibrary.KindEpisode), string(medialibrary.KindSeason),
		string(medialibrary.KindSeries), string(medialibrary.KindEpisode))
}

func mediaUnplayedCounts(ctx context.Context, db *gorm.DB, userID int64, records []mediaItemModel) (map[int64]int, error) {
	folderIDs := make([]int64, 0, len(records))
	for _, record := range records {
		if record.Kind == string(medialibrary.KindSeries) || record.Kind == string(medialibrary.KindSeason) {
			folderIDs = append(folderIDs, record.ID)
		}
	}
	result := make(map[int64]int, len(folderIDs))
	for _, id := range folderIDs {
		result[id] = 0
	}
	if len(folderIDs) == 0 {
		return result, nil
	}
	type countRow struct {
		RootID  int64
		Total   int64
		Watched int64
	}
	var rows []countRow
	err := db.WithContext(ctx).Raw(`WITH RECURSIVE descendants(root_id, id, kind) AS (
		SELECT id, id, kind FROM media_items WHERE id IN ?
		UNION ALL
		SELECT parent.root_id, child.id, child.kind
		FROM media_items child JOIN descendants parent ON child.parent_id = parent.id
	)
	SELECT descendants.root_id,
		COUNT(*) AS total,
		COALESCE(SUM(CASE WHEN user_data.played = 1 THEN 1 ELSE 0 END), 0) AS watched
	FROM descendants
	LEFT JOIN media_user_data user_data ON user_data.item_id = descendants.id AND user_data.user_id = ?
	WHERE descendants.kind = ?
	GROUP BY descendants.root_id`, folderIDs, userID, string(medialibrary.KindEpisode)).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.RootID] = int(row.Total - row.Watched)
	}
	return result, nil
}

func hasItemKind(kinds []medialibrary.ItemKind, want medialibrary.ItemKind) bool {
	for _, kind := range kinds {
		if kind == want {
			return true
		}
	}
	return false
}

func mediaItemOrder(query medialibrary.ItemQuery) string {
	fields := strings.Split(query.SortBy, ",")
	parts := make([]string, 0, len(fields)+1)
	direction := "ASC"
	if query.SortDescending {
		direction = "DESC"
	}
	for _, raw := range fields {
		field := strings.ToLower(strings.TrimSpace(raw))
		column := ""
		switch field {
		case "name", "sortname", "sort_name", "sorttitle":
			parts = append(parts,
				"media_items.sort_key COLLATE NOCASE "+direction,
				"media_items.sort_name COLLATE NOCASE "+direction,
			)
			continue
		case "premieredate", "premiere_date", "releasedate":
			column = "media_items.premiere_date"
		case "latestepisodedate", "latest_episode_date":
			column = `COALESCE(
				(SELECT MAX(episodes.premiere_date)
				 FROM media_items AS seasons
				 JOIN media_items AS episodes ON episodes.parent_id = seasons.id
				 WHERE seasons.parent_id = media_items.id AND episodes.kind = 'episode'),
				(SELECT MAX(episodes.premiere_date)
				 FROM media_items AS episodes
				 WHERE episodes.parent_id = media_items.id AND episodes.kind = 'episode'),
				media_items.premiere_date
			)`
		case "datelastcontentadded", "latestcontentadded", "latest_content_added":
			column = `COALESCE(
				(SELECT MAX(episodes.created_at)
				 FROM media_items AS seasons
				 JOIN media_items AS episodes ON episodes.parent_id = seasons.id
				 WHERE seasons.parent_id = media_items.id AND episodes.kind = 'episode'),
				(SELECT MAX(episodes.created_at)
				 FROM media_items AS episodes
				 WHERE episodes.parent_id = media_items.id AND episodes.kind = 'episode'),
				media_items.created_at
			)`
		case "productionyear", "production_year":
			column = "media_items.production_year"
		case "datecreated", "created", "created_at":
			column = "media_items.created_at"
		case "communityrating", "rating", "criticrating":
			column = "media_items.community_rating"
		case "runtime":
			column = "media_items.runtime_ticks"
		case "indexnumber", "episodenumber", "episode_number":
			column = "media_items.episode_number"
		case "parentindexnumber", "seasonnumber", "season_number":
			column = "media_items.season_number"
		default:
			continue
		}
		parts = append(parts, "CASE WHEN "+column+" IS NULL THEN 1 ELSE 0 END ASC, "+column+" "+direction)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ") + ", media_items.id " + direction
}

func mediaUserDataByItem(ctx context.Context, db *gorm.DB, userID int64, items []mediaItemModel) (map[int64]mediaUserDataModel, error) {
	result := make(map[int64]mediaUserDataModel, len(items))
	if len(items) == 0 {
		return result, nil
	}
	ids := make([]int64, len(items))
	for index, item := range items {
		ids[index] = item.ID
	}
	var records []mediaUserDataModel
	if err := db.WithContext(ctx).Where("user_id = ? AND item_id IN ?", userID, ids).Find(&records).Error; err != nil {
		return nil, err
	}
	for _, record := range records {
		result[record.ItemID] = record
	}
	return result, nil
}

func (s *Store) UpdateMediaUserData(ctx context.Context, itemID int64, data medialibrary.UserData) (medialibrary.UserData, error) {
	return s.UpdateMediaUserDataForUser(ctx, 1, itemID, data)
}

func (s *Store) UpdateMediaUserDataForUser(ctx context.Context, userID, itemID int64, data medialibrary.UserData) (medialibrary.UserData, error) {
	var count int64
	if err := s.db.WithContext(ctx).Model(&mediaItemModel{}).Where("id = ?", itemID).Count(&count).Error; err != nil {
		return medialibrary.UserData{}, err
	}
	if count == 0 {
		return medialibrary.UserData{}, domain.ErrNotFound
	}
	record := mediaUserDataModel{
		HideFromResume: data.HideFromResume, UserID: userID, ItemID: itemID, PlaybackPositionTicks: data.PlaybackPositionTicks, Played: data.Played,
		PlayCount: data.PlayCount, Favorite: data.Favorite, LastPlayedAt: newNullableSQLiteTimestamp(data.LastPlayedAt), UpdatedAt: newSQLiteTimestamp(s.now()),
	}
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "item_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"hide_from_resume", "playback_position_ticks", "played", "play_count", "favorite", "last_played_at", "updated_at",
		}),
	}).Create(&record).Error
	if err != nil {
		return medialibrary.UserData{}, err
	}
	return mediaUserDataFromModel(record), nil
}

func mediaLibraryFromModel(record mediaLibraryModel) medialibrary.Library {
	rootPaths := make([]string, 0, len(record.Roots))
	for _, root := range record.Roots {
		rootPaths = append(rootPaths, root.RootPath)
	}
	if len(rootPaths) == 0 && record.RootPath != "" {
		rootPaths = []string{record.RootPath}
	}
	return medialibrary.Library{
		ID: record.ID, Name: record.Name, ContentType: medialibrary.ContentType(record.ContentType), SortOrder: record.SortOrder, RootPath: record.RootPath, RootPaths: rootPaths,
		VideoScreenshotEnabled: record.VideoScreenshotEnabled, VideoScreenshotPercent: record.VideoScreenshotPercent,
		PlaybackCompletionPercent: record.PlaybackCompletionPercent,
		HidePeopleWithoutImages:   record.HidePeopleWithoutImages,
		ScanGeneration:            record.ScanGeneration, LastScannedAt: timeFromNullableSQLiteTimestamp(record.LastScannedAt),
		CreatedAt: record.CreatedAt.Time(), UpdatedAt: record.UpdatedAt.Time(),
	}
}

func mediaItemFromModel(record mediaItemModel, userData mediaUserDataModel) (medialibrary.Item, error) {
	genres := []string{}
	artists := []string{}
	if err := json.Unmarshal([]byte(record.GenresJSON), &genres); err != nil {
		return medialibrary.Item{}, err
	}
	if err := json.Unmarshal([]byte(record.ArtistsJSON), &artists); err != nil {
		return medialibrary.Item{}, err
	}
	return medialibrary.Item{
		ID: record.ID, LibraryID: record.LibraryID, ParentID: record.ParentID, Kind: medialibrary.ItemKind(record.Kind),
		Path: record.Path, MediaPath: record.MediaPath, Name: record.Name, SortName: record.SortName,
		Container: record.Container, SizeBytes: record.SizeBytes, ModifiedAt: timeFromNullableSQLiteTimestamp(record.ModifiedAt),
		MetadataPriorityKey: record.MetadataPriorityKey, TVDBID: record.TVDBID, MetadataSource: record.MetadataSource, ResolvedMetadataSource: record.ResolvedMetadataSource, TMDBID: record.TMDBID, IMDbID: record.IMDbID, Genres: genres, SeasonNumber: record.SeasonNumber,
		EpisodeNumber: record.EpisodeNumber, ProductionYear: record.ProductionYear, PremiereDate: record.PremiereDate,
		Overview: record.Overview, CommunityRating: record.CommunityRating, RuntimeTicks: record.RuntimeTicks,
		ImageURL: record.ImageURL, BackdropURL: record.BackdropURL, Album: record.Album,
		AlbumArtist: record.AlbumArtist, Artists: artists, TrackNumber: record.TrackNumber, DiscNumber: record.DiscNumber,
		Lyrics: record.Lyrics, TranslatedLyrics: record.TranslatedLyrics,
		MusicSource: record.MusicSource, MusicTrackID: record.MusicTrackID, MusicPictureID: record.MusicPictureID,
		MusicLyricID: record.MusicLyricID, MusicMetadataKey: record.MusicMetadataKey,
		UserData: mediaUserDataFromModel(userData), CreatedAt: record.CreatedAt.Time(), UpdatedAt: record.UpdatedAt.Time(),
		MetadataLocalized: record.MetadataLocalized,
		MetadataLocked:    record.MetadataLocked,
	}, nil
}

func mediaUserDataFromModel(record mediaUserDataModel) medialibrary.UserData {
	return medialibrary.UserData{
		HideFromResume: record.HideFromResume, PlaybackPositionTicks: record.PlaybackPositionTicks, Played: record.Played,
		PlayCount: record.PlayCount, Favorite: record.Favorite, LastPlayedAt: timeFromNullableSQLiteTimestamp(record.LastPlayedAt),
	}
}

func marshalStringSlice(values []string) (string, error) {
	if values == nil {
		values = []string{}
	}
	encoded, err := json.Marshal(values)
	return string(encoded), err
}

func escapeLike(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}

var _ medialibrary.Store = (*Store)(nil)

func sourceIdentityQuery(tx *gorm.DB, input medialibrary.ItemInput) *gorm.DB {
	if input.TMDBID != nil && *input.TMDBID > 0 && input.TVDBID != nil && *input.TVDBID > 0 {
		return tx.Where("(tmdb_id = ? OR tvdb_id = ?)", *input.TMDBID, *input.TVDBID)
	}
	if input.TVDBID != nil && *input.TVDBID > 0 {
		return tx.Where("tvdb_id = ?", *input.TVDBID)
	}
	return tx.Where("tmdb_id = ?", *input.TMDBID)
}
func (s *Store) UpdateMediaMetadataBatch(ctx context.Context, updates map[int64]medialibrary.MetadataInput) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		nested := &Store{db: tx, now: s.now}
		for id, input := range updates {
			if err := nested.UpdateMediaItemMetadata(ctx, id, input); err != nil {
				return err
			}
		}
		return nil
	})
}
