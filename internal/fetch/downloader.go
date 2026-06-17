package fetch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Downloader interface {
	Download(ctx context.Context, jobID int64, onlineVideoId string) (audioPath string, cleanup func(), err error)
}

type YtDlpDownloader struct {
	logger     *slog.Logger
	tool       string
	workingDir string
}

func (d *YtDlpDownloader) Download(ctx context.Context, jobID int64, onlineVideoId string) (audioPath string, cleanup func(), err error) {
	idLen := len(onlineVideoId)
	if idLen < 3 || idLen > 30 {
		return "", nil, fmt.Errorf("download fail: bad onlineVideoId len: %v", idLen)
	}
	url := fmt.Sprintf("https://www.youtube.com/watch?v=%v", onlineVideoId)
	jobDir := filepath.Join(d.workingDir, fmt.Sprintf("job%v", jobID))
	if err = os.MkdirAll(jobDir, 0o755); err != nil {
		d.logger.Error("failed to create directory for download", "dir", jobDir, "err", err)
		return "", nil, err
	}
	cleanup = func() {
		if err := os.RemoveAll(jobDir); err != nil {
			d.logger.Error("failed to cleanup download directory", "dir", jobDir, "err", err)
		} else {
			d.logger.Info("removed download directory", "dir", jobDir)
		}
	}
	outputPattern := filepath.Join(jobDir, "%(id)s.%(ext)s")
	args := []string{
		"-f", "bestaudio/best",
		"--no-playlist",
		"--no-cache-dir",
		"--no-mtime",
		"--no-warnings",
		"--retries", "3",
		"-o", outputPattern,
		"--print", "after_move:%(filepath)s",
		url,
	}

	cmd := exec.CommandContext(ctx, d.tool, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	d.logger.Info("running yt-dlp", "job", jobID, "tool", d.tool, "args", args)
	if err = cmd.Run(); err != nil {
		// check for context cancellation
		if ctx.Err() != nil {
			return "", cleanup, ctx.Err()
		}
		d.logger.Error("yt-dlp run failed", "err", err, "stderr", stderr.String())
		return "", cleanup, fmt.Errorf("yt-dlp failed: %w", err)
	}
	result := strings.TrimSpace(stdout.String())
	if result == "" {
		return "", cleanup, errors.New("yt-dlp produced no output path")
	}
	d.logger.Info("yt-dlp finished", "job", jobID, "result", result)
	return result, cleanup, nil
}

func NewYtDlpDownloader(logger *slog.Logger, tool string, workingDir string) (*YtDlpDownloader, error) {
	if logger == nil {
		return nil, errors.New("nil logger in YtDlpDownloader creation")
	}
	if len(tool) == 0 {
		return nil, errors.New("empty tool in YtDlpDownloader creation")
	}
	if !filepath.IsAbs(workingDir) {
		return nil, fmt.Errorf("invalid workingDir in YtDlpDownloader creation: %v", workingDir)
	}
	return &YtDlpDownloader{
		logger:     logger,
		tool:       tool,
		workingDir: workingDir,
	}, nil
}
