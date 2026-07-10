package eleven

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"mime/multipart"
	"net/http"

	qarauv1 "github.com/mxwell/qarau/gen/qarau/v1"
	"github.com/mxwell/qarau/internal/asr"
	constants "github.com/mxwell/qarau/internal/common"
)

type elevenTranscriber struct {
	logger *slog.Logger
	apiKey string
}

const (
	minSeconds       = 2
	speechToTextUrl  = "https://api.elevenlabs.io/v1/speech-to-text"
	elevenSTTModelID = "scribe_v2"
	fileFormat       = "pcm_s16le_16"
	languageCode     = "kaz"
	granularity      = "word"
)

func NewElevenTranscriber(logger *slog.Logger, apiKey string) (asr.Transcriber, error) {
	if logger == nil {
		return nil, errors.New("nil logger in elevenTranscriber creation")
	}
	if apiKey == "" {
		return nil, errors.New("empty apiKey in elevenTranscriber creation")
	}
	return &elevenTranscriber{
		logger: logger,
		apiKey: apiKey,
	}, nil
}

type elevenWord struct {
	Text     string  `json:"text"`
	Start    float64 `json:"start"`
	End      float64 `json:"end"`
	WordType string  `json:"type"`
	Logprob  float64 `json:"logprob"`
}

type elevenResponse struct {
	LanguageCode        string       `json:"language_code"`
	LanguageProbability float64      `json:"language_probability"`
	Text                string       `json:"text"`
	Words               []elevenWord `json:"words"`
	TranscriptionID     string       `json:"transcription_id"`
	AudioDurationSecs   float64      `json:"audio_duration_secs"`
}

type Float interface {
	float32 | float64
}

func clamp[T Float](x T, lower T, upper T) T {
	if x < lower {
		return lower
	}
	if x > upper {
		return upper
	}
	return x
}

func (t *elevenTranscriber) requestSTT(ctx context.Context, pcm []byte) ([]byte, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("model_id", elevenSTTModelID); err != nil {
		return nil, err
	}
	if err := w.WriteField("file_format", fileFormat); err != nil {
		return nil, err
	}
	if err := w.WriteField("timestamps_granularity", granularity); err != nil {
		return nil, err
	}
	if err := w.WriteField("language_code", languageCode); err != nil {
		return nil, err
	}
	fileWriter, err := w.CreateFormFile("file", "audio.pcm")
	if err != nil {
		return nil, err
	}
	if _, err = fileWriter.Write(pcm); err != nil {
		return nil, err
	}
	if err = w.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		speechToTextUrl,
		&body,
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("xi-api-key", t.apiKey)
	req.Header.Set("Content-Type", w.FormDataContentType())

	t.logger.Info("requesting STT API", "size", len(pcm))
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, errors.New("nil API response")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("bad status code in API response: %d", response.StatusCode)
	}

	return io.ReadAll(response.Body)
}

func (t *elevenTranscriber) parseEleven(data []byte) (*qarauv1.Transcription, error) {
	var response elevenResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, err
	}

	packedWords := make([]*qarauv1.Word, 0)

	for _, word := range response.Words {
		if word.WordType != "word" {
			continue
		}
		pct := 100 * math.Exp(word.Logprob)
		packedWords = append(packedWords, &qarauv1.Word{
			Word:       word.Text,
			StartMs:    uint32(word.Start * 1000),
			EndMs:      uint32(word.End * 1000),
			Confidence: uint32(clamp(pct, 0, 100)),
		})
	}
	t.logger.Info("parsed API response", "before", len(response.Words), "after", len(packedWords))

	return &qarauv1.Transcription{
		Model: elevenSTTModelID,
		Words: packedWords,
	}, nil
}

func (t *elevenTranscriber) Transcribe(
	ctx context.Context,
	pcmReader io.Reader,
	durationSecs int32,
	sampleRate float64,
) (*qarauv1.Transcription, error) {
	if durationSecs > constants.MaxDurationSecs {
		return nil, fmt.Errorf(
			"too long duration for ASR: %d > %d seconds",
			durationSecs,
			constants.MaxDurationSecs,
		)
	}

	pcm, err := io.ReadAll(pcmReader)
	if err != nil {
		return nil, err
	}

	pcmSize := float64(len(pcm))
	if pcmSize < minSeconds*sampleRate*2 || pcmSize > constants.MaxDurationSecs*sampleRate*2 {
		return nil, fmt.Errorf("too large audio data size: %f Bytes", pcmSize)
	}

	responseJson, err := t.requestSTT(ctx, pcm)
	if err != nil {
		return nil, err
	}

	return t.parseEleven(responseJson)
}
