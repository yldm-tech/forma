// Package ai is forma2's packages/ai and apps/web/lib/ai: the instance's one AI provider (Amazon Bedrock, Google Vertex AI, Azure OpenAI or an OpenAI-compatible endpoint), configured by forma2's AI_* variables, with text generation and schema-bound object generation, blocking or streamed; and on top of it the organization-scoped service every AI feature goes through, which checks the organization's AI smart tools toggle, maps a provider's 429 to a quota error and records what each generation cost.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// FinishReason is why a generation stopped, the AI SDK's FinishReason.
type FinishReason string

const (
	FinishStop          FinishReason = "stop"
	FinishLength        FinishReason = "length"
	FinishContentFilter FinishReason = "content-filter"
	FinishToolCalls     FinishReason = "tool-calls"
	FinishError         FinishReason = "error"
	FinishOther         FinishReason = "other"
)

// Usage is what a generation was billed, each count as the provider reported it (nil when it did not).
type Usage struct {
	InputTokens     *int64
	OutputTokens    *int64
	ReasoningTokens *int64
}

// Request is one generation.
type Request struct {
	System string
	Prompt string
	// Temperature is left to the provider when nil.
	Temperature     *float64
	MaxOutputTokens int
	// Timeout bounds the whole call, retries included; zero means none beyond the context's.
	Timeout time.Duration
}

// ObjectRequest is a generation whose answer must be a JSON value of Schema, forma2's generateObject options.
type ObjectRequest struct {
	Request
	// Schema is a JSON Schema (draft 7) document; the answer is validated against it.
	Schema            json.RawMessage
	SchemaName        string
	SchemaDescription string
	// Check, when set, runs on an answer that matches Schema and refuses it with an error: the rules a JSON Schema cannot state, which forma2's zod schemas carried as refinements.
	Check func(object json.RawMessage) error
}

// TextResult is a finished text generation.
type TextResult struct {
	Text         string
	FinishReason FinishReason
	Usage        Usage
	Model        string
}

// ObjectResult is a finished object generation.
type ObjectResult struct {
	Object       json.RawMessage
	FinishReason FinishReason
	Usage        Usage
	Model        string
}

// call is what a provider sends: the resolved model and the request, with the response format when an object is asked for.
type call struct {
	model           string
	system          string
	prompt          string
	temperature     *float64
	maxOutputTokens int
	format          *jsonFormat
}

type jsonFormat struct {
	name        string
	description string
	schema      map[string]any
}

// completion is what a provider answered.
type completion struct {
	text         string
	finishReason FinishReason
	usage        Usage
}

// provider is one AI provider's wire protocol. stream calls onDelta with each piece of text as it arrives and answers like generate once the generation has finished.
type provider interface {
	generate(ctx context.Context, c call) (*completion, error)
	stream(ctx context.Context, c call, onDelta func(string)) (*completion, error)
}

// Feature names the AI feature a generation serves, forma2's AI tracing feature ids. A feature with its own model variable runs on that model.
type Feature string

const (
	FeatureSurveyGeneration Feature = "ai_survey_generation"
	FeatureChartQuery       Feature = "ai_chart_query"
	FeatureTranslation      Feature = "ai_translation"
	FeatureExampleResponses Feature = "ai_example_responses"
)

// endpoints overrides where providers send their requests; production leaves it empty and tests point it at servers that speak each provider's protocol.
type endpoints struct {
	bedrock  string
	vertex   string
	vertexV1 bool
	retries  *int
}

// Client is forma2's @forma/ai: the configured provider, resolved once per model and kept for the life of the process.
type Client struct {
	cfg       config.AIConfig
	status    Status
	log       *slog.Logger
	endpoints endpoints

	mu        sync.Mutex
	providers map[string]provider
}

// NewClient builds the client for the configuration the process started with. Providers are created on first use, so a provider that cannot be reached does not stop the server from starting.
func NewClient(cfg config.Config, log *slog.Logger) *Client {
	return &Client{cfg: cfg.AI, status: ConfigurationStatus(cfg.AI), log: log, providers: map[string]provider{}}
}

// Status is the instance's AI configuration status.
func (c *Client) Status() Status { return c.status }

// Configured is forma2's isAiConfigured.
func (c *Client) Configured() bool { return c.status.IsConfigured }

// Model is the model a feature runs on: its own variable when the operator set one, else AI_MODEL. forma2 routed only translation and example responses.
func (c *Client) Model(feature Feature) string {
	switch feature {
	case FeatureTranslation:
		if m := normalize(c.cfg.ModelTranslation); m != "" {
			return m
		}
	case FeatureExampleResponses:
		if m := normalize(c.cfg.ModelExampleResponses); m != "" {
			return m
		}
	}
	return c.status.Model
}

