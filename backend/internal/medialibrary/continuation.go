package medialibrary

import (
	"context"
	"fmt"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

func (s *Service) ListNextUpForUser(ctx context.Context, access UserAccess, query NextUpQuery) (ItemPage, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return ItemPage{}, err
	}
	query, err := s.scopeContinuationQuery(ctx, access, query)
	if err != nil {
		return ItemPage{}, err
	}
	store, ok := s.store.(NextUpStore)
	if !ok {
		return ItemPage{}, fmt.Errorf("media store does not support next up queries")
	}
	page, err := store.ListMediaNextUpForUser(ctx, access.UserID, query)
	if err != nil {
		return ItemPage{}, err
	}
	if err := s.projectLocalAssetPage(ctx, page.Items); err != nil {
		return ItemPage{}, err
	}
	return page, nil
}

func (s *Service) scopeContinuationQuery(ctx context.Context, access UserAccess, query NextUpQuery) (NextUpQuery, error) {
	if query.Start < 0 || query.Limit < 0 {
		return NextUpQuery{}, fmt.Errorf("%w: invalid continuation pagination", ErrInvalidLibrary)
	}
	if query.LibraryID != nil {
		if _, err := s.GetLibraryForUser(ctx, access, *query.LibraryID); err != nil {
			return NextUpQuery{}, err
		}
	}
	if query.SeriesID != nil {
		series, err := s.GetItemForUser(ctx, access, *query.SeriesID)
		if err != nil {
			return NextUpQuery{}, err
		}
		if series.Kind != KindSeries {
			return NextUpQuery{}, domain.ErrNotFound
		}
	}
	if query.ParentID != nil {
		seriesID, found, err := s.containingSeriesID(ctx, access, *query.ParentID)
		if err != nil {
			return NextUpQuery{}, err
		}
		if !found || query.SeriesID != nil && *query.SeriesID != seriesID {
			query.SeriesID = nil
			query.AllowedLibraryIDs = []int64{}
			return query, nil
		}
		query.SeriesID = &seriesID
	}
	if !access.IsAdmin || access.FilterVisibleLibraries && !continuationQueryHasDirectScope(query) {
		ids, err := s.store.ListAccessibleLibraryIDs(ctx, access.UserID)
		if err != nil {
			return NextUpQuery{}, err
		}
		query.AllowedLibraryIDs = ids
	}
	return query, nil
}

func continuationQueryHasDirectScope(query NextUpQuery) bool {
	return query.LibraryID != nil || query.SeriesID != nil || query.ParentID != nil
}

func (s *Service) ListContinueWatchingForUser(ctx context.Context, access UserAccess, start, limit int) (ContinueWatchingPage, error) {
	return s.ListContinueWatchingForUserQuery(ctx, access, NextUpQuery{
		EnableResumable: true,
		Start:           start,
		Limit:           limit,
	})
}

func (s *Service) ListContinueWatchingForUserQuery(ctx context.Context, access UserAccess, query NextUpQuery) (ContinueWatchingPage, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return ContinueWatchingPage{}, err
	}
	query.EnableResumable = true
	query, err := s.scopeContinuationQuery(ctx, access, query)
	if err != nil {
		return ContinueWatchingPage{}, err
	}
	store, ok := s.store.(ContinueWatchingStore)
	if !ok {
		return ContinueWatchingPage{}, fmt.Errorf("media store does not support continue watching queries")
	}
	page, err := store.ListMediaContinueWatchingForUser(ctx, access.UserID, query)
	if err != nil {
		return ContinueWatchingPage{}, err
	}
	items := make([]Item, len(page.Items))
	for index := range page.Items {
		items[index] = page.Items[index].Item
	}
	if err := s.projectLocalAssetPage(ctx, items); err != nil {
		return ContinueWatchingPage{}, err
	}
	for index := range page.Items {
		page.Items[index].Item = items[index]
	}
	return page, nil
}

func (s *Service) NextEpisodeForUser(ctx context.Context, access UserAccess, itemID int64) (Item, error) {
	seriesID, found, err := s.containingSeriesID(ctx, access, itemID)
	if err != nil || !found {
		return Item{}, err
	}
	page, err := s.ListNextUpForUser(ctx, access, NextUpQuery{
		SeriesID:        &seriesID,
		EnableResumable: true,
		Limit:           1,
	})
	if err != nil || len(page.Items) == 0 {
		return Item{}, err
	}
	return page.Items[0], nil
}

func (s *Service) ClearResumeForUser(ctx context.Context, access UserAccess, itemID int64) (UserData, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return UserData{}, err
	}
	item, err := s.GetItemForUser(ctx, access, itemID)
	if err != nil {
		return UserData{}, err
	}
	data := item.UserData
	data.PlaybackPositionTicks = 0
	return s.store.UpdateMediaUserDataForUser(ctx, access.UserID, item.ID, data)
}

func (s *Service) containingSeriesID(ctx context.Context, access UserAccess, itemID int64) (int64, bool, error) {
	item, err := s.GetItemForUser(ctx, access, itemID)
	if err != nil {
		return 0, false, err
	}
	switch item.Kind {
	case KindSeries:
		return item.ID, true, nil
	case KindSeason:
		if item.ParentID != nil {
			return *item.ParentID, true, nil
		}
	case KindEpisode:
		if item.ParentID == nil {
			return 0, false, nil
		}
		season, getErr := s.GetItemForUser(ctx, access, *item.ParentID)
		if getErr != nil {
			return 0, false, getErr
		}
		if season.Kind == KindSeason && season.ParentID != nil {
			return *season.ParentID, true, nil
		}
	}
	return 0, false, nil
}

func (s *Service) SetHideFromResumeForUser(ctx context.Context, access UserAccess, itemID int64, hide bool) (UserData, error) {
	item, err := s.GetItemForUser(ctx, access, itemID)
	if err != nil {
		return UserData{}, err
	}
	data := item.UserData
	data.HideFromResume = hide
	return s.UpdateUserDataForUser(ctx, access, itemID, data)
}
