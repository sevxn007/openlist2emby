package sqlite

import (
	"context"
	"fmt"

	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
	"gorm.io/gorm"
)

type continuationCandidateRow struct {
	ItemID int64
	Source string
	Total  int64
}

func (s *Store) ListMediaNextUpForUser(ctx context.Context, userID int64, query medialibrary.NextUpQuery) (medialibrary.ItemPage, error) {
	if query.AllowedLibraryIDs != nil && len(query.AllowedLibraryIDs) == 0 {
		return medialibrary.ItemPage{Items: []medialibrary.Item{}}, nil
	}
	cte, args := nextUpCTE(userID, query)
	limit, start := continuationPagination(query.Start, query.Limit)
	pageArgs := append(append([]any{}, args...), limit, start)
	var rows []continuationCandidateRow
	if err := s.db.WithContext(ctx).Raw(cte+`
		SELECT item_id, 'next_up' AS source, COUNT(*) OVER() AS total
		FROM next_up_candidates
		ORDER BY activity_at DESC, series_id, candidate_kind, item_id
		LIMIT ? OFFSET ?`, pageArgs...).Scan(&rows).Error; err != nil {
		return medialibrary.ItemPage{}, err
	}
	total, err := continuationPageTotal(ctx, s.db, cte+` SELECT COUNT(*) FROM next_up_candidates`, args, start, rows)
	if err != nil {
		return medialibrary.ItemPage{}, err
	}
	items, err := s.continuationItems(ctx, userID, rows)
	if err != nil {
		return medialibrary.ItemPage{}, err
	}
	result := make([]medialibrary.Item, len(items))
	for index := range items {
		result[index] = items[index].Item
	}
	return medialibrary.ItemPage{Items: result, Total: int(total)}, nil
}

func (s *Store) ListMediaContinueWatchingForUser(ctx context.Context, userID int64, query medialibrary.NextUpQuery) (medialibrary.ContinueWatchingPage, error) {
	if query.AllowedLibraryIDs != nil && len(query.AllowedLibraryIDs) == 0 {
		return medialibrary.ContinueWatchingPage{Items: []medialibrary.ContinueWatchingItem{}}, nil
	}
	cte, args := nextUpCTE(userID, query)
	resumeKinds := continuationResumeKinds(query.Kinds)
	resumeScope := ""
	resumeArgs := []any{userID, resumeKinds}
	if query.AllowedLibraryIDs != nil {
		resumeScope = " AND item.library_id IN ?"
		resumeArgs = append(resumeArgs, query.AllowedLibraryIDs)
	}
	if query.LibraryID != nil {
		resumeScope += " AND item.library_id = ?"
		resumeArgs = append(resumeArgs, *query.LibraryID)
	}
	if query.SeriesID != nil {
		resumeScope += " AND item.kind = ? AND season.parent_id = ?"
		resumeArgs = append(resumeArgs, string(medialibrary.KindEpisode), *query.SeriesID)
	}
	combined := cte + fmt.Sprintf(`,
	resume_candidates AS (
		SELECT item.id AS item_id,
			CASE WHEN item.kind = '%s' THEN season.parent_id ELSE NULL END AS series_id,
			user_data.last_played_at AS activity_at,
			0 AS candidate_kind,
			'resume' AS source
		FROM media_items item
		JOIN media_user_data user_data ON user_data.item_id = item.id AND user_data.user_id = ?
		LEFT JOIN media_items season ON season.id = item.parent_id AND item.kind = '%s'
		WHERE item.kind IN ?
			AND user_data.playback_position_ticks > 0
			AND user_data.hide_from_resume = 0
			AND user_data.played = 0%s
	),
	continue_candidates AS (
		SELECT item_id, series_id, activity_at, candidate_kind, source FROM resume_candidates
		UNION ALL
		SELECT next_up.item_id, next_up.series_id, next_up.activity_at, next_up.candidate_kind, 'next_up'
		FROM next_up_candidates next_up
		WHERE NOT EXISTS (
			SELECT 1 FROM resume_candidates resume
			WHERE resume.series_id = next_up.series_id
		)
	)`, medialibrary.KindEpisode, medialibrary.KindEpisode, resumeScope)
	args = append(args, resumeArgs...)
	limit, start := continuationPagination(query.Start, query.Limit)
	pageArgs := append(append([]any{}, args...), limit, start)
	var rows []continuationCandidateRow
	if err := s.db.WithContext(ctx).Raw(combined+`
		SELECT item_id, source, COUNT(*) OVER() AS total
		FROM continue_candidates
		ORDER BY activity_at DESC, candidate_kind, item_id DESC
		LIMIT ? OFFSET ?`, pageArgs...).Scan(&rows).Error; err != nil {
		return medialibrary.ContinueWatchingPage{}, err
	}
	total, err := continuationPageTotal(ctx, s.db, combined+` SELECT COUNT(*) FROM continue_candidates`, args, start, rows)
	if err != nil {
		return medialibrary.ContinueWatchingPage{}, err
	}
	items, err := s.continuationItems(ctx, userID, rows)
	if err != nil {
		return medialibrary.ContinueWatchingPage{}, err
	}
	return medialibrary.ContinueWatchingPage{Items: items, Total: int(total)}, nil
}

