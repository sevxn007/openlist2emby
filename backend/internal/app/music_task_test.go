package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

type fakeMusicMediaItemResolver struct {
	itemID int64
	path   string
}

func (r *fakeMusicMediaItemResolver) ResolveMusicMediaPath(_ context.Context, itemID int64) (string, error) {
	r.itemID = itemID
	return r.path, nil
}

func TestMusicEmbedTaskResolvesMediaLibraryItemPath(t *testing.T) {
	resolver := &fakeMusicMediaItemResolver{path: "/Music/Artist/Song.flac"}
	service := NewService(&settingsFailureStore{}, nil, nil, nil, nil)
	service.audio = fakeAudioSupport{supported: true}
	handler := NewMusicEmbedTaskHandler(service, resolver)

	payload, err := handler.Prepare(t.Context(), json.RawMessage(`{
		"item_id":42,
		"source":"netease",
		"picture_id":"picture",
		"lyric_id":"lyric",
		"embed_cover":true,
		"embed_lyrics":true
	}`))
	if err != nil {
		t.Fatal(err)
	}
	var stored musicEmbedTaskPayload
	if err := json.Unmarshal(payload, &stored); err != nil {
		t.Fatal(err)
	}
	if resolver.itemID != 42 || stored.ItemID != 42 || stored.Path != resolver.path {
		t.Fatalf("resolver item = %d payload = %#v", resolver.itemID, stored)
	}
}

func TestMusicEmbedTaskRejectsPathWithoutMediaLibraryItem(t *testing.T) {
	service := NewService(&settingsFailureStore{}, nil, nil, nil, nil)
	handler := NewMusicEmbedTaskHandler(service, &fakeMusicMediaItemResolver{})
	_, err := handler.Prepare(t.Context(), json.RawMessage(`{
		"path":"/Music/Artist/Song.flac",
		"source":"netease",
		"picture_id":"picture",
		"embed_cover":true
	}`))
	if err == nil {
		t.Fatal("expected item_id validation error")
	}
}

type fakeAudioSupport struct {
	supported bool
}

func (f fakeAudioSupport) Supports(string) bool { return f.supported }

func (fakeAudioSupport) Embed(string, domain.AudioMetadata) error { return nil }
