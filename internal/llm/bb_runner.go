package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	dbgen "github.com/mxwell/qarau/db/gen"
	"github.com/mxwell/qarau/internal/pg"
	"github.com/mxwell/qarau/internal/quota"
)

const (
	pollPeriod    = 10 * time.Second
	backOffPeriod = 10 * time.Minute

	lockPeriod = 5 * time.Minute
)

var (
	ErrCancelFromContext = errors.New("BBRunner cancelled from context")
)

type BBRunner struct {
	name      string
	logger    *slog.Logger
	queries   *dbgen.Queries
	llmQuota  *quota.LlmQuotaController
	oaiClient *OaiClient
	prompt    uint
}

func NewBBRunner(
	logger *slog.Logger,
	queries *dbgen.Queries,
	llmQuota *quota.LlmQuotaController,
	oaiClient *OaiClient,
	prompt uint,
) (*BBRunner, error) {
	if logger == nil {
		return nil, errors.New("nil logger in BBRunner creation")
	}
	if queries == nil {
		return nil, errors.New("nil queries in BBRunner creation")
	}
	if llmQuota == nil {
		return nil, errors.New("nil LLM quota controller in BBRunner creation")
	}
	if oaiClient == nil {
		return nil, errors.New("nil OpenAI client in BBRunner creation")
	}
	return &BBRunner{
		name:      "embedded_bbrunner_" + strconv.Itoa(rand.Int()%100),
		logger:    logger,
		queries:   queries,
		llmQuota:  llmQuota,
		oaiClient: oaiClient,
		prompt:    prompt,
	}, nil
}

func (r *BBRunner) checkLlmQuotaAvailable(ctx context.Context) (bool, error) {
	today := quota.GetTodayForQuota()
	llmQuotum, err := r.queries.GetLlmQuota(ctx, today)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			llmQuotum = dbgen.GetLlmQuotaRow{
				UsedInputTokens:  0,
				UsedOutputTokens: 0,
			}
		} else {
			return false, err
		}
	}
	quotaOk, quotaStatus := r.llmQuota.Available(
		quota.TokenUsage{
			Input:  llmQuotum.UsedInputTokens,
			Output: llmQuotum.UsedOutputTokens,
		},
	)
	if !quotaOk {
		r.logger.Info("daily llm quota used up", "status", quotaStatus)
		return false, nil
	}
	return true, nil
}

func (r *BBRunner) markFailed(
	ctx context.Context,
	claimedRow dbgen.ClaimBreakdownBatchRow,
	errorMessage string,
) {
	queryCtx := context.WithoutCancel(ctx)
	_, err := r.queries.MarkBreakdownBatchFailed(queryCtx, dbgen.MarkBreakdownBatchFailedParams{
		ErrorMessage: &errorMessage,
		BatchID:      claimedRow.ID,
		LockedBy:     &r.name,
	})
	if err != nil {
		r.logger.Error(
			"failed to mark batch failed",
			"batch", claimedRow.ID,
			"message", errorMessage,
			"err", err,
		)
	} else {
		r.logger.Info(
			"marked batch failed",
			"batch", claimedRow.ID,
			"transcription", claimedRow.TranscriptionID,
			"message", errorMessage,
		)
	}
}

func (r *BBRunner) markDone(
	ctx context.Context,
	claimedRow dbgen.ClaimBreakdownBatchRow,
) {
	queryCtx := context.WithoutCancel(ctx)
	_, err := r.queries.MarkBreakdownBatchDone(queryCtx, dbgen.MarkBreakdownBatchDoneParams{
		BatchID:  claimedRow.ID,
		LockedBy: &r.name,
	})
	if err != nil {
		r.logger.Error(
			"failed to mark batch done",
			"batch", claimedRow.ID,
			"err", err,
		)
	} else {
		r.logger.Info(
			"marked batch done",
			"batch", claimedRow.ID,
			"transcription", claimedRow.TranscriptionID,
			"start", claimedRow.BatchStartSeq,
			"lang", claimedRow.TargetLang,
		)
	}
}

func (r *BBRunner) packTranslationsAndWordBreakdowns(
	seq int32,
	breakdown SentenceBreakdownGenericV1,
) ([]byte, []byte, error) {
	plainTranslations := PlainTranslations{
		Variants: breakdown.Translations,
	}

	translationBytes, err := json.Marshal(plainTranslations)
	if err != nil {
		r.logger.Error("failed to marshal translations in sentence breakdown", "seq", seq, "translations", len(plainTranslations.Variants))
		return nil, nil, fmt.Errorf("translations marshalling fail: %w", err)
	}

	wordBreakdownItems := FromWordGenericV1(breakdown.Breakdown)
	plainBreakdown := PlainBreakdown{
		Words: wordBreakdownItems,
	}

	breakdownBytes, err := json.Marshal(plainBreakdown)
	if err != nil {
		r.logger.Error("failed to marshal words in sentence breakdown", "seq", seq, "words", len(plainBreakdown.Words))
		return nil, nil, fmt.Errorf("word breakdown marshalling fail: %w", err)
	}

	return translationBytes, breakdownBytes, nil
}