func nextUpCTE(userID int64, query medialibrary.NextUpQuery) (string, []any) {
	scope := ""
	args := []any{userID}
	if !continuationKindIncluded(query.Kinds, medialibrary.KindEpisode) {
		scope += " AND 1 = 0"
	}
	if query.AllowedLibraryIDs != nil {
		scope += " AND episode.library_id IN ?"
		args = append(args, query.AllowedLibraryIDs)
	}
	if query.LibraryID != nil {
		scope += " AND episode.library_id = ?"
		args = append(args, *query.LibraryID)
	}
	if query.SeriesID != nil {
		scope += " AND series.id = ?"
		args = append(args, *query.SeriesID)
	}
	cutoff := ""
	if query.DateCutoff != nil {
		cutoff = " AND history.activity_at >= ?"
		args = append(args, newSQLiteTimestamp(query.DateCutoff.UTC()))
	}
	resumable := " AND playback_position_ticks = 0"
	if query.EnableResumable {
		resumable = ""
	}
	rewatchCTEs, rewatchUnion := nextUpRewatchSQL(query.EnableRewatching)
	statement := fmt.Sprintf(`WITH
	episode_state AS (
		SELECT episode.id AS item_id,
			episode.library_id,
			series.id AS series_id,
			COALESCE(episode.season_number, season.season_number) AS season_number,
			episode.episode_number,
			COALESCE(user_data.hide_from_resume, 0) AS hide_from_resume,
			COALESCE(user_data.played, 0) AS played,
			COALESCE(user_data.play_count, 0) AS play_count,
			COALESCE(user_data.playback_position_ticks, 0) AS playback_position_ticks,
			user_data.last_played_at
		FROM media_items episode
		JOIN media_items season ON season.id = episode.parent_id AND season.kind = '%s'
		JOIN media_items series ON series.id = season.parent_id AND series.kind = '%s'
		LEFT JOIN media_user_data user_data ON user_data.item_id = episode.id AND user_data.user_id = ?
		WHERE episode.kind = '%s'
			AND COALESCE(episode.season_number, season.season_number, 0) > 0
			AND episode.episode_number IS NOT NULL%s
	),
	series_history AS (
		SELECT series_id, MAX(last_played_at) AS activity_at
		FROM episode_state
		GROUP BY series_id
		HAVING MAX(CASE WHEN played = 1 OR play_count > 0 OR playback_position_ticks > 0 OR last_played_at IS NOT NULL THEN 1 ELSE 0 END) = 1
	),
	played_ranked AS (
		SELECT series_id, season_number, episode_number, item_id,
			ROW_NUMBER() OVER (
				PARTITION BY series_id
				ORDER BY season_number DESC, episode_number DESC, item_id DESC
			) AS anchor_rank
		FROM episode_state
		WHERE played = 1
	),
	played_anchor AS (
		SELECT series_id, season_number, episode_number, item_id
		FROM played_ranked
		WHERE anchor_rank = 1
	),
	unplayed_ranked AS (
		SELECT episode_state.item_id, episode_state.series_id, history.activity_at,
			episode_state.hide_from_resume, episode_state.playback_position_ticks,
			ROW_NUMBER() OVER (PARTITION BY episode_state.series_id ORDER BY episode_state.season_number, episode_state.episode_number, episode_state.item_id) AS candidate_rank
		FROM episode_state
		JOIN series_history history ON history.series_id = episode_state.series_id%s
		LEFT JOIN played_anchor anchor ON anchor.series_id = episode_state.series_id
		WHERE episode_state.played = 0 AND (
			anchor.item_id IS NULL OR
			episode_state.season_number > anchor.season_number OR
			(episode_state.season_number = anchor.season_number AND episode_state.episode_number > anchor.episode_number) OR
			(episode_state.season_number = anchor.season_number AND episode_state.episode_number = anchor.episode_number AND episode_state.item_id > anchor.item_id)
		)
	)%s,
	next_up_candidates AS (
		SELECT item_id, series_id, activity_at, 0 AS candidate_kind
		FROM unplayed_ranked WHERE candidate_rank = 1 AND hide_from_resume = 0%s%s
	)`, medialibrary.KindSeason, medialibrary.KindSeries, medialibrary.KindEpisode, scope, cutoff, rewatchCTEs, resumable, rewatchUnion)
	return statement, args
}

