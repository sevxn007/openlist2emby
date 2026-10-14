package tagger

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cabbagekobe/tunetag"
	"github.com/cabbagekobe/tunetag/aac"
	"github.com/cabbagekobe/tunetag/aiff"
	"github.com/cabbagekobe/tunetag/ape"
	"github.com/cabbagekobe/tunetag/asf"
	"github.com/cabbagekobe/tunetag/flac"
	"github.com/cabbagekobe/tunetag/id3v2"
	"github.com/cabbagekobe/tunetag/mp4"
	"github.com/cabbagekobe/tunetag/ogg"
	"github.com/cabbagekobe/tunetag/wav"
	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

const (
	mp4LyricsKey = "\xa9lyr"
	apeLyricsKey = "Lyrics"
	asfLyricsKey = "WM/Lyrics"
	lyricsField  = "LYRICS"
)

var supportedExtensions = map[string]struct{}{
	".aac":  {},
	".aif":  {},
	".aifc": {},
	".aiff": {},
	".ape":  {},
	".flac": {},
	".m4a":  {},
	".mp3":  {},
	".ogg":  {},
	".opus": {},
	".wav":  {},
	".wma":  {},
	".wv":   {},
}

type Tagger struct{}

func New() *Tagger {
	return &Tagger{}
}

func (t *Tagger) Supports(filename string) bool {
	_, ok := supportedExtensions[strings.ToLower(filepath.Ext(filename))]
	return ok
}

func (t *Tagger) Embed(path string, metadata domain.AudioMetadata) error {
	if !t.Supports(path) {
		return domain.ErrUnsupportedAudio
	}
	if metadata.Title == "" && metadata.Artist == "" && metadata.Album == "" && len(metadata.Cover) == 0 && strings.TrimSpace(metadata.Lyrics) == "" {
		return errors.New("audio metadata is empty")
	}
	if len(metadata.Cover) > 0 {
		metadata.CoverMIME = tunetag.SniffImageMIME(metadata.Cover)
		if metadata.CoverMIME == "" {
			return errors.New("cover image format is not supported")
		}
	}

	var err error
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		err = embedMP3(path, metadata)
	case ".flac":
		err = embedFLAC(path, metadata)
	case ".m4a":
		err = embedMP4(path, metadata)
	case ".wav":
		err = embedWAV(path, metadata)
	case ".aif", ".aifc", ".aiff":
		err = embedAIFF(path, metadata)
	case ".aac":
		err = embedAAC(path, metadata)
	case ".ogg", ".opus":
		err = embedOgg(path, metadata)
	case ".ape", ".wv":
		err = embedAPE(path, metadata)
	case ".wma":
		err = embedASF(path, metadata)
	default:
		return domain.ErrUnsupportedAudio
	}
	if errors.Is(err, mp4.ErrFragmentedUnsupport) {
		return fmt.Errorf("%w: %v", domain.ErrFragmentedAudio, err)
	}
	if err != nil {
		return fmt.Errorf("write %s metadata: %w", strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."), err)
	}
	return nil
}

func applyID3Metadata(tag *id3v2.Tag, metadata domain.AudioMetadata) {
	if metadata.Title != "" {
		tag.RemoveFrames("TIT2")
		tag.AddFrame(&id3v2.TextFrame{FrameID: "TIT2", Encoding: id3v2.EncUTF8, Text: []string{metadata.Title}})
	}
	if metadata.Artist != "" {
		tag.RemoveFrames("TPE1")
		tag.AddFrame(&id3v2.TextFrame{FrameID: "TPE1", Encoding: id3v2.EncUTF8, Text: []string{metadata.Artist}})
	}
	if metadata.Album != "" {
		tag.RemoveFrames("TALB")
		tag.AddFrame(&id3v2.TextFrame{FrameID: "TALB", Encoding: id3v2.EncUTF8, Text: []string{metadata.Album}})
	}
	if len(metadata.Cover) > 0 {
		tag.RemoveFrames("APIC")
		tag.AddFrame(&id3v2.PictureFrame{
			Encoding:    id3v2.EncUTF8,
			MIME:        metadata.CoverMIME,
			PictureType: uint8(tunetag.PictureCoverFront),
			Data:        metadata.Cover,
		})
	}
	if metadata.Lyrics != "" {
		tag.RemoveFrames("USLT")
		tag.AddFrame(&id3v2.UnsynchronisedLyricsFrame{
			Encoding: id3v2.EncUTF8,
			Language: "und",
			Text:     metadata.Lyrics,
		})
	}
}

func embedMP3(path string, metadata domain.AudioMetadata) error {
	tag, err := id3v2.ReadFile(path)
	if errors.Is(err, id3v2.ErrNoTag) {
		tag = &id3v2.Tag{Version: id3v2.V24, Padding: id3v2.DefaultPadding}
	} else if err != nil {
		return err
	}
	applyID3Metadata(tag, metadata)
	return tag.WriteFile(path)
}

