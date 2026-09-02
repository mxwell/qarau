package llm

import (
	"context"
	"log/slog"

	dbgen "github.com/mxwell/qarau/db/gen"
	"github.com/mxwell/qarau/internal/constants"
)

// Return batch items, batch start in sent seq, error
func GetBatchBoundariesAndContent(
	ctx context.Context,
	queries *dbgen.Queries,
	logger *slog.Logger,
	transcriptionID int64,
	sentSeq int32,
) ([]dbgen.GetSentencesRangeRow, int32, error) {
	batchStart := (sentSeq / constants.SentenceBatchSize) * constants.SentenceBatchSize

	batch, err := queries.GetSentencesRange(ctx, dbgen.GetSentencesRangeParams{
		TranscriptionID: transcriptionID,
		StartSeq:        batchStart,
		EndSeq:          batchStart + constants.SentenceBatchSize - 1, // inclusive
	})
	if err != nil {
		logger.Error(
			"sentence batch load fail",
			"transcriptionID", transcriptionID,
			"batchStart", batchStart,
			"err", err,
		)
		return nil, batchStart, err
	}
	if len(batch) == 0 {
		logger.Info("no sents found", "transcriptionID", transcriptionID, "batchStart", batchStart)
	}
	return batch, batchStart, nil
}
