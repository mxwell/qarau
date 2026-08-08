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
	"math/rand/v2"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	qarauv1 "github.com/mxwell/qarau/gen/qarau/v1"
	"github.com/mxwell/qarau/internal/asr"
	"github.com/mxwell/qarau/internal/audio"
	"github.com/mxwell/qarau/internal/constants"
	"github.com/mxwell/qarau/internal/langid"
	"github.com/mxwell/qarau/internal/vad"
	"github.com/streamer45/silero-vad-go/speech"
	"golang.org/x/sync/errgroup"
)

type elevenTranscriber struct {
	logger     *slog.Logger
	detector   *speech.Detector
	classifier *langid.Classifier
	apiKey     string
	testMode   bool
}

const (
	minSeconds       = 2
	speechToTextUrl  = "https://api.elevenlabs.io/v1/speech-to-text"
	elevenSTTModelID = "scribe_v2"
	fileFormat       = "pcm_s16le_16"
	languageCode     = "kaz"
	diarize          = "true"
	granularity      = "word"

	partMinFrames      = 7 * 60 * audio.SampleRateHz // min part size is 7 minutes
	partsMax           = 10                          // 11labs API limit is 20 concurrent jobs
	apiRequestAttempts = 3
)

func NewElevenTranscriber(
	logger *slog.Logger,
	detector *speech.Detector,
	classifier *langid.Classifier,
	apiKey string,
	testMode bool,
) (asr.Transcriber, error) {
	if logger == nil {
		return nil, errors.New("nil logger in elevenTranscriber creation")
	}
	if detector == nil {
		return nil, errors.New("nil detector in elevenTranscriber creation")
	}
	if classifier == nil {
		return nil, errors.New("nil classifier in elevenTranscriber creation")
	}
	if apiKey == "" {
		return nil, errors.New("empty apiKey in elevenTranscriber creation")
	}
	return &elevenTranscriber{
		logger:     logger,
		detector:   detector,
		classifier: classifier,
		apiKey:     apiKey,
		testMode:   testMode,
	}, nil
}

type elevenWord struct {
	Text      string  `json:"text"`
	Start     float64 `json:"start"`
	End       float64 `json:"end"`
	WordType  string  `json:"type"`
	SpeakerId string  `json:"speaker_id,omitempty"`
	Logprob   float64 `json:"logprob"`
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

// Returns (body, retriable, error)
func (t *elevenTranscriber) requestSTT(ctx context.Context, pcm []byte) ([]byte, bool, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("model_id", elevenSTTModelID); err != nil {
		return nil, false, err
	}
	if err := w.WriteField("diarize", diarize); err != nil {
		return nil, false, err
	}
	if err := w.WriteField("file_format", fileFormat); err != nil {
		return nil, false, err
	}
	if err := w.WriteField("timestamps_granularity", granularity); err != nil {
		return nil, false, err
	}
	if err := w.WriteField("language_code", languageCode); err != nil {
		return nil, false, err
	}
	fileWriter, err := w.CreateFormFile("file", "audio.pcm")
	if err != nil {
		return nil, false, err
	}
	if _, err = fileWriter.Write(pcm); err != nil {
		return nil, false, err
	}
	if err = w.Close(); err != nil {
		return nil, false, err
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		speechToTextUrl,
		&body,
	)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("xi-api-key", t.apiKey)
	req.Header.Set("Content-Type", w.FormDataContentType())

	t.logger.Info("requesting STT API", "size", len(pcm))

	if t.testMode {
		if rand.Int()%10 <= 2 {
			return nil, true, errors.New("random error")
		} else {
			durationMillis := len(pcm) / audio.BytesPer16bitFrame / audio.SampleRateKhz
			midMillis := durationMillis / 2
			endMillis := min(durationMillis, midMillis+1000)
			wordItem := fmt.Sprintf(
				`{ "text": "алма", "start": %.03f, "end": %.03f, "type": "word", "speaker_id": "1", "logprob": -0.1 }`,
				float64(midMillis)/1000,
				float64(endMillis)/1000,
			)
			t.logger.Info("test response word", "word", wordItem)
			responseString := fmt.Sprintf(
				`{ "language_code": "kk", "language_probability": 1.0, "text": "алма", "words": [%s], "transcription_id": "123", "audio_duration_secs": 1 }`,
				wordItem,
			)
			return []byte(responseString), true, nil
		}
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, true, err
	}
	if response == nil {
		return nil, true, errors.New("nil API response")
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		retriable := response.StatusCode == 429 || response.StatusCode >= 500
		return nil, retriable, fmt.Errorf("bad status code in API response: %d", response.StatusCode)
	}

	bodyBytes, err := io.ReadAll(response.Body)
	return bodyBytes, true, err
}

func (t *elevenTranscriber) parseEleven(data []byte) (*qarauv1.Transcription, error) {
	var response elevenResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, err
	}

	packedWords := make([]*qarauv1.Word, 0)

	speakers := map[string]uint32{}

	for _, word := range response.Words {
		if word.WordType != "word" {
			continue
		}
		pct := 100 * math.Exp(word.Logprob)
		speakerIdx, ok := speakers[word.SpeakerId]
		if !ok {
			speakerIdx = uint32(len(speakers))
			speakers[word.SpeakerId] = speakerIdx
		}
		packedWords = append(packedWords, &qarauv1.Word{
			Word:       word.Text,
			StartMs:    uint32(word.Start * 1000),
			EndMs:      uint32(word.End * 1000),
			Confidence: uint32(clamp(pct, 0, 100)),
			Speaker:    speakerIdx,
		})
	}
	t.logger.Info(
		"parsed API response",
		"before", len(response.Words),
		"after", len(packedWords),
		"speakers", len(speakers))

	return &qarauv1.Transcription{
		Model: elevenSTTModelID,
		Words: packedWords,
	}, nil
}