func embedFLAC(path string, metadata domain.AudioMetadata) error {
	file, err := flac.ReadFile(path)
	if err != nil {
		return err
	}
	if len(metadata.Cover) > 0 {
		file.RemovePictures()
		file.AddPicture(&flac.Picture{
			PictureType: uint32(tunetag.PictureCoverFront),
			MIME:        metadata.CoverMIME,
			Data:        metadata.Cover,
		})
	}
	if metadata.Title != "" {
		file.VorbisComment().Set("TITLE", metadata.Title)
	}
	if metadata.Artist != "" {
		file.VorbisComment().Set("ARTIST", metadata.Artist)
	}
	if metadata.Album != "" {
		file.VorbisComment().Set("ALBUM", metadata.Album)
	}
	if metadata.Lyrics != "" {
		file.VorbisComment().Set(lyricsField, metadata.Lyrics)
	}
	return file.WriteFile(path)
}

func embedMP4(path string, metadata domain.AudioMetadata) error {
	file, err := mp4.Read(path)
	if err != nil {
		return err
	}
	if len(metadata.Cover) > 0 {
		if metadata.CoverMIME != "image/jpeg" && metadata.CoverMIME != "image/png" {
			return errors.New("M4A cover must be JPEG or PNG")
		}
		file.Tag.Remove(mp4.KeyCover)
		file.Tag.AddCover(metadata.Cover)
	}
	if metadata.Title != "" {
		file.Tag.SetTitle(metadata.Title)
	}
	if metadata.Artist != "" {
		file.Tag.SetArtist(metadata.Artist)
	}
	if metadata.Album != "" {
		file.Tag.SetAlbum(metadata.Album)
	}
	if metadata.Lyrics != "" {
		file.Tag.SetText(mp4LyricsKey, metadata.Lyrics)
	}
	return file.WriteFile(path)
}

func embedWAV(path string, metadata domain.AudioMetadata) error {
	file, err := wav.ReadFile(path)
	if err != nil {
		return err
	}
	if file.ID3 == nil {
		file.ID3 = &id3v2.Tag{Version: id3v2.V24}
	}
	applyID3Metadata(file.ID3, metadata)
	return file.WriteFile(path)
}

func embedAIFF(path string, metadata domain.AudioMetadata) error {
	file, err := aiff.ReadFile(path)
	if err != nil {
		return err
	}
	if file.ID3 == nil {
		file.ID3 = &id3v2.Tag{Version: id3v2.V24}
	}
	applyID3Metadata(file.ID3, metadata)
	return file.WriteFile(path)
}

func embedAAC(path string, metadata domain.AudioMetadata) error {
	file, err := aac.ReadFile(path)
	if err != nil {
		return err
	}
	if file.V2 == nil {
		file.V2 = &id3v2.Tag{Version: id3v2.V24}
	}
	applyID3Metadata(file.V2, metadata)
	return file.WriteFile(path)
}

func embedOgg(path string, metadata domain.AudioMetadata) error {
	file, err := ogg.ReadFile(path)
	if err != nil {
		return err
	}
	if file.Comments == nil {
		return errors.New("Ogg file has no writable comment block")
	}
	if len(metadata.Cover) > 0 {
		file.RemovePictures()
		if err := file.AddPicture(&flac.Picture{
			PictureType: uint32(tunetag.PictureCoverFront),
			MIME:        metadata.CoverMIME,
			Data:        metadata.Cover,
		}); err != nil {
			return err
		}
	}
	if metadata.Lyrics != "" {
		file.Comments.Set(lyricsField, metadata.Lyrics)
	}
	return file.WriteFile(path)
}

func embedAPE(path string, metadata domain.AudioMetadata) error {
	tag, err := ape.ReadFile(path)
	if errors.Is(err, ape.ErrNoTag) {
		tag = &ape.Tag{HasHeader: true}
	} else if err != nil {
		return err
	}
	if len(metadata.Cover) > 0 {
		tag.RemovePictures()
		if err := tag.AddPicture(&ape.Picture{
			Filename: coverFilename(metadata.CoverMIME),
			Data:     metadata.Cover,
		}); err != nil {
			return err
		}
	}
	if metadata.Lyrics != "" {
		if err := tag.Set(apeLyricsKey, metadata.Lyrics); err != nil {
			return err
		}
	}
	return tag.WriteFile(path)
}

func embedASF(path string, metadata domain.AudioMetadata) error {
	file, err := asf.ReadFile(path)
	if err != nil {
		return err
	}
	if len(metadata.Cover) > 0 {
		file.RemovePictures()
		file.AddPicture(&asf.Picture{
			Type: uint8(tunetag.PictureCoverFront),
			MIME: metadata.CoverMIME,
			Data: metadata.Cover,
		})
	}
	if metadata.Lyrics != "" {
		file.SetExt(asfLyricsKey, metadata.Lyrics)
	}
	return file.WriteFile(path)
}

func coverFilename(mime string) string {
	if mime == "image/png" {
		return "cover.png"
	}
	return "cover.jpg"
}
