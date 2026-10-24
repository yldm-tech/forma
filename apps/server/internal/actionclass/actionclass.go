// Package actionclass is the action class module: the user actions (a code event, or a no-code page view, click, exit intent, scroll or dwell) that trigger app surveys, with forma2's fields and rules (packages/types/action-classes.ts, apps/web/lib/actionClass, the user-actions settings page and /api/v3/action-classes).
package actionclass

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
)

// Types of action class.
const (
	TypeCode   = "code"
	TypeNoCode = "noCode"
)

// ActionClass is forma2's TActionClass.
type ActionClass struct {
	ID           uuid.UUID       `json:"id"`
	CreatedAt    time.Time       `json:"createdAt"`
	UpdatedAt    time.Time       `json:"updatedAt"`
	Name         string          `json:"name"`
	Description  *string         `json:"description"`
	Type         string          `json:"type"`
	Key          *string         `json:"key"`
	NoCodeConfig json.RawMessage `json:"noCodeConfig"`
	WorkspaceID  uuid.UUID       `json:"workspaceId"`
}

// Input is a validated ZActionClassInput. Key is meaningful only for code actions and NoCodeConfig (normalized, nil for null) only for no-code ones.
type Input struct {
	Name string
	// Description is nil when the input did not name it; its Value is nil for an explicit null.
	Description  *Optional
	Type         string
	Key          *string
	NoCodeConfig json.RawMessage
}

// Optional is a field that may be absent, null or a value.
type Optional struct {
	Value *string
}

var (
	noCodeTypes    = []string{"click", "pageView", "exitIntent", "fiftyPercentScroll", "pageDwell"}
	urlRules       = []string{"exactMatch", "contains", "startsWith", "endsWith", "notMatch", "notContains", "matchesRegex"}
	urlLikeRules   = []string{"exactMatch", "startsWith", "notMatch"}
	connectors     = []string{"or", "and"}
	domainHostname = regexp.MustCompile(`^(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,63}$`)
	ipv4Hostname   = regexp.MustCompile(`^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$`)
)

// noCodeConfigInput is ZActionClassNoCodeConfig as it arrives, with presence kept so a missing field is told from an empty one.
type noCodeConfigInput struct {
	Type       *string `json:"type"`
	URLFilters *[]struct {
		Value *string `json:"value"`
		Rule  *string `json:"rule"`
	} `json:"urlFilters"`
	URLFiltersConnector *string `json:"urlFiltersConnector"`
	ElementSelector     *struct {
		CSSSelector *string `json:"cssSelector"`
		InnerHTML   *string `json:"innerHtml"`
	} `json:"elementSelector"`
	TimeInSeconds *float64 `json:"timeInSeconds"`
}

// noCodeConfig is the normalized document that is stored: the fields of its type only, strings trimmed, as zod's parse output was.
type noCodeConfig struct {
	Type                string           `json:"type"`
	URLFilters          []urlFilter      `json:"urlFilters"`
	URLFiltersConnector *string          `json:"urlFiltersConnector,omitempty"`
	ElementSelector     *elementSelector `json:"elementSelector,omitempty"`
	TimeInSeconds       *int             `json:"timeInSeconds,omitempty"`
}

type urlFilter struct {
	Value string `json:"value"`
	Rule  string `json:"rule"`
}

type elementSelector struct {
	CSSSelector *string `json:"cssSelector,omitempty"`
	InnerHTML   *string `json:"innerHtml,omitempty"`
}

