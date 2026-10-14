package tagger

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cabbagekobe/tunetag/flac"
	"github.com/cabbagekobe/tunetag/id3v2"
	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

var testJPEG = []byte{0xff, 0xd8, 0xff, 0xdb, 0x00, 0x43}

func TestEmbedMP3WritesAndReplacesCoverAndLyrics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(path, []byte{0xff, 0xfb, 0x90, 0x64, 0, 0, 0, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	tagger := New()
	for _, lyrics := range []string{"first lyrics", "replacement lyrics"} {
		if err := tagger.Embed(path, domain.AudioMetadata{Cover: testJPEG, Lyrics: lyrics}); err != nil {
			t.Fatal(err)
		}
	}

	tag, err := id3v2.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if pictures := tag.PictureFrames(); len(pictures) != 1 || string(pictures[0].Data) != string(testJPEG) {
		t.Fatalf("pictures = %#v", pictures)
	}
	var lyricsFrames []*id3v2.UnsynchronisedLyricsFrame
	for _, frame := range tag.Frames {
		if lyrics, ok := frame.(*id3v2.UnsynchronisedLyricsFrame); ok {
			lyricsFrames = append(lyricsFrames, lyrics)
		}
	}
	if len(lyricsFrames) != 1 || lyricsFrames[0].Text != "replacement lyrics" {
		t.Fatalf("lyrics = %#v", lyricsFrames)
	}
}

func TestEmbedFLACWritesPictureAndLyrics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.flac")
	fixture := append([]byte{'f', 'L', 'a', 'C', 0x80, 0, 0, 34}, make([]byte, 34)...)
	fixture = append(fixture, 0xff, 0xf8, 0x00, 0x00)
	if err := os.WriteFile(path, fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := New().Embed(path, domain.AudioMetadata{
		Cover:  testJPEG,
		Lyrics: "[00:01.00]Lyrics",
	}); err != nil {
		t.Fatal(err)
	}

	file, err := flac.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if pictures := file.Pictures(); len(pictures) != 1 || string(pictures[0].Data) != string(testJPEG) {
		t.Fatalf("pictures = %#v", pictures)
	}
	if lyrics := file.VorbisComment().First(lyricsField); lyrics != "[00:01.00]Lyrics" {
		t.Fatalf("lyrics = %q", lyrics)
	}
}

func TestEmbedRejectsUnsupportedFormat(t *testing.T) {
	err := New().Embed("video.mkv", domain.AudioMetadata{Lyrics: "lyrics"})
	if !errors.Is(err, domain.ErrUnsupportedAudio) {
		t.Fatalf("error = %v", err)
	}
}
