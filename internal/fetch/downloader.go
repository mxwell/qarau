package fetch

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"strings"
)

type Downloader interface {
	Download(ctx context.Context, jobId int64, onlineVideoId string) (audioPath string, err error)
}

type YtDlpDownloader struct {
	logger     *slog.Logger
	tool       string
	workingDir string
}

func (d *YtDlpDownloader) Download(ctx context.Context, jobId int64, onlineVideoId string) (audioPath string, err error) {
	idLen := len(onlineVideoId)
	if idLen < 3 || idLen > 30 {
		return "", fmt.Errorf("download fail: bad onlineVideId len: %v", idLen)
	}
	url := fmt.Sprintf("https://www.youtube.com/watch?v=%v", onlineVideoId)
	jobDir := path.Join(d.workingDir, fmt.Sprintf("job%v", jobId))
	if err = os.MkdirAll(jobDir, 0o755); err != nil {
		d.logger.Error("failed to create directory for download", "dir", jobDir, "err", err)
		return "", err
	}
	outputPattern := path.Join(jobDir, "%(id)s.%(ext)s")
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

	d.logger.Info("running yt-dlp", "job", jobId, "tool", d.tool, "args", args)
	if err = cmd.Run(); err != nil {
		d.logger.Error("yt-dlp run failed", "err", err, "stderr", stderr.String())
		return "", fmt.Errorf("yt-dlp failed: %w", err)
	}
	result := strings.TrimSpace(stdout.String())
	d.logger.Info("yt-dlp finished", "job", jobId, "result", result)
	return result, nil
}

func NewYtDlpDownloader(logger *slog.Logger, tool string, workingDir string) *YtDlpDownloader {
	return &YtDlpDownloader{
		logger:     logger,
		tool:       tool,
		workingDir: workingDir,
	}
}
