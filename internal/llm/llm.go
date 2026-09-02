package llm

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/invopop/jsonschema"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
)

const (
	placeholderClipTitle    = "{{CLIP_TITLE}}"
	placeholderChannelTitle = "{{CHANNEL_TITLE}}"
	placeholderSubtitles    = "{{SUBTITLES}}"

	LangRu            = "ru"
	PromptVersion     = 1
	modelForBreakdown = openai.ChatModelGPT5_6Luna
	maxOutputTokens   = 32768
	promptAttempts    = 3
	promptTimeout     = 3 * time.Minute
)

type OaiClient struct {
	logger     *slog.Logger
	client     openai.Client
	schemaRuV1 map[string]any
}

func generateSchema[T any]() (map[string]any, error) {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}
	var v T
	schema := reflector.Reflect(v)

	data, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal schema for LLM request: %w", err)
	}
	var result map[string]any
	if err = json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal schema for LLM request: %w", err)
	}
	return result, nil
}

func New(logger *slog.Logger, apiKey string) (*OaiClient, error) {
	if apiKey == "" {
		return nil, errors.New("empty apiKey in OaiClient creation")
	}
	schemaRuV1, err := generateSchema[ResponseRuV1]()
	if err != nil {
		return nil, fmt.Errorf("failed to generate schema for OaiClient: %w", err)
	}
	return &OaiClient{
		logger:     logger,
		client:     openai.NewClient(option.WithAPIKey(apiKey)),
		schemaRuV1: schemaRuV1,
	}, nil
}

//go:embed prompts/*.md
var promptFiles embed.FS

func promptTemplateName(name string, targetLang string, ver int) string {
	return name + "." + targetLang + ".v" + strconv.Itoa(ver) + ".md"
}

func (c OaiClient) buildPrompt(
	name string,
	targetLang string,
	ver int,
	title, channel string,
	sentences []string,
) (string, error) {
	promptName := promptTemplateName(name, targetLang, ver)
	template, err := promptFiles.ReadFile(path.Join("prompts", promptName))
	if err != nil {
		c.logger.Error(
			"failed to load prompt template",
			"name", name,
			"ver", ver,
			"err", err,
		)
		return "", fmt.Errorf("prompt template load fail: %w", err)
	}
	r1 := strings.ReplaceAll(
		strings.TrimSpace(string(template)),
		placeholderClipTitle,
		title,
	)
	r2 := strings.ReplaceAll(r1, placeholderChannelTitle, channel)

	numbered := make([]string, 0, len(sentences))
	for i, sent := range sentences {
		numbered = append(numbered, strconv.Itoa(i+1)+". "+sent)
	}

	prompt := strings.ReplaceAll(r2, placeholderSubtitles, strings.Join(numbered, "\n"))

	c.logger.Info(
		"prompt generated",
		"prompt", prompt,
	)
	return prompt, nil
}

type WordRuV1 struct {
	Word            string `json:"word" jsonschema_description:"The word or phrase from the sentence."`
	PartOfSpeech    string `json:"pos" jsonschema_description:"Part of speech of the word or phrase."`
	Base            string `json:"base" jsonschema_description:"Base or dictionary form of the word or phrase."`
	BaseTranslation string `json:"base_translation" jsonschema_description:"Russian translation of the base form."`
	WordTranslation string `json:"word_translation" jsonschema_description:"Russian translation of the word or phrase"`
	Comment         string `json:"comment" jsonschema_description:"Optional explanatory comment for the grammar form, including suffixes and their roles."`
}

type SentenceBreakdownRuV1 struct {
	SentenceNum  int        `json:"sentence_num" jsonschema_description:"The sentence number taken from input"`
	Sentence     string     `json:"sentence" jsonschema_description:"The original sentence."`
	Translations []string   `json:"translations" jsonschema_description:"Russian translation of the sentence."`
	Breakdown    []WordRuV1 `json:"breakdown" jsonschema_description:"A breakdown of the sentence into words or phrases with linguistic details."`
}

