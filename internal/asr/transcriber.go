package asr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"

	vosk "github.com/alphacep/vosk-api/go"
)

type Transcriber interface {
	Transcribe(context.Context, io.Reader, int32, float64) ([]PackedWord, error)
}

type voskTranscriber struct {
	logger *slog.Logger
	model  *vosk.VoskModel
}

func NewVoskTranscriber(logger *slog.Logger, modelPath string) (Transcriber, error) {
	if logger == nil {
		return nil, errors.New("nil logger in voskTranscriber creation")
	}
	vosk.SetLogLevel(-1)
	logger.Info("initializing vosk model", "path", modelPath)
	model, err := vosk.NewModel(modelPath)
	if err != nil {
		return nil, err
	}
	return &voskTranscriber{
		logger: logger,
		model:  model,
	}, nil
}

type voskResultWord struct {
	Conf  float64 `json:"conf"`
	End   float64 `json:"end"`
	Start float64 `json:"start"`
	Word  string  `json:"word"`
}

type voskResult struct {
	Result []voskResultWord `json:"result"`
	Text   string           `json:"text"`
}

type PackedWord struct {
	Word        string
	StartMs     int
	EndMs       int
	ConfPercent int8
}

func packWord(resultWord voskResultWord) PackedWord {
	return PackedWord{
		Word:        resultWord.Word,
		StartMs:     int(resultWord.Start * 1000),
		EndMs:       int(resultWord.End * 1000),
		ConfPercent: int8(min(100, resultWord.Conf*100)),
	}
}

func (t *voskTranscriber) Transcribe(
	ctx context.Context,
	pcmReader io.Reader,
	durationSecs int32,
	sampleRate float64,
) ([]PackedWord, error) {
	rec, err := vosk.NewRecognizer(t.model, sampleRate)
	if err != nil {
		return nil, err
	}
	defer rec.Free()
	rec.SetWords(1) // include words with timestamps into the result

	buf := make([]byte, 16_384) // about 0.5 second
	readBytes := 0
	readChunks := 0
	estimatedBytes := durationSecs * 2 * int32(sampleRate)
	emptyReadsInRow := 0

	packedWords := make([]PackedWord, 0)

	for {
		n, err := pcmReader.Read(buf)
		if n > 0 {
			emptyReadsInRow = 0

			rec.AcceptWaveform(buf[:n])
			readBytes += n
			readChunks += 1
		} else {
			emptyReadsInRow += 1
			if emptyReadsInRow > 10 {
				t.logger.Error("stopping after too many empty reads in a row", "emptyReads", emptyReadsInRow)
				return packedWords, errors.New("too many empty reads")
			}
		}
		if ctx.Err() != nil {
			t.logger.Info("context cancelled", "err", ctx.Err())
			return packedWords, ctx.Err()
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			t.logger.Error("failed to read PCM data", "err", err)
			return packedWords, err
		}
		if readChunks%10 == 0 {
			t.logger.Info("copied PCM to recognizer", "bytes", fmt.Sprintf("%d/%d", readBytes, estimatedBytes), "chunks", readChunks)
		}
		result := rec.Result()

		var parsed voskResult
		if err := json.Unmarshal([]byte(result), &parsed); err != nil {
			t.logger.Error("result parse fail", "result", result, "err", err)
			return packedWords, fmt.Errorf("Vosk result parse fail: %w", err)
		}
		if len(parsed.Text) > 0 {
			t.logger.Info("result parsed", "parsed", parsed.Text)
		}
		resultWords := parsed.Result
		for i := range resultWords {
			packedWords = append(packedWords, packWord(resultWords[i]))
		}
	}

	t.logger.Info("finished copying PCM to recognizer", "bytes", readBytes, "chunks", readChunks)

	finalResult := rec.FinalResult()

	var parsed voskResult
	if err := json.Unmarshal([]byte(finalResult), &parsed); err != nil {
		t.logger.Error("final result parse fail", "finalResult", finalResult, "err", err)
		return packedWords, fmt.Errorf("Vosk final result parse fail: %w", err)
	}
	t.logger.Info("final result parsed", "parsed", parsed.Text)
	resultWords := parsed.Result
	for i := range resultWords {
		packedWords = append(packedWords, packWord(resultWords[i]))
	}

	return packedWords, nil
}
