package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/mxwell/qarau/internal/quota"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/responses"
)

// StreamedSentence is a single sentence breakdown, delivered as soon as its
// JSON object has fully arrived
type StreamedSentence struct {
	SentenceNum int
	Sentence    SentenceBreakdownGenericV1
	Elapsed     time.Duration
}

type SentenceCallback func(StreamedSentence)

// pumpResult carries what only the pump goroutine can observe.
type pumpResult struct {
	usage quota.TokenUsage
	err   error
}

// streamSentenceBreakdownRuV1 issues a streaming request and decodes the
// sentences array incrementally.
//
// No retries at the moment.
func (c OaiClient) streamSentenceBreakdownRuV1(
	ctx context.Context,
	title, channel string,
	promptVersion uint,
	sentences []string,
	onSentence SentenceCallback,
) ([]SentenceBreakdownRuV1, quota.TokenUsage, error) {
	usage := quota.TokenUsage{}

	body, err := c.buildBreakdownBodyRuV1(title, channel, promptVersion, sentences)
	if err != nil {
		return nil, usage, err
	}

	requestCtx, cancel := context.WithTimeout(ctx, promptTimeout)
	defer cancel()

	promptStart := time.Now()
	stream := c.client.Responses.NewStreaming(requestCtx, body, option.WithMaxRetries(0))
	defer stream.Close()

	// The pump feeds raw output text into a pipe.
	// The decoder below reads it
	// and blocks until each sentence object is complete.
	reader, writer := io.Pipe()
	done := make(chan pumpResult, 1)
	go func() {
		res := c.pumpStream(stream, writer, promptStart)
		writer.CloseWithError(res.err)
		done <- res
	}()

	collected, decodeErr := c.decodeSentenceStream(reader, promptStart, onSentence)
	// Unblock the pump if we stopped reading early, e.g. on a decode error.
	reader.CloseWithError(decodeErr)

	res := <-done
	usage = res.usage

	if res.err != nil {
		c.logger.Error(
			"LLM stream fail",
			"time", fmt.Sprintf("%.3f", time.Since(promptStart).Seconds()),
			"sentences", len(collected),
			"err", res.err,
		)
		return collected, usage, fmt.Errorf("llm stream fail: %w", res.err)
	}
	if decodeErr != nil {
		c.logger.Error(
			"LLM stream decode fail",
			"time", fmt.Sprintf("%.3f", time.Since(promptStart).Seconds()),
			"sentences", len(collected),
			"err", decodeErr,
		)
		return collected, usage, decodeErr
	}

	return collected, usage, nil
}

// Event types of the Responses streaming API that we are interested in
const (
	eventOutputTextDelta = "response.output_text.delta"
	eventCompleted       = "response.completed"
	eventFailed          = "response.failed"
	eventIncomplete      = "response.incomplete"
	eventError           = "error"
)

// pumpStream forwards output text deltas into writer and collects token usage
func (c OaiClient) pumpStream(
	stream *ssestream.Stream[responses.ResponseStreamEventUnion],
	w io.Writer,
	promptStart time.Time,
) pumpResult {
	res := pumpResult{}
	firstDelta := true

	for stream.Next() {
		event := stream.Current()

		switch event.Type {
		case eventOutputTextDelta:
			if firstDelta {
				firstDelta = false
				c.logger.Info(
					"llm stream - first output token",
					"time", fmt.Sprintf("%.3f", time.Since(promptStart).Seconds()),
				)
			}
			if _, err := io.WriteString(w, event.Delta); err != nil {
				res.err = err
				return res
			}
		case eventCompleted:
			res.usage.Input = event.Response.Usage.InputTokens
			res.usage.Output = event.Response.Usage.OutputTokens
			c.logger.Info(
				"llm stream response - token usage",
				"input", event.Response.Usage.InputTokens,
				"output", event.Response.Usage.OutputTokens,
				"reasoning", event.Response.Usage.OutputTokensDetails.ReasoningTokens,
				"time", fmt.Sprintf("%.3f", time.Since(promptStart).Seconds()),
			)
		case eventIncomplete:
			// Usage still counts against the quota even when truncated.
			res.usage.Input = event.Response.Usage.InputTokens
			res.usage.Output = event.Response.Usage.OutputTokens
			res.err = fmt.Errorf("incomplete response: %s", event.Response.IncompleteDetails.Reason)
			return res
		case eventFailed:
			res.usage.Input = event.Response.Usage.InputTokens
			res.usage.Output = event.Response.Usage.OutputTokens
			res.err = fmt.Errorf("failed response: %s", event.Response.Error.Message)
			return res
		case eventError:
			res.err = fmt.Errorf("stream error %s: %s", event.Code, event.Message)
			return res
		}
	}
	res.err = stream.Err()

	return res
}