type WordGenericV1 struct {
	Word            string
	PartOfSpeech    string
	Base            string
	BaseTranslation string
	WordTranslation string
	Comment         string
}

type SentenceBreakdownGenericV1 struct {
	Sentence     string
	Translations []string
	Breakdown    []WordGenericV1
}

type ResponseRuV1 struct {
	Sentences []SentenceBreakdownRuV1 `json:"sentences" jsonschema_description:"A list of sentences with translations and linguistic breakdown."`
}

type TokenUsage struct {
	Input  int64
	Output int64
}

// Put breakdowns in the right positions according to sentence_num.
// If some position is empty, then its 'text' should be left empty.
func (c OaiClient) arrangeAndConvert(sentenceCount int, sents []SentenceBreakdownRuV1) ([]SentenceBreakdownGenericV1, error) {
	result := make([]SentenceBreakdownGenericV1, sentenceCount)
	vacant := sentenceCount
	for _, sent := range sents {
		pos := sent.SentenceNum - 1
		if pos < 0 || pos >= sentenceCount {
			c.logger.Warn("sentence pos in LLM output is out of range", "pos", pos, "sentenceCount", sentenceCount)
			continue
		}
		if result[pos].Sentence != "" {
			c.logger.Warn("duplicate sentence in LLM output", "pos", pos, "prev", result[pos].Sentence, "cur", sent.Sentence)
		} else {
			vacant--
		}
		result[pos] = SentenceBreakdownGenericV1{
			Sentence:     sent.Sentence,
			Translations: sent.Translations,
			Breakdown:    wordRuV1ToGeneric(sent.Breakdown),
		}
	}
	if vacant > 0 {
		if vacant*2 > sentenceCount {
			c.logger.Error("too many sentences got no breakdown from LLM", "vacant", vacant, "sentenceCount", sentenceCount)
			return nil, errors.New("too many missing breakdowns from LLM")
		} else {
			c.logger.Warn("some sentences got no breakdown from LLM", "vacant", vacant, "sentenceCount", sentenceCount)
		}

	}
	return result, nil
}

func (c OaiClient) parseLlm(
	sentenceCount int,
	response *responses.Response,
) ([]SentenceBreakdownGenericV1, error) {
	if response == nil {
		return nil, errors.New("nil LLM response")
	}
	var sentenceBreakdowns ResponseRuV1
	err := json.Unmarshal([]byte(response.OutputText()), &sentenceBreakdowns)
	if err != nil {
		c.logger.Error("llm response unmarshalling fail", "err", err, "size", len(response.OutputText()))
		return nil, err
	}
	generic, err := c.arrangeAndConvert(sentenceCount, sentenceBreakdowns.Sentences)
	if err != nil {
		c.logger.Error("llm response conversion fail", "err", err)
		return nil, err
	}
	return generic, nil
}

