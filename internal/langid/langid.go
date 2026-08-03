package langid

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mxwell/qarau/internal/audio"
	ort "github.com/yalue/onnxruntime_go"
)

const (
	LangIdSampleMinLength = 10 * audio.SampleRateHz // 10 seconds in frames: report error if not possible to collect any samples with at least 10s
	LangIdSampleMaxLength = 30 * audio.SampleRateHz // 30 seconds in frames

	LangIdMinProbability = 0.5
)

type Classifier struct {
	logger  *slog.Logger
	session *ort.DynamicAdvancedSession
	lock    sync.Mutex
	labels  []string
	kkIndex int
}

// Returns -1 if the number of matches is different from 1,
// i.e. no matches or multiple matches => -1.
// If the match is unique, then returns the label index.
func indexWithPrefix(labels []string, prefix string) int {
	result := -1
	for i, label := range labels {
		if strings.HasPrefix(label, prefix) {
			if result != -1 {
				return -1
			}
			result = i
		}
	}
	return result
}

func top3(values []float32) ([]int, error) {
	if len(values) < 3 {
		return nil, fmt.Errorf("too few elements: %d", len(values))
	}
	indices := make([]int, 0, len(values))
	for i := range values {
		indices = append(indices, i)
	}
	sort.Slice(indices, func(x, y int) bool {
		return values[indices[x]] > values[indices[y]]
	})
	return indices[:3], nil
}

func New(logger *slog.Logger, modelDirPath string, sharedLibPath string) (*Classifier, error) {
	ort.SetSharedLibraryPath(sharedLibPath)
	if !ort.IsInitialized() {
		if err := ort.InitializeEnvironment(); err != nil {
			return nil, fmt.Errorf("initialize onnxruntime: %w", err)
		}
	}

	labelsPath := filepath.Join(modelDirPath, "voxlingua107_labels.json")
	labelsFile, err := os.Open(labelsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open labels file %s: %w", labelsPath, err)
	}
	defer labelsFile.Close()

	labelsContent, err := io.ReadAll(labelsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read labels: %w", err)
	}
	var labels []string
	if err := json.Unmarshal(labelsContent, &labels); err != nil {
		return nil, fmt.Errorf("failed to unmarshal labels: %w", err)
	}
	kkIndex := indexWithPrefix(labels, "kk")
	if kkIndex == -1 {
		return nil, errors.New("unique kk label not found")
	}

	modelPath := filepath.Join(modelDirPath, "voxlingua107_ecapa.onnx")
	sess, err := ort.NewDynamicAdvancedSession(
		modelPath,
		[]string{"waveform"},
		[]string{"log_probs"},
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create ONNX runtime session for langid: %w", err)
	}
	return &Classifier{
		logger:  logger,
		session: sess,
		labels:  labels,
		kkIndex: kkIndex,
	}, nil
}

func (c *Classifier) IsKazakh(f32Data []float32) (bool, error) {
	start := time.Now()
	n := len(f32Data)
	if n < LangIdSampleMinLength {
		return false, fmt.Errorf("audio too short for langid: %d frames", n)
	}
	if n > LangIdSampleMaxLength {
		return false, fmt.Errorf("audio too long for langid: %d frames", n)
	}
	inputShape := ort.NewShape(1, int64(n))
	input, err := ort.NewTensor(inputShape, f32Data)
	if err != nil {
		return false, err
	}
	defer input.Destroy()

	outputShape := ort.NewShape(1, int64(len(c.labels)))
	outputBuf := make([]float32, len(c.labels))
	output, err := ort.NewTensor(outputShape, outputBuf)
	if err != nil {
		return false, err
	}
	defer output.Destroy()

	c.lock.Lock()
	defer c.lock.Unlock()
	if err := c.session.Run([]ort.Value{input}, []ort.Value{output}); err != nil {
		c.logger.Error("langid model failed", "audio", len(f32Data), "err", err)
		return false, fmt.Errorf("run langid model: %w", err)
	}
	logp := output.GetData()
	top, err := top3(logp)
	if err != nil {
		return false, fmt.Errorf("top3 fail: %w", err)
	}
	isKazakh := slices.Contains(top, c.kkIndex) && math.Exp(float64(logp[c.kkIndex])) > LangIdMinProbability
	elapsed := time.Since(start)
	topLanguages := make([]string, 0, len(top))
	for _, index := range top {
		prob := math.Exp(float64(logp[index]))
		topLanguages = append(topLanguages, fmt.Sprintf("%s - %f", c.labels[index], prob))
	}
	c.logger.Info("languages identified", "isKazakh", isKazakh, "topLanguages", topLanguages, "time", elapsed)
	return isKazakh, nil
}