// decodeSentenceStream walks the top-level object until it reaches the
// "sentences" array, then decodes its elements one at a time.
func (c OaiClient) decodeSentenceStream(
	r io.Reader,
	promptStart time.Time,
	onSentence SentenceCallback,
) ([]SentenceBreakdownRuV1, error) {
	dec := json.NewDecoder(r)

	if err := expectDelim(dec, '{'); err != nil {
		return nil, fmt.Errorf("llm stream: response start: %w", err)
	}

	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("llm stream: field name: %w", err)
		}
		key, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("llm stream: expected field name, got %v", token)
		}
		if key != "sentences" {
			var skipped json.RawMessage
			if err := dec.Decode(&skipped); err != nil {
				return nil, fmt.Errorf("llm stream: skip field %q: %w", key, err)
			}
			continue
		}
		return c.decodeSentenceArray(dec, promptStart, onSentence)
	}

	return nil, errors.New("llm stream: no 'sentences' field in response")
}

func (c OaiClient) decodeSentenceArray(
	dec *json.Decoder,
	promptStart time.Time,
	onSentence SentenceCallback,
) ([]SentenceBreakdownRuV1, error) {
	if err := expectDelim(dec, '['); err != nil {
		return nil, fmt.Errorf("llm stream: sentences array start: %w", err)
	}

	result := make([]SentenceBreakdownRuV1, 0)
	for dec.More() {
		var sent SentenceBreakdownRuV1
		if err := dec.Decode(&sent); err != nil {
			return result, fmt.Errorf("llm stream: sentence %d decode fail: %w", len(result)+1, err)
		}
		result = append(result, sent)

		if onSentence != nil {
			onSentence(StreamedSentence{
				SentenceNum: sent.SentenceNum,
				Sentence: SentenceBreakdownGenericV1{
					Sentence:     sent.Sentence,
					Translations: sent.Translations,
					Breakdown:    wordRuV1ToGeneric(sent.Breakdown),
				},
				Elapsed: time.Since(promptStart),
			})
		}
	}

	// dec.More() also goes false on a truncated stream, so the closing bracket
	// is what tells a complete response apart from a cut-off one.
	if err := expectDelim(dec, ']'); err != nil {
		return result, fmt.Errorf("llm stream: sentences array end after %d sentences: %w", len(result), err)
	}

	return result, nil
}

func expectDelim(dec *json.Decoder, want json.Delim) error {
	token, err := dec.Token()
	if err != nil {
		return err
	}
	got, ok := token.(json.Delim)
	if !ok || got != want {
		return fmt.Errorf("expected %q, got %v", want, token)
	}
	return nil
}

// DoSentenceBreakdownStream is the streaming counterpart of DoSentenceBreakdown:
// onSentence fires per sentence as it arrives, while the
// returned result is identical in shape to the blocking call.
func (c OaiClient) DoSentenceBreakdownStream(
	ctx context.Context,
	targetLang, title, channel string,
	promptVersion uint,
	sentences []string,
	onSentence SentenceCallback,
) (SentenceBreakdownResult, error) {
	if targetLang != LangRu {
		return SentenceBreakdownResult{Usage: zeroUsage}, fmt.Errorf(
			"streaming sentence breakdown is not implemented for targetLang %s", targetLang,
		)
	}

	raw, usage, err := c.streamSentenceBreakdownRuV1(ctx, title, channel, promptVersion, sentences, onSentence)
	if err != nil {
		return SentenceBreakdownResult{Usage: usage}, err
	}

	generic, err := c.arrangeAndConvert(len(sentences), raw)
	if err != nil {
		c.logger.Error("llm stream response conversion fail", "err", err)
		return SentenceBreakdownResult{Usage: usage}, err
	}

	return SentenceBreakdownResult{
		Metadata: BreakdownMetadata{
			Model:         modelForBreakdown,
			PromptVersion: promptVersion,
		},
		Usage:     usage,
		Sentences: generic,
	}, nil
}