func (c OaiClient) requestWithRetriesV1(
	ctx context.Context,
	sentenceCount int,
	body responses.ResponseNewParams,
	attempts int,
) ([]SentenceBreakdownGenericV1, TokenUsage, error) {
	var savedErr error
	usage := TokenUsage{}
	requestCtx, cancel := context.WithTimeout(ctx, promptTimeout)
	defer cancel()

	for attempt := range attempts {
		savedErr = nil
		promptStart := time.Now()

		response, err := c.client.Responses.New(requestCtx, body, option.WithMaxRetries(0))
		promptTime := fmt.Sprintf("%.3f", time.Since(promptStart).Seconds())
		if response != nil {
			c.logger.Info(
				"prompt response - token usage",
				"input", response.Usage.InputTokens,
				"output", response.Usage.OutputTokens,
				"time", promptTime,
			)
			usage.Input += response.Usage.InputTokens
			usage.Output += response.Usage.OutputTokens
		}
		if err != nil {
			savedErr = err
			c.logger.Error(
				"LLM prompt attempt fail",
				"attempt", attempt+1,
				"attempts", attempts,
				"time", promptTime,
				"err", err,
			)
			if attempt+1 < attempts {
				delayMillis := time.Duration(1000*(2<<attempt) + rand.Int()%1000)
				delay := time.Millisecond * delayMillis
				c.logger.Info(
					"LLM prompt will retry",
					"delayMillis", delay.Milliseconds(),
				)
				select {
				case <-requestCtx.Done():
					return nil, usage, requestCtx.Err()
				case <-time.After(delay):
				}
				continue
			} else {
				break
			}
		}
		converted, err := c.parseLlm(sentenceCount, response)
		if err != nil {
			savedErr = err
			c.logger.Error(
				"LLM response parsing failed",
				"attempt", attempt+1,
				"attempts", attempts,
				"err", err,
			)
			continue
		}
		return converted, usage, nil
	}
	if savedErr != nil {
		return nil, usage, fmt.Errorf(
			"llm prompt fail after %d attempts: %w", attempts, savedErr,
		)
	}
	return nil, usage, fmt.Errorf(
		"llm request fail after %d attempts", attempts,
	)
}

func (c OaiClient) doSentenceBreakdownRuV1(ctx context.Context, title, channel string, sentences []string) ([]SentenceBreakdownGenericV1, TokenUsage, error) {
	usage := TokenUsage{}
	prompt, err := c.buildPrompt("grammar_breakdown", LangRu, PromptVersion, title, channel, sentences)
	if err != nil {
		return nil, usage, fmt.Errorf("sentence breakdown fail: %w", err)
	}
	if len(prompt) == 0 {
		return nil, usage, errors.New("empty prompt")
	}

	model := modelForBreakdown

	c.logger.Info("prompt to llm for ru breakdown", "model", model, "prompt_size", len(prompt), "sents", len(sentences))

	format := responses.ResponseFormatTextConfigParamOfJSONSchema(
		"schema_ru_v1",
		c.schemaRuV1,
	)
	format.OfJSONSchema.Strict = param.NewOpt(true)

	body := responses.ResponseNewParams{
		Model:           model,
		MaxOutputTokens: param.NewOpt[int64](maxOutputTokens),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String(prompt),
		},
		Text: responses.ResponseTextConfigParam{
			Format: format,
		},
	}

	return c.requestWithRetriesV1(ctx, len(sentences), body, promptAttempts)
}

func wordRuV1ToGeneric(words []WordRuV1) []WordGenericV1 {
	result := make([]WordGenericV1, 0, len(words))
	for _, src := range words {
		result = append(result, WordGenericV1{
			Word:            src.Word,
			PartOfSpeech:    src.PartOfSpeech,
			Base:            src.Base,
			BaseTranslation: src.BaseTranslation,
			WordTranslation: src.WordTranslation,
			Comment:         src.Comment,
		})
	}
	return result
}

type BreakdownMetadata struct {
	Model         string
	PromptVersion int
}

var (
	curBreakdownMetadata = BreakdownMetadata{
		Model:         modelForBreakdown,
		PromptVersion: PromptVersion,
	}
	zeroUsage = TokenUsage{}
)

type SentenceBreakdownResult struct {
	Metadata  BreakdownMetadata
	Usage     TokenUsage
	Sentences []SentenceBreakdownGenericV1
}

func (c OaiClient) DoSentenceBreakdown(ctx context.Context, targetLang, title, channel string, sentences []string) (SentenceBreakdownResult, error) {
	if targetLang == LangRu {
		generic, usage, err := c.doSentenceBreakdownRuV1(ctx, title, channel, sentences)
		if err != nil {
			return SentenceBreakdownResult{Usage: usage}, err
		}
		return SentenceBreakdownResult{curBreakdownMetadata, usage, generic}, nil
	} else {
		return SentenceBreakdownResult{Usage: zeroUsage}, fmt.Errorf("sentence breakdown is not implemented for targetLang %s", targetLang)
	}
}
