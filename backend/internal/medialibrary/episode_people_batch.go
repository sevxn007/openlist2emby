package medialibrary

import (
	"context"
	"fmt"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

// ListEpisodePeopleBatchForUser resolves episode-only credits for one season.
// TMDB-backed providers use one season request; other providers retain the
// existing per-episode fallback.
func (s *Service) ListEpisodePeopleBatchForUser(ctx context.Context, access UserAccess, itemIDs []int64, language string) (map[int64][]domain.MediaPerson, error) {
	result := make(map[int64][]domain.MediaPerson, len(itemIDs))
	if len(itemIDs) == 0 {
		return result, nil
	}
	if err := s.requireEnabled(ctx); err != nil {
		return nil, err
	}

	items := make([]Item, 0, len(itemIDs))
	var seasonID int64
	for _, itemID := range itemIDs {
		item, err := s.GetItemForUser(ctx, access, itemID)
		if err != nil {
			return nil, err
		}
		if item.Kind != KindEpisode || item.ParentID == nil || item.EpisodeNumber == nil {
			continue
		}
		if seasonID == 0 {
			seasonID = *item.ParentID
		} else if seasonID != *item.ParentID {
			return nil, fmt.Errorf("%w: episodes must belong to one season", ErrInvalidLibrary)
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return result, nil
	}
	if s.assets != nil {
		for _, item := range items {
			people, itemErr := s.ListEpisodePeopleForUser(ctx, access, item.ID, language)
			if itemErr != nil {
				return nil, itemErr
			}
			result[item.ID] = people
		}
		return result, nil
	}

	provider, apiKey, available, err := s.creditsProvider(ctx)
	if err != nil || !available {
		return result, err
	}
	batchProvider, supportsBatch := provider.(SeasonEpisodeCreditsProvider)
	if !supportsBatch {
		for _, item := range items {
			people, itemErr := s.ListEpisodePeopleForUser(ctx, access, item.ID, language)
			if itemErr != nil {
				return nil, itemErr
			}
			result[item.ID] = people
		}
		return result, nil
	}

	query, found, err := s.creditsQueryForItem(ctx, access, items[0])
	if err != nil || !found {
		return result, err
	}
	query.EpisodeNumber = nil
	peopleByNumber, err := batchProvider.SeasonEpisodeCredits(ctx, apiKey, query, language)
	if err != nil {
		return nil, err
	}
	library, err := s.store.GetLibrary(ctx, items[0].LibraryID)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		result[item.ID] = filterPeopleByImageAvailability(library.HidePeopleWithoutImages, peopleByNumber[*item.EpisodeNumber], false)
	}
	return result, nil
}
