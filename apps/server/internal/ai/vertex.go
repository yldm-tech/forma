package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"cloud.google.com/go/auth"
	"cloud.google.com/go/auth/credentials"
	"google.golang.org/genai"

	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// vertex is forma2's google provider, the AI SDK's createVertex: Gemini on Vertex AI in AI_GOOGLE_CLOUD_PROJECT and AI_GOOGLE_CLOUD_LOCATION, authenticated with AI_GOOGLE_CLOUD_CREDENTIALS_JSON, else the credentials file at AI_GOOGLE_CLOUD_APPLICATION_CREDENTIALS, else Application Default Credentials.
type vertex struct {
	client *genai.Client
}

// vertexMultiRegions are the locations forma2 sent to a multi-region host on the v1 API.
var vertexMultiRegions = map[string]bool{"us": true, "eu": true}

func newVertex(ctx context.Context, cfg config.AIConfig, e endpoints) (provider, error) {
	project, location := normalize(cfg.GoogleCloudProject), normalize(cfg.GoogleCloudLocation)
	creds, err := vertexCredentials(cfg)
	if err != nil {
		return nil, err
	}
	// The AI SDK's base URLs: the regional host (or the global one) on v1beta1, and for us and eu the multi-region host on v1.
	opts := genai.HTTPOptions{APIVersion: "v1beta1"}
	switch {
	case vertexMultiRegions[location]:
		opts.BaseURL, opts.APIVersion = "https://aiplatform."+location+".rep.googleapis.com/", "v1"
	case location == "global":
		opts.BaseURL = "https://aiplatform.googleapis.com/"
	default:
		opts.BaseURL = "https://" + location + "-aiplatform.googleapis.com/"
	}
	if e.vertex != "" {
		opts.BaseURL = e.vertex
	}
	attempts := int32(e.maxRetries() + 1) //nolint:gosec // a small constant
	opts.RetryOptions = &genai.HTTPRetryOptions{Attempts: &attempts}
	client, err := genai.NewClient(ctx, &genai.ClientConfig{Backend: genai.BackendVertexAI, Project: project, Location: location, Credentials: creds, HTTPOptions: opts})
	if err != nil {
		return nil, fmt.Errorf("vertex: create client: %w", err)
	}
	return &vertex{client: client}, nil
}

var cloudPlatformScope = []string{"https://www.googleapis.com/auth/cloud-platform"}

// vertexCredentials is forma2's credential precedence; nil leaves the genai SDK to find Application Default Credentials.
func vertexCredentials(cfg config.AIConfig) (*auth.Credentials, error) {
	var raw []byte
	if j := normalize(string(cfg.GoogleCloudCredentialsJSON)); j != "" {
		if !config.IsJSONObject(j) {
			return nil, &ConfigurationError{Code: StatusProviderNotConfigured, Message: "AI_GOOGLE_CLOUD_CREDENTIALS_JSON must be a valid JSON object", Provider: config.AIProviderGoogle, InvalidFields: []string{"AI_GOOGLE_CLOUD_CREDENTIALS_JSON"}}
		}
		raw = []byte(j)
	} else if path := normalize(cfg.GoogleCloudApplicationCredentials); path != "" {
		b, err := os.ReadFile(path) //nolint:gosec // the operator's own credentials file
		if err != nil {
			return nil, fmt.Errorf("vertex: read AI_GOOGLE_CLOUD_APPLICATION_CREDENTIALS: %w", err)
		}
		raw = b
	} else {
		return nil, nil
	}
	// google-auth-library read the credential type from the file; the Go library wants it named.
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, fmt.Errorf("vertex: read the credentials type: %w", err)
	}
	creds, err := credentials.NewCredentialsFromJSON(credentials.CredType(head.Type), raw, &credentials.DetectOptions{Scopes: cloudPlatformScope})
	if err != nil {
		return nil, fmt.Errorf("vertex: load credentials: %w", err)
	}
	return creds, nil
}

// config is the AI SDK's Google request: the system instruction, temperature and maxOutputTokens, and for an object application/json with the schema in Google's OpenAPI dialect.
func (p *vertex) config(c call) *genai.GenerateContentConfig {
	gc := &genai.GenerateContentConfig{}
	if c.system != "" {
		gc.SystemInstruction = &genai.Content{Parts: []*genai.Part{{Text: c.system}}}
	}
	if c.temperature != nil {
		t := float32(*c.temperature)
		gc.Temperature = &t
	}
	if c.maxOutputTokens > 0 {
		gc.MaxOutputTokens = int32(min(c.maxOutputTokens, 1<<30)) //nolint:gosec // clamped
	}
	if c.format != nil {
		gc.ResponseMIMEType = "application/json"
		gc.ResponseSchema = openAPISchema(c.format.schema, true)
	}
	return gc
}

func (p *vertex) generate(ctx context.Context, c call) (*completion, error) {
	res, err := p.client.Models.GenerateContent(ctx, c.model, []*genai.Content{genai.NewContentFromText(c.prompt, genai.RoleUser)}, p.config(c))
	if err != nil {
		return nil, err
	}
	out := &completion{finishReason: FinishOther, usage: vertexUsage(res.UsageMetadata)}
	if len(res.Candidates) > 0 {
		out.text = candidateText(res.Candidates[0])
		out.finishReason = vertexFinishReason(res.Candidates[0].FinishReason)
	}
	return out, nil
}