// Insert an early result, before a batch is complete
func (r *BBRunner) insertSingleBreakdown(
	ctx context.Context,
	transcriptionID int64,
	targetLang string,
	batch []dbgen.GetSentencesRangeRow,
	sentence StreamedSentence,
) error {
	sentIndex := sentence.SentIndex
	if sentIndex < 0 || sentIndex >= len(batch) {
		r.logger.Warn(
			"sentence index out of range",
			"transcriptionID", transcriptionID,
			"sentIndex", sentIndex,
			"batch", len(batch),
		)
		return errors.New("sentence index out of range")
	}
	if sentence.Breakdown.Sentence == "" {
		r.logger.Error(
			"empty sentence in generated breakdown",
			"transcriptionID", transcriptionID,
			"sentIndex", sentIndex,
			"batch", len(batch),
		)
		return errors.New("empty sentence in generated breakdown")
	}
	seq := batch[sentIndex].Seq
	translationBytes, breakdownBytes, err := r.packTranslationsAndWordBreakdowns(seq, sentence.Breakdown)
	if err != nil {
		return err
	}
	err = r.queries.InsertSentenceBreakdown(ctx, dbgen.InsertSentenceBreakdownParams{
		TranscriptionID: transcriptionID,
		SentenceSeq:     seq,
		TargetLang:      targetLang,
		Model:           sentence.Metadata.Model,
		PromptVersion:   int32(sentence.Metadata.PromptVersion),
		Translations:    translationBytes,
		Breakdown:       breakdownBytes,
	})
	if err != nil {
		r.logger.Error("failed to insert generated sent breakdown", "transcriptionID", transcriptionID, "seq", seq, "err", err)
		return fmt.Errorf("breakdown insert fail: %w", err)
	}
	r.logger.Info("inserted single sent breakdown", "transcriptionID", transcriptionID, "seq", seq)
	return nil
}

func (r *BBRunner) process(
	ctx context.Context,
	transcriptionID int64,
	targetLang string,
	batch []dbgen.GetSentencesRangeRow,
) error {
	sentences := make([]string, 0, len(batch))
	for i := range batch {
		sentences = append(sentences, batch[i].Text)
	}

	videoDetails, err := r.queries.GetVideoByTranscriptionID(ctx, transcriptionID)
	if err != nil {
		r.logger.Error("video details load fail", "err", err, "transcriptionID", transcriptionID)
		return fmt.Errorf("failed to load video details for breakdown: %w", err)
	}

	insertedIndexes := map[int]bool{}
	var savedInsertErr error
	breakdownResult, err := r.oaiClient.DoSentenceBreakdownStream(
		ctx,
		targetLang,
		videoDetails.Title,
		videoDetails.ChannelTitle,
		r.prompt,
		sentences,
		func(ss StreamedSentence) {
			insertErr := r.insertSingleBreakdown(
				ctx,
				transcriptionID,
				targetLang,
				batch,
				ss,
			)
			if insertErr != nil {
				r.logger.Error("insertSingleBreakdown fail", "insertErr", insertErr)
				if savedInsertErr == nil {
					savedInsertErr = insertErr
				}
			} else {
				insertedIndexes[ss.SentIndex] = true
			}
		},
	)
	if breakdownResult.Usage.Input > 0 || breakdownResult.Usage.Output > 0 {
		today := quota.GetTodayForQuota()
		row, quotaErr := r.queries.UpsertLlmQuota(ctx, dbgen.UpsertLlmQuotaParams{
			Day:              today,
			UsedInputTokens:  breakdownResult.Usage.Input,
			UsedOutputTokens: breakdownResult.Usage.Output,
		})
		if quotaErr != nil {
			r.logger.Error("llm quota upsert failed", "err", quotaErr)
			err = errors.Join(err, quotaErr)
		} else {
			r.logger.Info(
				"llm quota daily usage bumped",
				"input", row.UsedInputTokens,
				"output", row.UsedOutputTokens,
			)
		}
	}
	if err != nil {
		r.logger.Error("breakdown generation failed", "transcriptionID", transcriptionID, "err", err)
		return err
	}

	insertedCount := len(insertedIndexes)
	// the same condition as in arrangeAndConvert()
	if insertedCount*2 >= len(batch) {
		r.logger.Info(
			"inserted sentence breakdowns",
			"transcriptionID", transcriptionID,
			"inserted", insertedCount,
			"sentences", len(batch),
		)
	} else {
		r.logger.Error(
			"too few sentence breakdowns inserted",
			"transcriptionID", transcriptionID,
			"inserted", insertedCount,
			"sentences", len(batch),
			"err", savedInsertErr,
		)
		if savedInsertErr != nil {
			return fmt.Errorf("breakdown insert fail: %w", savedInsertErr)
		} else {
			return fmt.Errorf("only %d out of %d breakdowns inserted", insertedCount, len(batch))
		}
	}

	return nil
}

