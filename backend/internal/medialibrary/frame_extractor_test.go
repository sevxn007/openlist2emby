package medialibrary

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFFmpegFrameExtractorUsesSignedURLUserAgent(t *testing.T) {
	directory := t.TempDir()
	ffprobePath := writeFrameCommand(t, directory, "ffprobe", `#!/bin/sh
previous=""
found=""
for argument in "$@"; do
  if [ "$previous" = "-user_agent" ] && [ "$argument" = "openlist2emby/VideoScreenshot" ]; then
    found=1
  fi
  previous="$argument"
done
[ "$found" = "1" ] || exit 42
printf '{"format":{"duration":"100"}}'
`)
	ffmpegPath := writeFrameCommand(t, directory, "ffmpeg", `#!/bin/sh
previous=""
found=""
for argument in "$@"; do
  if [ "$previous" = "-user_agent" ] && [ "$argument" = "openlist2emby/VideoScreenshot" ]; then
    found=1
  fi
  previous="$argument"
done
[ "$found" = "1" ] || exit 42
printf 'frame'
`)

	extractor := &FFmpegFrameExtractor{ffmpegPath: ffmpegPath, ffprobePath: ffprobePath}
	frame, err := extractor.Capture(t.Context(), "https://video.example/signed", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(frame, []byte("frame")) {
		t.Fatalf("frame = %q", frame)
	}
}

func writeFrameCommand(t *testing.T, directory, name, content string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
