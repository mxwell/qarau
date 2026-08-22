package asr

import (
	"context"
	"errors"
	"io"

	qarauv1 "github.com/mxwell/qarau/gen/qarau/v1"
)

var (
	ErrAudioNotKazakh = errors.New("audio language not identified as Kazakh")
)

type Transcriber interface {
	Transcribe(context.Context, io.Reader, int32, float64) (*qarauv1.Transcription, error)
}