// Return:
// - flag that batch was found
// - flag for backoff
// - a fatal error that can't be ignored
func (r *BBRunner) tryBatch(ctx context.Context) (bool, bool, error) {
	batchFound := false
	backOff := false

	quotaOk, err := r.checkLlmQuotaAvailable(ctx)
	if err != nil {
		r.logger.Error("llm quota check fail", "err", err)
		return batchFound, backOff, nil
	}
	if !quotaOk {
		backOff = true
		return batchFound, backOff, nil
	}

	lockedUntil := pg.NewTimestamptz(time.Now().Add(lockPeriod))

	claimedRow, err := r.queries.ClaimBreakdownBatch(ctx, dbgen.ClaimBreakdownBatchParams{
		LockedBy:    &r.name,
		LockedUntil: lockedUntil,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			r.logger.Error("claim batch fail", "err", err)
		}
		return batchFound, backOff, nil
	}
	batchFound = true

	transcriptionID := claimedRow.TranscriptionID
	batchStart := claimedRow.BatchStartSeq
	targetLang := claimedRow.TargetLang

	batch, _, err := GetBatchBoundariesAndContent(
		ctx,
		r.queries,
		r.logger,
		transcriptionID,
		batchStart,
	)
	if err != nil {
		r.logger.Error(
			"batch load fail",
			"transcription", transcriptionID,
			"start", batchStart,
			"err", err,
		)
		return batchFound, backOff, nil
	}
	if len(batch) == 0 {
		r.logger.Error(
			"empty batch",
			"transcription", transcriptionID,
			"start", batchStart,
		)
		r.markFailed(ctx, claimedRow, "empty batch")
		return batchFound, backOff, nil
	}

	// inclusive
	batchEnd := batchStart + int32(len(batch)) - 1
	batchStartMs := batch[0].StartMs
	batchEndMs := batch[len(batch)-1].EndMs

	r.logger.Info(
		"loaded sent batch",
		"transcription", transcriptionID,
		"start", batchStart,
		"end", batchEnd,
		"start_ms", batchStartMs,
		"end_ms", batchEndMs,
	)

	err = r.process(ctx, transcriptionID, targetLang, batch)
	if err != nil {
		r.logger.Error(
			"batch processing failed",
			"batch", claimedRow.ID,
			"transcription", transcriptionID,
			"err", err,
		)
		r.markFailed(ctx, claimedRow, err.Error())
		return batchFound, backOff, nil
	}

	r.markDone(ctx, claimedRow)

	return batchFound, backOff, nil
}

func (r *BBRunner) sleep(ctx context.Context, period time.Duration) error {
	select {
	case <-ctx.Done():
		r.logger.Info("BBRunner received signal from context", "ctx.Err()", ctx.Err())
		return ErrCancelFromContext
	case <-time.After(period):
		return nil
	}
}

// Steps:
// - check LLM quota => back-off for a long time if no quota for today
// - pick a pending batch
// - load its sentences
// - send to LLM
// - store to DB
// - bump LLM usage
// - mark batch 'done' (or remove?)
func (r *BBRunner) Loop(ctx context.Context) error {
	announced := false

	for {
		if !announced {
			r.logger.Info("trying to claim batch")
			announced = true
		}

		batchFound, backOff, err := r.tryBatch(ctx)
		if err != nil {
			return err
		}

		if batchFound {
			announced = false
		}

		sleepPeriod := pollPeriod
		if backOff {
			sleepPeriod = backOffPeriod
			r.logger.Info("back-off in BBRunner", "period", sleepPeriod)
		}
		if err := r.sleep(ctx, sleepPeriod); err != nil {
			return err
		}
		if ctx.Err() != nil {
			r.logger.Info("stopping BBRunner", "ctx.Err()", ctx.Err())
			return ErrCancelFromContext
		}
	}
}