func (p *vertex) stream(ctx context.Context, c call, onDelta func(string)) (*completion, error) {
	out := &completion{finishReason: FinishOther}
	text := ""
	for res, err := range p.client.Models.GenerateContentStream(ctx, c.model, []*genai.Content{genai.NewContentFromText(c.prompt, genai.RoleUser)}, p.config(c)) {
		if err != nil {
			return nil, err
		}
		if res.UsageMetadata != nil {
			out.usage = vertexUsage(res.UsageMetadata)
		}
		if len(res.Candidates) == 0 {
			continue
		}
		if d := candidateText(res.Candidates[0]); d != "" {
			text += d
			onDelta(d)
		}
		if r := res.Candidates[0].FinishReason; r != "" {
			out.finishReason = vertexFinishReason(r)
		}
	}
	out.text = text
	return out, nil
}

// candidateText is the candidate's answer: its text parts without the model's thoughts.
func candidateText(c *genai.Candidate) string {
	if c.Content == nil {
		return ""
	}
	var b strings.Builder
	for _, part := range c.Content.Parts {
		if part.Text != "" && !part.Thought {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

// vertexFinishReason is the AI SDK's mapGoogleGenerativeAIFinishReason for an answer without tool calls.
func vertexFinishReason(r genai.FinishReason) FinishReason {
	switch r {
	case genai.FinishReasonStop:
		return FinishStop
	case genai.FinishReasonMaxTokens:
		return FinishLength
	case genai.FinishReasonImageSafety, genai.FinishReasonRecitation, genai.FinishReasonSafety, genai.FinishReasonBlocklist, genai.FinishReasonProhibitedContent, genai.FinishReasonSPII:
		return FinishContentFilter
	case genai.FinishReasonMalformedFunctionCall:
		return FinishError
	default:
		return FinishOther
	}
}

// vertexUsage is the AI SDK's Google usage: output tokens include the thoughts, which are also the reasoning tokens.
func vertexUsage(u *genai.GenerateContentResponseUsageMetadata) Usage {
	var out Usage
	if u == nil {
		return out
	}
	in := int64(u.PromptTokenCount)
	output := int64(u.CandidatesTokenCount) + int64(u.ThoughtsTokenCount)
	out.InputTokens, out.OutputTokens = &in, &output
	if u.ThoughtsTokenCount > 0 {
		reasoning := int64(u.ThoughtsTokenCount)
		out.ReasoningTokens = &reasoning
	}
	return out
}

// openAPISchema is the AI SDK's convertJSONSchemaToOpenAPISchema: Google's schema dialect has no type unions, so a union with null becomes nullable, and it drops what Google does not accept (additionalProperties, $schema, length and item bounds other than minLength).
func openAPISchema(node map[string]any, root bool) *genai.Schema {
	if node == nil {
		return nil
	}
	if t, _ := node["type"].(string); t == "object" {
		props, _ := node["properties"].(map[string]any)
		if len(props) == 0 && node["additionalProperties"] == nil {
			if root {
				return nil
			}
			s := &genai.Schema{Type: genai.TypeObject}
			if d, ok := node["description"].(string); ok {
				s.Description = d
			}
			return s
		}
	}
	s := &genai.Schema{}
	if d, ok := node["description"].(string); ok {
		s.Description = d
	}
	if req, ok := node["required"].([]any); ok {
		for _, r := range req {
			if name, ok := r.(string); ok {
				s.Required = append(s.Required, name)
			}
		}
	}
	if f, ok := node["format"].(string); ok {
		s.Format = f
	}
	if v, ok := node["const"]; ok {
		s.Enum = []string{fmt.Sprint(v)}
	}
	switch t := node["type"].(type) {
	case string:
		s.Type = genai.Type(strings.ToUpper(t))
	case []any:
		var nonNull []string
		hasNull := false
		for _, item := range t {
			name, _ := item.(string)
			if name == "null" {
				hasNull = true
			} else {
				nonNull = append(nonNull, name)
			}
		}
		if len(nonNull) == 0 {
			s.Type = genai.TypeNULL
		} else {
			for _, name := range nonNull {
				s.AnyOf = append(s.AnyOf, &genai.Schema{Type: genai.Type(strings.ToUpper(name))})
			}
			if hasNull {
				s.Nullable = genai.Ptr(true)
			}
		}
	}
	if enum, ok := node["enum"].([]any); ok {
		s.Enum = nil
		for _, v := range enum {
			s.Enum = append(s.Enum, fmt.Sprint(v))
		}
	}
	if props, ok := node["properties"].(map[string]any); ok {
		s.Properties = map[string]*genai.Schema{}
		for k, v := range props {
			if child, ok := v.(map[string]any); ok {
				s.Properties[k] = openAPISchema(child, false)
			}
		}
	}
	if items, ok := node["items"].(map[string]any); ok {
		s.Items = openAPISchema(items, false)
	}
	if anyOf, ok := node["anyOf"].([]any); ok {
		var nonNull []map[string]any
		hasNull := false
		for _, item := range anyOf {
			child, _ := item.(map[string]any)
			if t, _ := child["type"].(string); t == "null" {
				hasNull = true
				continue
			}
			nonNull = append(nonNull, child)
		}
		switch {
		case hasNull && len(nonNull) == 1:
			converted := openAPISchema(nonNull[0], false)
			merged := *converted
			merged.Nullable = genai.Ptr(true)
			if merged.Description == "" {
				merged.Description = s.Description
			}
			if len(merged.Required) == 0 {
				merged.Required = s.Required
			}
			s = &merged
		default:
			for _, child := range nonNull {
				s.AnyOf = append(s.AnyOf, openAPISchema(child, false))
			}
			if hasNull {
				s.Nullable = genai.Ptr(true)
			}
		}
	}
	if n, ok := node["minLength"].(float64); ok {
		v := int64(n)
		s.MinLength = &v
	}
	return s
}
