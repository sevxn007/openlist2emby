package medialibrary

import (
	"context"
	"github.com/sevxn007/openlist2emby/backend/internal/client/tvdb"
	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/metadata"
	"strings"
)

func NewMetadataRegistry(tmdb MetadataProvider) *metadata.Registry {
	registry := metadata.NewRegistry()
	if tmdb != nil {
		_ = registry.Register(metadata.TMDB, metadata.TMDBAdapter{Client: tmdb})
	}
	_ = registry.Register(metadata.TVDB, tvdb.NewService())
	return registry
}
func sourceIDs(item Item) map[string]int64 {
	ids := map[string]int64{}
	if item.TMDBID != nil {
		ids[metadata.TMDB] = *item.TMDBID
	}
	if item.TVDBID != nil {
		ids[metadata.TVDB] = *item.TVDBID
	}
	return ids
}
func resolvedSource(item Item) string {
	if item.ResolvedMetadataSource != "" {
		return item.ResolvedMetadataSource
	}
	if item.TMDBID != nil {
		return metadata.TMDB
	}
	return ""
}
func resolvedID(item Item) int64 { return sourceIDs(item)[resolvedSource(item)] }
func setInputIdentity(input *ItemInput, source string, id int64) {
	if source == "" {
		source = metadata.TMDB
	}
	input.ResolvedMetadataSource = source
	if source == metadata.TVDB {
		input.TVDBID = &id
	} else {
		input.TMDBID = &id
	}
}
func setMetadataIdentity(input *MetadataInput, source string, id int64) {
	if source == "" {
		source = metadata.TMDB
	}
	input.ResolvedMetadataSource = source
	if source == metadata.TVDB {
		input.TVDBID = &id
	} else {
		input.TMDBID = &id
	}
}
func (h *TaskHandler) sourceSettings(ctx context.Context, key string) (domain.Settings, error) {
	settings, err := h.store.GetSettings(ctx)
	if err != nil {
		return domain.Settings{}, err
	}
	if key != "" {
		settings.TMDBAPIKey = key
	}
	return settings, nil
}
func (h *TaskHandler) resolveMetadata(ctx context.Context, key string, item Item, kind, name string, year *int) (domain.MediaDetail, bool) {
	settings, err := h.sourceSettings(ctx, key)
	if err != nil {
		return domain.MediaDetail{}, false
	}
	detail, err := h.sourceRegistry().Resolve(ctx, settings, metadata.Selection{Mode: item.MetadataSource, Kind: kind, Name: name, Year: year, IDs: sourceIDs(item)})
	return detail, err == nil
}
func (h *TaskHandler) resolveSeason(ctx context.Context, key string, series Item, number *int) (domain.MediaDetail, bool) {
	settings, err := h.sourceSettings(ctx, key)
	if err != nil {
		return domain.MediaDetail{}, false
	}
	detail, err := h.sourceRegistry().Detail(ctx, settings, resolvedSource(series), "tv", resolvedID(series), number)
	return detail, err == nil
}

// sourceCredits bridges the existing optional credits interface while source
// identity remains explicit in the query, never encoded into credentials.
type sourceCredits struct {
	registry *metadata.Registry
	settings domain.Settings
}

func (p sourceCredits) Credits(ctx context.Context, _ string, q domain.MediaCreditsQuery, _ string) ([]domain.MediaPerson, error) {
	return p.registry.Credits(ctx, p.settings, q)
}

func (h *TaskHandler) sourceRegistry() *metadata.Registry {
	if h.sources != nil {
		return h.sources
	}
	return NewMetadataRegistry(h.metadata)
}
func (h *TaskHandler) metadataAvailable(ctx context.Context, key string) bool {
	settings, err := h.sourceSettings(ctx, key)
	if err != nil {
		return false
	}
	for _, source := range metadata.Priority(settings) {
		if h.sourceRegistry().Configured(settings, source) {
			return true
		}
	}
	return false
}
func (h *TaskHandler) priorityKey(ctx context.Context, key string) string {
	settings, err := h.sourceSettings(ctx, key)
	if err != nil {
		return ""
	}
	return strings.Join(metadata.Priority(settings), ",")
}
func (h *TaskHandler) shouldResolveMetadata(ctx context.Context, key string, item Item) bool {
	return !item.MetadataLocked && h.metadataAvailable(ctx, key) && (item.MetadataRefreshNeeded || (item.MetadataSource == "" || item.MetadataSource == metadata.Auto) && item.MetadataPriorityKey != h.priorityKey(ctx, key))
}

func (p sourceCredits) SeasonEpisodeCredits(ctx context.Context, _ string, q domain.MediaCreditsQuery, _ string) (map[int][]domain.MediaPerson, error) {
	return p.registry.SeasonEpisodeCredits(ctx, p.settings, q)
}