// ParseNoCodeConfig validates a no-code configuration against ZActionClassNoCodeConfig, its refinements included, and returns it normalized for storage. A JSON null is nil.
func ParseNoCodeConfig(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return nil, nil
	}
	const prefix = "noCodeConfig"
	var in noCodeConfigInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, apperr.Validation([]apperr.FieldError{{Field: prefix, Rule: "type"}})
	}
	var fields []apperr.FieldError
	fail := func(field, rule, param string) {
		fields = append(fields, apperr.FieldError{Field: prefix + field, Rule: rule, Param: param})
	}
	out := noCodeConfig{URLFilters: []urlFilter{}}
	switch {
	case in.Type == nil:
		fail(".type", "required", "")
	case !slices.Contains(noCodeTypes, *in.Type):
		fail(".type", "oneof", strings.Join(noCodeTypes, " "))
	default:
		out.Type = *in.Type
	}
	if in.URLFilters == nil {
		fail(".urlFilters", "required", "")
	} else {
		for i, f := range *in.URLFilters {
			at := ".urlFilters." + strconv.Itoa(i)
			value, rule := "", ""
			if f.Value != nil {
				value = strings.TrimSpace(*f.Value)
			}
			if f.Rule != nil {
				rule = *f.Rule
			}
			switch {
			case f.Value == nil:
				fail(at+".value", "required", "")
			case value == "":
				fail(at+".value", "min", "1")
			}
			if !slices.Contains(urlRules, rule) {
				fail(at+".rule", "oneof", strings.Join(urlRules, " "))
			} else if value != "" && !validURLFilterValue(value, rule) {
				fail(at+".value", "url", "")
			}
			out.URLFilters = append(out.URLFilters, urlFilter{Value: value, Rule: rule})
		}
	}
	if c := in.URLFiltersConnector; c != nil {
		if !slices.Contains(connectors, *c) {
			fail(".urlFiltersConnector", "oneof", strings.Join(connectors, " "))
		}
		out.URLFiltersConnector = c
	}
	switch out.Type {
	case "click":
		sel := in.ElementSelector
		if sel == nil {
			fail(".elementSelector", "required", "")
			break
		}
		es := &elementSelector{CSSSelector: trimmed(sel.CSSSelector), InnerHTML: trimmed(sel.InnerHTML)}
		// "Either cssSelector or innerHtml must be provided": an empty string counts as not provided.
		if (es.CSSSelector == nil || *es.CSSSelector == "") && (es.InnerHTML == nil || *es.InnerHTML == "") {
			fail(".elementSelector", "element_selector", "")
		}
		out.ElementSelector = es
	case "pageDwell":
		t := in.TimeInSeconds
		switch {
		case t == nil:
			fail(".timeInSeconds", "required", "")
		case *t != float64(int(*t)):
			fail(".timeInSeconds", "int", "")
		case *t < 1:
			fail(".timeInSeconds", "min", "1")
		case *t > 3600:
			fail(".timeInSeconds", "max", "3600")
		default:
			seconds := int(*t)
			out.TimeInSeconds = &seconds
		}
	}
	if len(fields) > 0 {
		return nil, apperr.Validation(fields)
	}
	doc, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("encode no-code config: %w", err)
	}
	return doc, nil
}

func trimmed(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	return &t
}

// validURLFilterValue is isValidActionClassUrlFilterValue: the rules that compare against a whole URL (exactMatch, startsWith, notMatch) need a path starting with "/" or an absolute http(s) URL whose host is a domain name, localhost, an IPv4 address or a bracketed IPv6 address. The other rules take any text.
func validURLFilterValue(value, rule string) bool {
	if !slices.Contains(urlLikeRules, rule) || strings.HasPrefix(value, "/") {
		return true
	}
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return false
	}
	if strings.HasPrefix(u.Host, "[") {
		return true
	}
	host := u.Hostname()
	return domainHostname.MatchString(host) || host == "localhost" || ipv4Hostname.MatchString(host)
}

// ValidateInput applies ZActionClassInput's field rules: the name is trimmed and required, a code action's key, when not null, is trimmed and non-empty, and a no-code configuration is parsed by ParseNoCodeConfig.
func ValidateInput(in *Input, rawNoCodeConfig json.RawMessage) error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return apperr.Validation([]apperr.FieldError{{Field: "name", Rule: "min", Param: "1"}})
	}
	switch in.Type {
	case TypeCode:
		in.NoCodeConfig = nil
		if in.Key != nil {
			key := strings.TrimSpace(*in.Key)
			if key == "" {
				return apperr.Validation([]apperr.FieldError{{Field: "key", Rule: "min", Param: "1"}})
			}
			in.Key = &key
		}
	case TypeNoCode:
		in.Key = nil
		doc, err := ParseNoCodeConfig(rawNoCodeConfig)
		if err != nil {
			return err
		}
		in.NoCodeConfig = doc
	default:
		return apperr.Validation([]apperr.FieldError{{Field: "type", Rule: "oneof", Param: TypeCode + " " + TypeNoCode}})
	}
	return nil
}
