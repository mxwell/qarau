package api

import (
	"context"
	"errors"
	"log/slog"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	dbgen "github.com/mxwell/qarau/db/gen"
	qarauv1 "github.com/mxwell/qarau/gen/qarau/v1"
	"github.com/mxwell/qarau/internal/sentences"
)

type SentenceManager struct {
	logger  *slog.Logger
	queries *dbgen.Queries
	pool    *pgxpool.Pool
}

func NewSentenceManager(logger *slog.Logger, queries *dbgen.Queries, pool *pgxpool.Pool) (*SentenceManager, error) {
	if logger == nil {
		return nil, errors.New("nil logger in SentenceManager creation")
	}
	if queries == nil {
		return nil, errors.New("nil queries in SentenceManager creation")
	}
	if pool == nil {
		return nil, errors.New("nil pool in SentenceManager creation")
	}
	return &SentenceManager{
		logger:  logger,
		queries: queries,
		pool:    pool,
	}, nil
}

func (m *SentenceManager) toSentences(words []*qarauv1.Word, transcriptionID int64) []sentences.Sentence {
	sents := sentences.SegmentWordStream(words)
	minLen := math.MaxInt32
	maxLen := 0
	for _, s := range sents {
		slen := s.EndWordSeq - s.StartWordSeq + 1
		minLen = min(minLen, slen)
		maxLen = max(maxLen, slen)
	}
	m.logger.Info(
		"segmented word stream into sentences",
		"sents", len(sents),
		"words", len(words),
		"transcriptionID", transcriptionID,
		"minLen", minLen,
		"maxLen", maxLen,
	)
	return sents
}

// Return number of inserted sentences, error
func (m *SentenceManager) upsertSentences(ctx context.Context, transcriptionID int64, sents []sentences.Sentence) (int64, error) {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) // safe even after tx.Commit() -- does nothing

	qtx := m.queries.WithTx(tx)

	m.logger.Info("deleting old sentences if any", "transcriptionID", transcriptionID)
	err = qtx.DeleteSentencesByTranscriptionId(ctx, transcriptionID)
	if err != nil {
		m.logger.Error("sentence delete query failed", "transcriptionID", transcriptionID, "err", err)
		return 0, err
	}

	sentParams := make([]dbgen.InsertSentencesParams, 0, len(sents))

	for i, sent := range sents {
		sentParams = append(sentParams, dbgen.InsertSentencesParams{
			TranscriptionID: transcriptionID,
			Seq:             int32(i),
			StartWordSeq:    int32(sent.StartWordSeq),
			EndWordSeq:      int32(sent.EndWordSeq),
			StartMs:         int32(sent.StartMs),
			EndMs:           int32(sent.EndMs),
			Text:            sent.Text,
		})
	}

	inserted, err := qtx.InsertSentences(ctx, sentParams)
	if err != nil {
		m.logger.Error("sentence insert query failed", "params", len(sentParams), "transcriptionID", transcriptionID, "err", err)
		return 0, err
	}
	m.logger.Info("stored new sentences", "inserted", inserted, "transcriptionID", transcriptionID)
	if err = tx.Commit(ctx); err != nil {
		m.logger.Error("sentence upsert tx fail", "transcriptionID", transcriptionID, "err", err)
		return 0, err
	}
	return inserted, nil
}

// Return number of inserted sentences, error
func (m *SentenceManager) CreateAndUpsertSentencesForTranscription(ctx context.Context, transcriptionID int64, transcription *qarauv1.Transcription) (int64, error) {
	sents := m.toSentences(transcription.Words, transcriptionID)
	return m.upsertSentences(ctx, transcriptionID, sents)
}

// Return number of inserted sentences, error
func (m *SentenceManager) BackfillSentences(ctx context.Context, transcriptionID int64) (int64, error) {
	_, err := m.queries.GetTranscription(ctx, transcriptionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			m.logger.Info("transcription not found", "transcriptionID", transcriptionID)
			return 0, ErrNoSuchTranscription
		}
		m.logger.Error("transcription load fail", "transcriptionID", transcriptionID, "err", err)
		return 0, err
	}

	dbWords, err := m.queries.GetWords(ctx, dbgen.GetWordsParams{
		TranscriptionID: transcriptionID,
		StartSeq:        0,
		WordCount:       1_000_000,
	})
	if err != nil {
		m.logger.Error("word load query failed", "transcriptionID", transcriptionID, "err", err)
		return 0, err
	}

	m.logger.Info("transcription words loaded", "transcriptionID", transcriptionID, "words", len(dbWords))

	words := make([]*qarauv1.Word, 0, len(dbWords))
	for i := range dbWords {
		w := &dbWords[i]
		if int32(i) != w.Seq {
			m.logger.Error("seq mismatch in the word loaded from DB", "transcriptionID", transcriptionID, "seq", w.Seq, "i", i)
			return 0, errors.New("word data corrupted")
		}
		words = append(words, &qarauv1.Word{
			Word:       w.Word,
			StartMs:    uint32(w.StartMs),
			EndMs:      uint32(w.EndMs),
			Confidence: uint32(w.Confidence),
			Speaker:    uint32(w.Speaker),
		})
	}

	sents := m.toSentences(words, transcriptionID)
	return m.upsertSentences(ctx, transcriptionID, sents)
}
