package asr

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
)

type Transcoder interface {
	Transcode(ctx context.Context, audioFile string) (io.ReadCloser, error)
}

type ffmpegTranscoder struct {
	logger *slog.Logger
	tool   string
}

func NewFfmpegTranscoder(logger *slog.Logger, tool string) (Transcoder, error) {
	if logger == nil {
		return nil, errors.New("nil logger in ffmpegTranscoder creation")
	}
	if !strings.HasPrefix(tool, "/") {
		return nil, fmt.Errorf("tool path must start with /: %v", tool)
	}
	return &ffmpegTranscoder{
		logger: logger,
		tool:   tool,
	}, nil
}

type ffmpegInvocation struct {
	cmd    *exec.Cmd
	stderr *bytes.Buffer
	pipe   io.ReadCloser
	closed bool
}

func (i *ffmpegInvocation) Read(buf []byte) (int, error) {
	return i.pipe.Read(buf)
}

func (i *ffmpegInvocation) Close() error {
	if i.closed {
		return nil
	}
	i.closed = true
	// Draining the stdout pipe
	_, _ = io.Copy(io.Discard, i.pipe)

	if err := i.cmd.Wait(); err != nil {
		return fmt.Errorf("ffmpeg fail with stderr=[%v]: %w", i.stderr.String(), err)
	}
	return nil
}

func (t *ffmpegTranscoder) Transcode(ctx context.Context, audioFile string) (io.ReadCloser, error) {
	args := []string{
		"-i", audioFile,
		"-ar", "16000", // 16kHz sample rate
		"-ac", "1", // mono
		"-f", "s16le", // 16bit
		"-loglevel", "error",
		"-hide_banner",
		"-", // write PCM to stdout
	}
	cmd := exec.CommandContext(ctx, t.tool, args...)

	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create pipe for ffmpeg stdout: %w", err)
	}

	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		t.logger.Error("ffmpeg run failed", "err", err, "stderr", stderr.String())
		return nil, fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	return &ffmpegInvocation{
		cmd:    cmd,
		stderr: stderr,
		pipe:   pipe,
		closed: false,
	}, nil
}