func nextUpRewatchSQL(enabled bool) (string, string) {
	if !enabled {
		return "", ""
	}
	ctes := `,
	rewatch_anchor_ranked AS (
		SELECT episode_state.*,
			ROW_NUMBER() OVER (
				PARTITION BY series_id
				ORDER BY last_played_at DESC, item_id DESC
			) AS anchor_rank
		FROM episode_state
		WHERE played = 1 AND last_played_at IS NOT NULL
	),
	rewatch_anchor AS (
		SELECT * FROM rewatch_anchor_ranked WHERE anchor_rank = 1
	),
	rewatch_ranked AS (
		SELECT candidate.item_id, candidate.series_id, anchor.last_played_at AS activity_at,
			candidate.hide_from_resume, candidate.playback_position_ticks,
			ROW_NUMBER() OVER (PARTITION BY candidate.series_id ORDER BY candidate.season_number, candidate.episode_number, candidate.item_id) AS candidate_rank
		FROM episode_state candidate
		JOIN rewatch_anchor anchor ON anchor.series_id = candidate.series_id
		WHERE candidate.played = 1 AND (
			candidate.season_number > anchor.season_number OR
			(candidate.season_number = anchor.season_number AND candidate.episode_number > anchor.episode_number) OR
			(candidate.season_number = anchor.season_number AND candidate.episode_number = anchor.episode_number AND candidate.item_id > anchor.item_id)
		)
	)`
	union := `
		UNION ALL
		SELECT item_id, series_id, activity_at, 1 AS candidate_kind
		FROM rewatch_ranked WHERE candidate_rank = 1 AND hide_from_resume = 0 AND playback_position_ticks = 0`
	return ctes, union
}

func continuationPageTotal(ctx context.Context, db *gorm.DB, countSQL string, args []any, start int, rows []continuationCandidateRow) (int64, error) {
	if len(rows) > 0 {
		return rows[0].Total, nil
	}
	if start == 0 {
		return 0, nil
	}
	var total int64
	if err := db.WithContext(ctx).Raw(countSQL, args...).Scan(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func continuationResumeKinds(kinds []medialibrary.ItemKind) []string {
	result := make([]string, 0, 2)
	for _, kind := range []medialibrary.ItemKind{medialibrary.KindMovie, medialibrary.KindEpisode} {
		if continuationKindIncluded(kinds, kind) {
			result = append(result, string(kind))
		}
	}
	return result
}

func continuationKindIncluded(kinds []medialibrary.ItemKind, kind medialibrary.ItemKind) bool {
	if len(kinds) == 0 {
		return true
	}
	for _, candidate := range kinds {
		if candidate == kind {
			return true
		}
	}
	return false
}

func continuationPagination(start, limit int) (int, int) {
	if start < 0 {
		start = 0
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return limit, start
}

func (s *Store) continuationItems(ctx context.Context, userID int64, rows []continuationCandidateRow) ([]medialibrary.ContinueWatchingItem, error) {
	if len(rows) == 0 {
		return []medialibrary.ContinueWatchingItem{}, nil
	}
	ids := make([]int64, len(rows))
	for index := range rows {
		ids[index] = rows[index].ItemID
	}
	var records []mediaItemModel
	if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&records).Error; err != nil {
		return nil, err
	}
	userData, err := mediaUserDataByItem(ctx, s.db, userID, records)
	if err != nil {
		return nil, err
	}
	bindings, err := mediaMetadataBindingsByItem(ctx, s.db, records)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]medialibrary.Item, len(records))
	for _, record := range records {
		item, mapErr := mediaItemFromModel(record, userData[record.ID])
		if mapErr != nil {
			return nil, mapErr
		}
		item.MetadataBinding = bindings[record.ID]
		byID[record.ID] = item
	}
	items := make([]medialibrary.ContinueWatchingItem, 0, len(rows))
	for _, row := range rows {
		item, ok := byID[row.ItemID]
		if !ok {
			continue
		}
		items = append(items, medialibrary.ContinueWatchingItem{
			Item: item, Source: medialibrary.ContinueWatchingSource(row.Source),
		})
	}
	return items, nil
}
