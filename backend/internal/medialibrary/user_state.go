package medialibrary

import (
	"context"
	"time"
)

// SetPlayedForUser marks an accessible item played or unplayed without
// replacing unrelated per-user state such as favorites.
func (s *Service) SetPlayedForUser(ctx context.Context, access UserAccess, itemID int64, played bool) (UserData, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return UserData{}, err
	}
	item, err := s.GetItemForUser(ctx, access, itemID)
	if err != nil {
		return UserData{}, err
	}
	now := time.Now().UTC()
	if store, ok := s.store.(PlayedStateStore); ok {
		return store.SetMediaPlayedStateForUser(ctx, access.UserID, item.ID, played, now)
	}

	data := item.UserData
	data.PlaybackPositionTicks = 0
	if played {
		if !data.Played {
			data.PlayCount++
		}
		data.Played = true
		data.LastPlayedAt = &now
	} else {
		data.Played = false
	}
	return s.store.UpdateMediaUserDataForUser(ctx, access.UserID, item.ID, data)
}
