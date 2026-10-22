package ai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// jsonToolName is the tool the AI SDK's Bedrock provider forces the model to call when it asks for an object: Converse has no response format, so the object arrives as that tool's input.
const jsonToolName = "json"

// bedrock is forma2's aws provider: the Bedrock Converse API with the static credentials of AI_AWS_*, never the default credential chain.
type bedrock struct {
	client *bedrockruntime.Client
}

func newBedrock(cfg config.AIConfig, e endpoints) provider {
	creds := aws.Credentials{
		AccessKeyID:     normalize(cfg.AWSAccessKeyID),
		SecretAccessKey: normalize(string(cfg.AWSSecretAccessKey)),
		SessionToken:    normalize(string(cfg.AWSSessionToken)),
		Source:          "AI_AWS",
	}
	opts := bedrockruntime.Options{
		Region:           normalize(cfg.AWSRegion),
		Credentials:      aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) { return creds, nil }),
		RetryMaxAttempts: e.maxRetries() + 1,
	}
	if e.bedrock != "" {
		opts.BaseEndpoint = aws.String(e.bedrock)
	}
	return &bedrock{client: bedrockruntime.New(opts)}
}

type converseRequest struct {
	modelID   *string
	system    []types.SystemContentBlock
	messages  []types.Message
	inference *types.InferenceConfiguration
	tools     *types.ToolConfiguration
}

// request is the AI SDK's Bedrock command: the system prompt, one user message, maxTokens and temperature, and for an object the json tool with toolChoice any.
func (p *bedrock) request(c call) converseRequest {
	r := converseRequest{
		modelID:  aws.String(c.model),
		messages: []types.Message{{Role: types.ConversationRoleUser, Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: c.prompt}}}},
	}
	if c.system != "" {
		r.system = []types.SystemContentBlock{&types.SystemContentBlockMemberText{Value: c.system}}
	}
	inference := &types.InferenceConfiguration{}
	if c.maxOutputTokens > 0 {
		inference.MaxTokens = aws.Int32(int32(min(c.maxOutputTokens, 1<<30))) //nolint:gosec // clamped above
	}
	if c.temperature != nil {
		// Bedrock accepts 0 to 1; the AI SDK clamps.
		inference.Temperature = aws.Float32(float32(min(max(*c.temperature, 0), 1)))
	}
	if inference.MaxTokens != nil || inference.Temperature != nil {
		r.inference = inference
	}
	if c.format != nil {
		r.tools = &types.ToolConfiguration{
			Tools: []types.Tool{&types.ToolMemberToolSpec{Value: types.ToolSpecification{
				Name:        aws.String(jsonToolName),
				Description: aws.String("Respond with a JSON object."),
				InputSchema: &types.ToolInputSchemaMemberJson{Value: document.NewLazyDocument(c.format.schema)},
			}}},
			ToolChoice: &types.ToolChoiceMemberAny{Value: types.AnyToolChoice{}},
		}
	}
	return r
}

func (p *bedrock) generate(ctx context.Context, c call) (*completion, error) {
	r := p.request(c)
	res, err := p.client.Converse(ctx, &bedrockruntime.ConverseInput{ModelId: r.modelID, System: r.system, Messages: r.messages, InferenceConfig: r.inference, ToolConfig: r.tools})
	if err != nil {
		return nil, err
	}
	out := &completion{finishReason: bedrockFinishReason(res.StopReason, c.format != nil), usage: bedrockUsage(res.Usage)}
	msg, ok := res.Output.(*types.ConverseOutputMemberMessage)
	if !ok {
		return out, nil
	}
	for _, block := range msg.Value.Content {
		switch b := block.(type) {
		case *types.ContentBlockMemberText:
			out.text += b.Value
		case *types.ContentBlockMemberToolUse:
			if c.format == nil || aws.ToString(b.Value.Name) != jsonToolName || b.Value.Input == nil {
				continue
			}
			var input any
			if err := b.Value.Input.UnmarshalSmithyDocument(&input); err != nil {
				return nil, fmt.Errorf("bedrock: read the json tool input: %w", err)
			}
			encoded, err := json.Marshal(input)
			if err != nil {
				return nil, fmt.Errorf("bedrock: encode the json tool input: %w", err)
			}
			// The AI SDK answers the json tool's input as the generation's text.
			out.text = string(encoded)
		}
	}
	return out, nil
}

func (p *bedrock) stream(ctx context.Context, c call, onDelta func(string)) (*completion, error) {
	r := p.request(c)
	res, err := p.client.ConverseStream(ctx, &bedrockruntime.ConverseStreamInput{ModelId: r.modelID, System: r.system, Messages: r.messages, InferenceConfig: r.inference, ToolConfig: r.tools})
	if err != nil {
		return nil, err
	}
	s := res.GetStream()
	defer func() { _ = s.Close() }()
	out := &completion{finishReason: FinishOther}
	text := ""
	for event := range s.Events() {
		switch ev := event.(type) {
		case *types.ConverseStreamOutputMemberContentBlockDelta:
			switch d := ev.Value.Delta.(type) {
			case *types.ContentBlockDeltaMemberText:
				if c.format == nil {
					text += d.Value
					onDelta(d.Value)
				}
			case *types.ContentBlockDeltaMemberToolUse:
				// With the json tool forced, the tool's input is the object being written.
				if c.format != nil && d.Value.Input != nil {
					text += *d.Value.Input
					onDelta(*d.Value.Input)
				}
			}
		case *types.ConverseStreamOutputMemberMessageStop:
			out.finishReason = bedrockFinishReason(ev.Value.StopReason, c.format != nil)
		case *types.ConverseStreamOutputMemberMetadata:
			out.usage = bedrockUsage(ev.Value.Usage)
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	out.text = text
	return out, nil
}

// bedrockFinishReason is the AI SDK's mapBedrockFinishReason: a tool_use stop is the end of the answer when the tool is the json response tool.
func bedrockFinishReason(reason types.StopReason, jsonTool bool) FinishReason {
	switch reason {
	case types.StopReasonEndTurn, types.StopReasonStopSequence:
		return FinishStop
	case types.StopReasonMaxTokens:
		return FinishLength
	case types.StopReasonContentFiltered, types.StopReasonGuardrailIntervened:
		return FinishContentFilter
	case types.StopReasonToolUse:
		if jsonTool {
			return FinishStop
		}
		return FinishToolCalls
	default:
		return FinishOther
	}
}

func bedrockUsage(u *types.TokenUsage) Usage {
	var out Usage
	if u == nil {
		return out
	}
	if u.InputTokens != nil {
		n := int64(*u.InputTokens)
		out.InputTokens = &n
	}
	if u.OutputTokens != nil {
		n := int64(*u.OutputTokens)
		out.OutputTokens = &n
	}
	return out
}