func (t *elevenTranscriber) requestWithRetries(ctx context.Context, pcm []byte, attempts int) (*qarauv1.Transcription, error) {
	var savedErr error
	for attempt := range attempts {
		savedErr = nil
		responseJson, retriable, err := t.requestSTT(ctx, pcm)
		if err != nil {
			savedErr = err
			t.logger.Error(
				"STT request failed",
				"attempt", attempt+1,
				"attempts", attempts,
				"err", err,
			)
			if retriable && attempt+1 < attempts {
				delayMillis := time.Duration(1000*(2<<attempt) + rand.Int()%1000)
				delay := time.Millisecond * delayMillis
				t.logger.Info(
					"STT request will retry",
					"delayMillis", delay.Milliseconds(),
				)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(delay):
				}
				continue
			} else {
				break
			}
		}
		parsedResponse, err := t.parseEleven(responseJson)
		if err != nil {
			savedErr = err
			t.logger.Error(
				"STT response parsing failed",
				"attempt", attempt+1,
				"attempts", attempts,
				"err", err,
			)
			continue
		}
		return parsedResponse, nil
	}
	if savedErr != nil {
		return nil, fmt.Errorf("STT request fail after %d attempts: %w", attempts, savedErr)
	}
	return nil, fmt.Errorf("STT request fail after %d attempts", attempts)
}

func (t *elevenTranscriber) applyVad(f32Data []float32) ([]speech.Segment, error) {
	frames := len(f32Data)
	t.detector.Reset() // detector accumulates state across runs, let's reset it
	segments, err := t.detector.Detect(f32Data)
	if err != nil {
		t.logger.Error("vad failed", "frames", frames, "err", err)
		return nil, fmt.Errorf("vad fail: %w", err)
	}
	t.logger.Info("vad applied", "frames", frames, "segments", len(segments))
	return segments, nil
}

func printDetectedTimestamps(timestamps []speech.Segment) {
	parts := make([]string, 0)
	for i := range timestamps {
		parts = append(parts, fmt.Sprintf("%f-%f", timestamps[i].SpeechStartAt, timestamps[i].SpeechEndAt))
	}
	fmt.Println("Detected timestamps", strings.Join(parts, "  "))
}

func printFragments(fragments []vad.Fragment) {
	parts := make([]string, 0)
	for i := range fragments {
		subparts := make([]string, 0)
		for j := range fragments[i].Ranges {
			r := &fragments[i].Ranges[j]
			subparts = append(subparts, fmt.Sprintf("[%d-%d -> %d]", r.RealStart, r.RealEnd, r.CopyStart))
		}
		parts = append(parts, "{ "+strings.Join(subparts, " ")+" }")
	}
	fmt.Println("Fragments", strings.Join(parts, "; "))
}

func (t *elevenTranscriber) stitch(fragments []vad.Fragment) ([]byte, error) {
	if len(fragments) == 0 {
		return nil, errors.New("no fragments to stitch")
	}
	size := 0
	for i := range fragments {
		size += fragments[i].LengthInFrames()
	}
	result := make([]byte, 0, size*2)
	for i := range fragments {
		result = append(result, fragments[i].Content...)
	}
	return result, nil
}

func (t *elevenTranscriber) partition(fragments []vad.Fragment) ([][]vad.Fragment, error) {
	totalFrames := 0
	for i := range fragments {
		totalFrames += fragments[i].LengthInFrames()
	}
	if totalFrames == 0 {
		return nil, errors.New("no content in fragments")
	}
	targetFrames := max(partMinFrames, totalFrames/partsMax)
	partitions := partitionFragments(t.logger, fragments, targetFrames)
	if partitions == nil {
		return nil, errors.New("failed to partition fragments")
	}
	t.logger.Info("partitioned fragments",
		"partitions", len(partitions),
		"targetFrames", targetFrames,
		"fragments", len(fragments),
		"frames", totalFrames,
	)
	return partitions, nil
}

func (t *elevenTranscriber) concatWords(parts [][]*qarauv1.Word) []*qarauv1.Word {
	n := len(parts)

	wordCount := 0
	for i := range n {
		wordCount += len(parts[i])
	}
	words := make([]*qarauv1.Word, 0, wordCount)
	for i := range n {
		var prevStart uint32
		if len(words) > 0 {
			prevStart = words[len(words)-1].StartMs
		}
		if len(parts[i]) == 0 {
			continue
		}
		curStart := parts[i][0].StartMs
		if prevStart > curStart {
			t.logger.Warn(
				"word timestamps overlap in concatWords",
				"offset", len(words),
				"prevStart", prevStart,
				"curStart", curStart,
			)
		}
		words = append(words, parts[i]...)
	}
	t.logger.Info(
		"concatenated words from parts",
		"parts", n,
		"wordCount", wordCount,
	)
	return words
}

