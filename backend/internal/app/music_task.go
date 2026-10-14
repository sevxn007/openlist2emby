package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/sevxn007/openlist2emby/backend/internal/task"
)

const MusicEmbedTaskType = "music.embed_metadata"

type MusicEmbedTaskHandler struct {
	service  *Service
	resolver MusicMediaItemResolver
}

type musicEmbedTaskPayload struct {
	ItemID          int64  `json:"item_id"`
	Path            string `json:"path"`
	Source          string `json:"source"`
	PictureID       string `json:"picture_id"`
	LyricID         string `json:"lyric_id"`
	EmbedCover      bool   `json:"embed_cover"`
	EmbedLyrics     bool   `json:"embed_lyrics"`
	OpenListBaseURL string `json:"openlist_base_url"`
}

type MusicEmbedTaskSummary struct {
	ItemID      int64  `json:"item_id"`
	Path        string `json:"path"`
	EmbedCover  bool   `json:"embed_cover"`
	EmbedLyrics bool   `json:"embed_lyrics"`
}

func NewMusicEmbedTaskHandler(service *Service, resolvers ...MusicMediaItemResolver) *MusicEmbedTaskHandler {
	var resolver MusicMediaItemResolver
	if len(resolvers) > 0 {
		resolver = resolvers[0]
	}
	return &MusicEmbedTaskHandler{service: service, resolver: resolver}
}

func (h *MusicEmbedTaskHandler) Descriptor() task.Descriptor {
	return task.Descriptor{
		Type:           MusicEmbedTaskType,
		PayloadVersion: 1,
		Manual:         true,
		Schedulable:    false,
		Cancelable:     true,
	}
}

func (h *MusicEmbedTaskHandler) Prepare(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	if h == nil || h.service == nil {
		return nil, task.ErrTypeUnavailable
	}
	var input MusicEmbedInput
	if err := decodeStrictJSON(raw, &input); err != nil {
		return nil, badRequest("invalid music embed payload")
	}
	if h.resolver != nil {
		if input.ItemID <= 0 {
			return nil, badRequest("item_id must identify a music library audio item")
		}
		mediaPath, err := h.resolver.ResolveMusicMediaPath(ctx, input.ItemID)
		if err != nil {
			return nil, unprocessable("music library audio item is unavailable")
		}
		input.Path = mediaPath
	}
	normalized, err := h.service.normalizeMusicEmbedInput(input)
	if err != nil {
		return nil, err
	}
	settings, err := h.service.store.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(musicEmbedTaskPayload{
		ItemID:          normalized.ItemID,
		Path:            normalized.Path,
		Source:          normalized.Source,
		PictureID:       normalized.PictureID,
		LyricID:         normalized.LyricID,
		EmbedCover:      normalized.EmbedCover,
		EmbedLyrics:     normalized.EmbedLyrics,
		OpenListBaseURL: settings.OpenListBaseURL,
	})
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func (h *MusicEmbedTaskHandler) Run(
	ctx context.Context,
	raw json.RawMessage,
	execution task.Execution,
) (json.RawMessage, error) {
	if h == nil || h.service == nil {
		return nil, task.ErrTypeUnavailable
	}
	var payload musicEmbedTaskPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, task.NewPublicError("invalid_payload", "stored music task payload is invalid", http.StatusUnprocessableEntity, false, err)
	}
	settings, err := h.service.store.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	if settings.OpenListBaseURL != payload.OpenListBaseURL {
		return nil, task.NewPublicError(
			"openlist_config_changed",
			"OpenList configuration changed after the task was created",
			http.StatusConflict,
			false,
			nil,
		)
	}
	if strings.TrimSpace(settings.OpenListAdminKey) == "" {
		return nil, task.NewPublicError(
			"openlist_admin_key_missing",
			"OpenList administrator key is not configured",
			http.StatusPreconditionFailed,
			false,
			nil,
		)
	}
	result, err := h.service.embedMusicWithAccess(
		ctx,
		settings.OpenListBaseURL,
		settings.OpenListAdminKey,
		MusicEmbedInput{
			ItemID:      payload.ItemID,
			Path:        payload.Path,
			Source:      payload.Source,
			PictureID:   payload.PictureID,
			LyricID:     payload.LyricID,
			EmbedCover:  payload.EmbedCover,
			EmbedLyrics: payload.EmbedLyrics,
		},
		func(progress MusicEmbedProgress) {
			execution.Report(task.Progress{Stage: string(progress.Stage), Percent: progress.Percent})
		},
	)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

func (h *MusicEmbedTaskHandler) PublicSummary(raw json.RawMessage) any {
	var payload musicEmbedTaskPayload
	if json.Unmarshal(raw, &payload) != nil {
		return nil
	}
	return MusicEmbedTaskSummary{
		ItemID:      payload.ItemID,
		Path:        payload.Path,
		EmbedCover:  payload.EmbedCover,
		EmbedLyrics: payload.EmbedLyrics,
	}
}

func decodeStrictJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values")
	}
	return nil
}
