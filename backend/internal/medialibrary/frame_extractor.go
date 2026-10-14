package medialibrary

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	videoFrameTimeout = 2 * time.Minute
	maxProbeOutput    = 1 << 20
	maxCommandStderr  = 64 << 10
)

var errVideoFrameOutputTooLarge = errors.New("video frame output is too large")

// VideoFrameExtractor isolates the external FFmpeg process for deterministic
// media scan and HTTP handler tests.
type VideoFrameExtractor interface {
	Available() error
	Capture(context.Context, string, int) ([]byte, error)
}

type FFmpegFrameExtractor struct {
	ffmpegPath  string
	ffprobePath string
}

func NewFFmpegFrameExtractor() *FFmpegFrameExtractor {
	ffmpegPath, _ := exec.LookPath("ffmpeg")
	ffprobePath, _ := exec.LookPath("ffprobe")
	return &FFmpegFrameExtractor{ffmpegPath: ffmpegPath, ffprobePath: ffprobePath}
}

func (e *FFmpegFrameExtractor) Available() error {
	if e == nil || e.ffmpegPath == "" || e.ffprobePath == "" {
		return ErrVideoScreenshotUnavailable
	}
	return nil
}

func (e *FFmpegFrameExtractor) Capture(ctx context.Context, sourceURL string, percent int) ([]byte, error) {
	if err := e.Available(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(sourceURL) == "" || percent < 1 || percent > 99 {
		return nil, fmt.Errorf("%w: invalid capture request", ErrInvalidLibrary)
	}

	commandContext, cancel := context.WithTimeout(ctx, videoFrameTimeout)
	defer cancel()
	duration, err := e.probeDuration(commandContext, sourceURL)
	if err != nil {
		return nil, err
	}
	timestamp := duration * float64(percent) / 100
	stdout := newCappedBuffer(MaxUploadedImageBytes)
	stderr := newCappedBuffer(maxCommandStderr)
	command := exec.CommandContext(commandContext, e.ffmpegPath,
		"-v", "error",
		"-user_agent", videoScreenshotUserAgent,
		"-ss", strconv.FormatFloat(timestamp, 'f', 3, 64),
		"-i", sourceURL,
		"-frames:v", "1",
		"-vf", `scale=min(1280\,iw):-2`,
		"-f", "image2pipe",
		"-vcodec", "mjpeg",
		"pipe:1",
	)
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return nil, videoFrameCommandError(commandContext, stdout, err)
	}
	if stdout.Len() == 0 {
		return nil, ErrVideoScreenshotFailed
	}
	return bytes.Clone(stdout.Bytes()), nil
}

func (e *FFmpegFrameExtractor) probeDuration(ctx context.Context, sourceURL string) (float64, error) {
	stdout := newCappedBuffer(maxProbeOutput)
	stderr := newCappedBuffer(maxCommandStderr)
	command := exec.CommandContext(ctx, e.ffprobePath,
		"-v", "error",
		"-user_agent", videoScreenshotUserAgent,
		"-show_entries", "format=duration",
		"-of", "json",
		sourceURL,
	)
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return 0, videoFrameCommandError(ctx, stdout, err)
	}
	var response struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		return 0, ErrVideoScreenshotFailed
	}
	duration, err := strconv.ParseFloat(strings.TrimSpace(response.Format.Duration), 64)
	if err != nil || duration <= 0 {
		return 0, ErrVideoScreenshotFailed
	}
	return duration, nil
}

func videoFrameCommandError(ctx context.Context, output *cappedBuffer, _ error) error {
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ctx.Err()
	}
	if output.overflow {
		return fmt.Errorf("%w: %v", ErrVideoScreenshotFailed, errVideoFrameOutputTooLarge)
	}
	// FFmpeg output can repeat a signed source URL. Keep the public error stable
	// and do not surface command output or arguments.
	return ErrVideoScreenshotFailed
}

type cappedBuffer struct {
	bytes.Buffer
	max      int
	overflow bool
}

func newCappedBuffer(max int) *cappedBuffer {
	return &cappedBuffer{max: max}
}

func (b *cappedBuffer) Write(value []byte) (int, error) {
	remaining := b.max - b.Len()
	if remaining <= 0 {
		b.overflow = true
		return 0, errVideoFrameOutputTooLarge
	}
	if len(value) > remaining {
		written, _ := b.Buffer.Write(value[:remaining])
		b.overflow = true
		return written, errVideoFrameOutputTooLarge
	}
	return b.Buffer.Write(value)
}