func (t *elevenTranscriber) parallelRequest(
	ctx context.Context,
	fragments []vad.Fragment,
	requestAttempts int,
) ([]*qarauv1.Word, error) {
	partitions, err := t.partition(fragments)
	if err != nil {
		return nil, fmt.Errorf("partition fail in parallelRequest: %w", err)
	}

	n := len(partitions)
	inputs := make([][]byte, n)
	for i := range partitions {
		inputs[i], err = t.stitch(partitions[i])
		if err != nil {
			return nil, fmt.Errorf("stitch fail in parallelRequest: %w", err)
		}
	}

	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(partsMax)
	results := make([][]*qarauv1.Word, n)
	for i := range n {
		group.Go(func() error {
			partTranscription, err := t.requestWithRetries(ctx, inputs[i], requestAttempts)
			if err != nil {
				t.logger.Error(
					"STT part processing failed",
					"part", i,
					"err", err,
				)
				return fmt.Errorf("part %d/%d failed: %w", i, n, err)
			}
			restoredWords := vad.RestoreWordTimestamps(
				t.logger,
				vad.JoinRanges(partitions[i]),
				partTranscription.Words,
			)
			results[i] = restoredWords
			t.logger.Info(
				"STT part processed",
				"part", i,
				"words", len(partTranscription.Words),
			)
			return nil
		})
	}

	if err := group.Wait(); err != nil {
		return nil, err
	}

	return t.concatWords(results), nil
}

func printWords(words []*qarauv1.Word) {
	parts := make([]string, 0)
	for i := range words {
		w := words[i]
		parts = append(parts, fmt.Sprintf("[%d-%d %s]", w.StartMs, w.EndMs, w.Word))
	}
	fmt.Println("Words", strings.Join(parts, "  "))
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

	f32Data, err := audio.Convert16BitBytesToF32(pcm)
	if err != nil {
		return nil, fmt.Errorf("failed to convert pcm bytes to float32: %w", err)
	}

	speechSegments, err := t.applyVad(f32Data)
	if err != nil {
		return nil, err
	}
	if len(speechSegments) == 0 {
		t.logger.Info("no speech detected", "pcm", len(pcm))
		return &qarauv1.Transcription{
			Model: elevenSTTModelID,
			Words: make([]*qarauv1.Word, 0),
		}, nil
	}
	if t.testMode {
		printDetectedTimestamps(speechSegments)
	}
	fragments, err := vad.SegmentAudioByTimestampRanges(
		t.logger,
		pcm,
		speechSegments,
		/* padMillis */ 100,
		/* gapMaxMillis = 5 secs */ 5000,
		/* fragmentMaxMillis = 5 minutes */ 300_000,
	)
	if err != nil {
		t.logger.Error("SegmentAudioByTimestampRanges fail", "err", err, "pcm", len(pcm), "speechSegments", len(speechSegments))
		return nil, fmt.Errorf("SegmentAudioByTimestampRanges fail: %w", err)
	}
	if len(fragments) == 0 {
		t.logger.Error("no fragments in SegmentAudioByTimestampRanges", "speechSegments", len(speechSegments))
		return nil, fmt.Errorf("SegmentAudioByTimestampRanges returned no fragments")
	}
	if t.testMode {
		printFragments(fragments)
	}

	langIdSamples, err := langid.SampleForLangID(t.logger, fragments)
	if err != nil {
		// XXX we can (1) skip langid, or (2) stop if no sample is collected.
		// Choosing 'stop' to make it more noticeable for deeper investigation.
		return nil, fmt.Errorf("sampling for langid failed: %w", err)
	}

	kazakhCount := 0
	for sampleIndex, sample := range langIdSamples {
		kazakh, err := t.classifier.IsKazakh(sample)
		if err != nil {
			t.logger.Error("langid fail", "err", err, "sampleIndex", sampleIndex)
			return nil, fmt.Errorf("langid fail: %w", err)
		}
		t.logger.Info("langid sample done", "kazakh", kazakh, "sampleIndex", sampleIndex)
		if kazakh {
			kazakhCount++
		}
	}
	t.logger.Info("langid done", "kazakhCount", kazakhCount, "samples", len(langIdSamples))

	if kazakhCount == 0 {
		return nil, asr.ErrAudioNotKazakh
	}

	restoredWords, err := t.parallelRequest(ctx, fragments, apiRequestAttempts)
	if err != nil {
		t.logger.Error(
			"parallelRequest failed",
			"fragments", len(fragments),
			"err", err,
		)
		return nil, fmt.Errorf("transcribe fail: %w", err)
	}

	if t.testMode {
		printWords(restoredWords)
	}

	return &qarauv1.Transcription{
		Model: elevenSTTModelID,
		Words: restoredWords,
	}, nil
}
