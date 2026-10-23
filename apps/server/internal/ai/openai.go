package ai

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"

	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// defaultRetries is the AI SDK's maxRetries: a failed call is retried twice before its error reaches the caller.
const defaultRetries = 2

func (e endpoints) maxRetries() int {
	if e.retries != nil {
		return *e.retries
	}
	return defaultRetries
}

// openAICompatible is forma2's openai-compatible provider: the chat completions API at AI_OPENAI_COMPATIBLE_BASE_URL. The base URL is the operator's, so a private address such as a bundled vLLM is reachable on purpose.
type openAICompatible struct {
	chat              openai.ChatCompletionService
	structuredOutputs bool
}

func newOpenAICompatible(cfg config.AIConfig, e endpoints) (provider, error) {
	opts := []option.RequestOption{option.WithBaseURL(normalize(cfg.OpenAICompatibleBaseURL)), option.WithMaxRetries(e.maxRetries())}
	if key := normalize(string(cfg.OpenAICompatibleAPIKey)); key != "" {
		opts = append(opts, option.WithAPIKey(key))
	}
	if raw := normalize(string(cfg.OpenAICompatibleHeadersJSON)); raw != "" {
		headers, err := config.ParseStringRecord(raw)
		if err != nil {
			return nil, &ConfigurationError{Code: StatusProviderNotConfigured, Message: "AI_OPENAI_COMPATIBLE_HEADERS_JSON must be a JSON object of string values", Provider: config.AIProviderOpenAICompatible, InvalidFields: []string{"AI_OPENAI_COMPATIBLE_HEADERS_JSON"}}
		}
		for _, k := range slices.Sorted(maps.Keys(headers)) {
			opts = append(opts, option.WithHeader(k, headers[k]))
		}
	}
	if raw := normalize(string(cfg.OpenAICompatibleQueryParamsJSON)); raw != "" {
		params, err := config.ParseStringRecord(raw)
		if err != nil {
			return nil, &ConfigurationError{Code: StatusProviderNotConfigured, Message: "AI_OPENAI_COMPATIBLE_QUERY_PARAMS_JSON must be a JSON object of string values", Provider: config.AIProviderOpenAICompatible, InvalidFields: []string{"AI_OPENAI_COMPATIBLE_QUERY_PARAMS_JSON"}}
		}
		for _, k := range slices.Sorted(maps.Keys(params)) {
			opts = append(opts, option.WithQuery(k, params[k]))
		}
	}
	// The service is built without openai.NewClient on purpose: the client would read OPENAI_API_KEY and its siblings from the environment and send them to the operator's endpoint.
	return &openAICompatible{chat: openai.NewChatCompletionService(opts...), structuredOutputs: supportsStructuredOutputs(cfg.OpenAICompatibleSupportsStructuredOutputs)}, nil
}

// params is the AI SDK's openai-compatible request: system and user messages, max_tokens, and for an object either a strict json_schema response format or, when the endpoint does not support structured outputs, json_object.
func (p *openAICompatible) params(c call) openai.ChatCompletionNewParams {
	var messages []openai.ChatCompletionMessageParamUnion
	if c.system != "" {
		messages = append(messages, openai.SystemMessage(c.system))
	}
	messages = append(messages, openai.UserMessage(c.prompt))
	params := openai.ChatCompletionNewParams{Model: c.model, Messages: messages}
	if c.maxOutputTokens > 0 {
		params.MaxTokens = openai.Int(int64(c.maxOutputTokens))
	}
	if c.temperature != nil {
		params.Temperature = openai.Float(*c.temperature)
	}
	if c.format != nil {
		if p.structuredOutputs {
			js := shared.ResponseFormatJSONSchemaJSONSchemaParam{Name: c.format.name, Strict: openai.Bool(true), Schema: c.format.schema}
			if c.format.description != "" {
				js.Description = openai.String(c.format.description)
			}
			params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{JSONSchema: js}}
		} else {
			params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{OfJSONObject: &shared.ResponseFormatJSONObjectParam{}}
		}
	}
	return params
}

func (p *openAICompatible) generate(ctx context.Context, c call) (*completion, error) {
	res, err := p.chat.New(ctx, p.params(c))
	if err != nil {
		return nil, err
	}
	if len(res.Choices) == 0 {
		return nil, fmt.Errorf("openai-compatible: the response has no choices")
	}
	choice := res.Choices[0]
	return &completion{text: choice.Message.Content, finishReason: chatFinishReason(choice.FinishReason), usage: chatUsage(res.Usage)}, nil
}

func (p *openAICompatible) stream(ctx context.Context, c call, onDelta func(string)) (*completion, error) {
	s := p.chat.NewStreaming(ctx, p.params(c))
	defer func() { _ = s.Close() }()
	out := &completion{finishReason: FinishOther}
	text := ""
	for s.Next() {
		chunk := s.Current()
		if chunk.JSON.Usage.Valid() {
			out.usage = chatUsage(chunk.Usage)
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if choice.FinishReason != "" {
			out.finishReason = chatFinishReason(choice.FinishReason)
		}
		if d := choice.Delta.Content; d != "" {
			text += d
			onDelta(d)
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	out.text = text
	return out, nil
}

// chatFinishReason is the AI SDK's mapOpenAICompatibleFinishReason.
func chatFinishReason(reason string) FinishReason {
	switch reason {
	case "stop":
		return FinishStop
	case "length":
		return FinishLength
	case "content_filter":
		return FinishContentFilter
	case "function_call", "tool_calls":
		return FinishToolCalls
	default:
		return FinishOther
	}
}

func chatUsage(u openai.CompletionUsage) Usage {
	var out Usage
	if u.JSON.PromptTokens.Valid() {
		out.InputTokens = &u.PromptTokens
	}
	if u.JSON.CompletionTokens.Valid() {
		out.OutputTokens = &u.CompletionTokens
	}
	if u.CompletionTokensDetails.JSON.ReasoningTokens.Valid() {
		out.ReasoningTokens = &u.CompletionTokensDetails.ReasoningTokens
	}
	return out
}
