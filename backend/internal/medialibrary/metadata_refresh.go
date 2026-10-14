package medialibrary

import (
	"context"
	"fmt"
	"strings"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/metadata"
)

type RefreshMetadataInput struct {
	TMDBID         *int64 `json:"tmdb_id,omitempty"`
	TVDBID         *int64 `json:"tvdb_id,omitempty"`
	MetadataSource string `json:"metadata_source,omitempty"`
}

// RefreshItemMetadataForUser retains the existing TMDB refresh service contract.
func (s *Service) RefreshItemMetadataForUser(ctx context.Context, access UserAccess, id, tmdbID int64) (Item, error) {
	return s.RefreshMetadataForUser(ctx, access, id, RefreshMetadataInput{TMDBID: &tmdbID, MetadataSource: metadata.TMDB})
}

// RefreshMetadataForUser validates the entire remote hierarchy before replacing
// stored metadata. A failed provider request leaves all previous data intact.
func (s *Service) RefreshMetadataForUser(ctx context.Context, access UserAccess, id int64, request RefreshMetadataInput) (Item, error) {
	item, err := s.GetItemForUser(ctx, access, id)
	if err != nil {
		return Item{}, err
	}
	if err = s.authorizeLibraryEdit(ctx, access, item.LibraryID); err != nil {
		return Item{}, err
	}
	if item.Kind != KindMovie && item.Kind != KindSeries {
		return Item{}, fmt.Errorf("%w: refresh requires a movie or series", ErrInvalidLibrary)
	}
	if request.TMDBID != nil {
		if *request.TMDBID <= 0 {
			return Item{}, fmt.Errorf("%w: TMDB ID must be positive", ErrInvalidLibrary)
		}
		item.TMDBID = request.TMDBID
	}
	if request.TVDBID != nil {
		if *request.TVDBID <= 0 {
			return Item{}, fmt.Errorf("%w: TVDB ID must be positive", ErrInvalidLibrary)
		}
		item.TVDBID = request.TVDBID
	}
	if request.MetadataSource != "" {
		item.MetadataSource = request.MetadataSource
	}
	if item.MetadataSource == "" {
		item.MetadataSource = metadata.Auto
	}
	if item.MetadataSource != metadata.Auto && item.MetadataSource != metadata.TMDB && item.MetadataSource != metadata.TVDB {
		return Item{}, fmt.Errorf("%w: invalid metadata source", ErrInvalidLibrary)
	}
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		return Item{}, err
	}
	kind := "movie"
	if item.Kind == KindSeries {
		kind = "tv"
	}
	detail, err := s.sourceRegistry().Resolve(ctx, settings, metadata.Selection{Mode: item.MetadataSource, Kind: kind, Name: item.Name, Year: item.ProductionYear, IDs: sourceIDs(item)})
	if err != nil {
		return Item{}, err
	}
	updates := map[int64]MetadataInput{}
	root := metadataInputFromItem(item)
	applyTMDBDetail(&root, detail, detail.ID)
	root.MetadataPriorityKey = strings.Join(metadata.Priority(settings), ",")
	root.PreserveLock = true
	updates[item.ID] = root
	if item.Kind == KindSeries {
		descendants, err := s.listDescendantsForUser(ctx, access, item.ID)
		if err != nil {
			return Item{}, err
		}
		seasons := map[int64]domain.MediaDetail{}
		for _, child := range descendants {
			if child.Kind != KindSeason || child.SeasonNumber == nil {
				continue
			}
			sd, err := s.sourceRegistry().Detail(ctx, settings, detail.Source, "tv", detail.ID, child.SeasonNumber)
			if err != nil {
				return Item{}, err
			}
			seasons[child.ID] = sd
			if child.MetadataLocked {
				continue
			}
			in := metadataInputFromItem(child)
			applyTMDBSeasonDetail(&in, sd)
			in.PreserveLock = true
			updates[child.ID] = in
		}
		for _, child := range descendants {
			if child.Kind != KindEpisode || child.ParentID == nil || child.EpisodeNumber == nil || child.MetadataLocked {
				continue
			}
			sd, ok := seasons[*child.ParentID]
			if !ok {
				continue
			}
			ep, ok := findEpisodeDetail(sd.Episodes, *child.EpisodeNumber)
			if !ok {
				return Item{}, fmt.Errorf("%w: episode %d is absent from the selected source", ErrInvalidLibrary, child.ID)
			}
			in := metadataInputFromItem(child)
			applyTMDBEpisodeDetail(&in, ep)
			in.PreserveLock = true
			updates[child.ID] = in
		}
	}
	normalized := make(map[int64]MetadataInput, len(updates))
	for id, in := range updates {
		n, err := normalizeMetadata(in)
		if err != nil {
			return Item{}, err
		}
		normalized[id] = n
	}
	// Stores that support atomic metadata batches keep hierarchy replacement in
	// one transaction. The fallback is retained for lightweight test stores.
	if store, ok := s.store.(interface {
		UpdateMediaMetadataBatch(context.Context, map[int64]MetadataInput) error
	}); ok {
		if err := store.UpdateMediaMetadataBatch(ctx, normalized); err != nil {
			return Item{}, err
		}
	} else {
		for id, in := range normalized {
			if err := s.store.UpdateMediaItemMetadata(ctx, id, in); err != nil {
				return Item{}, err
			}
		}
	}
	for id := range normalized {
		if err := s.localizeStoredItem(ctx, id); err != nil {
			return Item{}, err
		}
	}
	return s.GetItemForUser(ctx, access, item.ID)
}
