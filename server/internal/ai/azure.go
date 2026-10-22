package ai

import (
	"context"
	"errors"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"

	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// azureOpenAI is forma2's azure provider, the AI SDK's createAzure: the Responses API at <base>/v1/responses?api-version=<version>, where the base is AI_AZURE_BASE_URL (ending at /openai) or https://<resource>.openai.azure.com/openai, authenticated with the api-key header. AI_MODEL is the deployment name.
type azureOpenAI struct {
	responses responses.ResponseService
}

func newAzure(cfg config.AIConfig, e endpoints) provider {
	base := strings.TrimRight(normalize(cfg.AzureBaseURL), "/")
	if base == "" {
		base = "https://" + normalize(cfg.AzureResourceName) + ".openai.azure.com/openai"
	}
	version := normalize(cfg.AzureAPIVersion)
	if version == "" {
		version = "v1"
	}
	return &azureOpenAI{responses: responses.NewResponseService(
		option.WithBaseURL(base+"/v1/"),
		option.WithQuery("api-version", version),
		option.WithHeader("api-key", normalize(string(cfg.AzureAPIKey))),
		option.WithMaxRetries(e.maxRetries()),
	)}
}

// params is the AI SDK's Responses request: a system and a user message, max_output_tokens, and for an object a strict json_schema text format.
func (p *azureOpenAI) params(c call) responses.ResponseNewParams {
	var input responses.ResponseInputParam
	if c.system != "" {
		input = append(input, responses.ResponseInputItemParamOfMessage(c.system, responses.EasyInputMessageRoleSystem))
	}
	input = append(input, responses.ResponseInputItemParamOfMessage(responses.ResponseInputMessageContentListParam{responses.ResponseInputContentParamOfInputText(c.prompt)}, responses.EasyInputMessageRoleUser))
	params := responses.ResponseNewParams{Model: c.model, Input: responses.ResponseNewParamsInputUnion{OfInputItemList: input}}
	if c.maxOutputTokens > 0 {
		params.MaxOutputTokens = openai.Int(int64(c.maxOutputTokens))
	}
	if c.temperature != nil {
		params.Temperature = openai.Float(*c.temperature)
	}
	if c.format != nil {
		js := &responses.ResponseFormatTextJSONSchemaConfigParam{Name: c.format.name, Schema: c.format.schema, Strict: openai.Bool(true)}
		if c.format.description != "" {
			js.Description = openai.String(c.format.description)
		}
		params.Text = responses.ResponseTextConfigParam{Format: responses.ResponseFormatTextConfigUnionParam{OfJSONSchema: js}}
	}
	return params
}

func (p *azureOpenAI) generate(ctx context.Context, c call) (*completion, error) {
	res, err := p.responses.New(ctx, p.params(c))
	if err != nil {
		return nil, err
	}
	if res.Error.Message != "" {
		return nil, errors.New("azure: " + res.Error.Message)
	}
	return &completion{text: res.OutputText(), finishReason: responsesFinishReason(res.IncompleteDetails.Reason), usage: responsesUsage(res.Usage)}, nil
}

func (p *azureOpenAI) stream(ctx context.Context, c call, onDelta func(string)) (*completion, error) {
	s := p.responses.NewStreaming(ctx, p.params(c))
	defer func() { _ = s.Close() }()
	out := &completion{finishReason: FinishOther}
	text := ""
	for s.Next() {
		ev := s.Current()
		switch ev.Type {
		case "response.output_text.delta":
			text += ev.Delta
			onDelta(ev.Delta)
		case "response.completed", "response.incomplete":
			out.finishReason = responsesFinishReason(ev.Response.IncompleteDetails.Reason)
			out.usage = responsesUsage(ev.Response.Usage)
		case "response.failed":
			return nil, errors.New("azure: " + ev.Response.Error.Message)
		case "error":
			return nil, errors.New("azure: " + ev.Message)
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	out.text = text
	return out, nil
}

// responsesFinishReason is the AI SDK's mapOpenAIResponseFinishReason for a response without tool calls.
func responsesFinishReason(incomplete string) FinishReason {
	switch incomplete {
	case "":
		return FinishStop
	case "max_output_tokens":
		return FinishLength
	case "content_filter":
		return FinishContentFilter
	default:
		return FinishOther
	}
}

func responsesUsage(u responses.ResponseUsage) Usage {
	var out Usage
	if u.JSON.InputTokens.Valid() {
		out.InputTokens = &u.InputTokens
	}
	if u.JSON.OutputTokens.Valid() {
		out.OutputTokens = &u.OutputTokens
	}
	if u.OutputTokensDetails.JSON.ReasoningTokens.Valid() {
		out.ReasoningTokens = &u.OutputTokensDetails.ReasoningTokens
	}
	return out
}