// provider resolves the configured provider, forma2's getAiModel: a configuration that cannot serve is a ConfigurationError.
func (c *Client) provider(ctx context.Context) (provider, error) {
	s := c.status
	if !s.IsConfigured {
		code := s.ErrorCode
		if code == "" {
			code = StatusProviderNotConfigured
		}
		return nil, &ConfigurationError{Code: code, Message: configurationMessage(s), Provider: s.Provider, Model: s.Model, MissingFields: s.MissingFields, InvalidFields: s.InvalidFields}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if p, ok := c.providers[s.Provider]; ok {
		return p, nil
	}
	p, err := c.newProvider(ctx, s.Provider)
	if err != nil {
		return nil, err
	}
	c.providers[s.Provider] = p
	return p, nil
}

func (c *Client) newProvider(ctx context.Context, name string) (provider, error) {
	switch name {
	case config.AIProviderAWS:
		return newBedrock(c.cfg, c.endpoints), nil
	case config.AIProviderGoogle:
		return newVertex(ctx, c.cfg, c.endpoints)
	case config.AIProviderAzure:
		return newAzure(c.cfg, c.endpoints), nil
	default:
		return newOpenAICompatible(c.cfg, c.endpoints)
	}
}

func (r Request) call(model string) call {
	return call{model: model, system: r.System, prompt: r.Prompt, temperature: r.Temperature, maxOutputTokens: r.MaxOutputTokens}
}

func withTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, d)
}

// GenerateText is forma2's generateText.
func (c *Client) GenerateText(ctx context.Context, feature Feature, req Request) (*TextResult, error) {
	p, err := c.provider(ctx)
	if err != nil {
		return nil, err
	}
	ctx, cancel := withTimeout(ctx, req.Timeout)
	defer cancel()
	model := c.Model(feature)
	out, err := p.generate(ctx, req.call(model))
	if err != nil {
		return nil, err
	}
	return &TextResult{Text: out.text, FinishReason: out.finishReason, Usage: out.usage, Model: model}, nil
}

// GenerateObject is forma2's generateObject: the answer must be a JSON value of the request's schema. Stopping at the output token limit is an OutputTokenLimitError, any other unusable answer a NoObjectError.
func (c *Client) GenerateObject(ctx context.Context, feature Feature, req ObjectRequest) (*ObjectResult, error) {
	return c.object(ctx, feature, req, nil)
}

// StreamObject is forma2's streamObject: onPartial receives each new snapshot of the object as the model writes it, whole rather than a delta and unvalidated, so it is for display only; the result is the finished object, held to the same rules as GenerateObject.
func (c *Client) StreamObject(ctx context.Context, feature Feature, req ObjectRequest, onPartial func(partial any)) (*ObjectResult, error) {
	return c.object(ctx, feature, req, onPartial)
}

func (c *Client) object(ctx context.Context, feature Feature, req ObjectRequest, onPartial func(any)) (*ObjectResult, error) {
	p, err := c.provider(ctx)
	if err != nil {
		return nil, err
	}
	schema, validator, err := compileSchema(req.Schema)
	if err != nil {
		return nil, err
	}
	ctx, cancel := withTimeout(ctx, req.Timeout)
	defer cancel()
	model := c.Model(feature)
	cl := req.call(model)
	name := req.SchemaName
	if name == "" {
		name = "response"
	}
	cl.format = &jsonFormat{name: name, description: req.SchemaDescription, schema: schema}

	var out *completion
	if onPartial == nil {
		out, err = p.generate(ctx, cl)
	} else {
		var text string
		last := ""
		out, err = p.stream(ctx, cl, func(delta string) {
			text += delta
			v, ok := parsePartialJSON(text)
			if !ok {
				return
			}
			// Only a snapshot that differs from the last one goes out, as in the AI SDK.
			encoded, _ := json.Marshal(v)
			if string(encoded) == last {
				return
			}
			last = string(encoded)
			onPartial(v)
		})
	}
	if err != nil {
		return nil, err
	}
	if out.finishReason == FinishLength {
		return nil, &OutputTokenLimitError{MaxOutputTokens: req.MaxOutputTokens, OutputTokens: out.usage.OutputTokens, ReasoningTokens: out.usage.ReasoningTokens}
	}
	if out.finishReason != FinishStop {
		return nil, &NoObjectError{FinishReason: out.finishReason}
	}
	object, err := parseObject(out.text, validator, req.Check)
	if err != nil {
		return nil, &NoObjectError{FinishReason: out.finishReason, Cause: err}
	}
	return &ObjectResult{Object: object, FinishReason: out.finishReason, Usage: out.usage, Model: model}, nil
}

// parseObject is the AI SDK's parseCompleteOutput for an object: the text must parse as JSON and match the schema, then pass the request's own check.
func parseObject(text string, validator *jsonschema.Schema, check func(json.RawMessage) error) (json.RawMessage, error) {
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader([]byte(text)))
	if err != nil {
		return nil, fmt.Errorf("could not parse the response: %w", err)
	}
	if err := validator.Validate(v); err != nil {
		return nil, errors.New("response did not match schema")
	}
	raw := json.RawMessage(text)
	if check != nil {
		if err := check(raw); err != nil {
			return nil, fmt.Errorf("response did not match schema: %w", err)
		}
	}
	return raw, nil
}

// compileSchema decodes a request schema and compiles its validator.
func compileSchema(raw json.RawMessage) (map[string]any, *jsonschema.Schema, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, nil, fmt.Errorf("decode output schema: %w", err)
	}
	parsed, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, nil, fmt.Errorf("decode output schema: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("output.json", parsed); err != nil {
		return nil, nil, fmt.Errorf("compile output schema: %w", err)
	}
	s, err := compiler.Compile("output.json")
	if err != nil {
		return nil, nil, fmt.Errorf("compile output schema: %w", err)
	}
	return doc, s, nil
}
