// Package jsonschema validates the JSON documents the server stores (survey blocks, styling, segment filters, response data, ...) against JSON Schemas exported from the zod definitions in packages/types.
//
// The schemas carry structure only. Cross-field rules that zod expresses as refinements (unique ids, logic that must reference existing questions) are not in them and belong to the service layer.
package jsonschema

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// Regenerate with `vp run --filter @forma/types export-json-schema`; CI fails when these are stale.
//
//go:embed schemas/*.json
var schemaFiles embed.FS

// Validator holds every exported schema, compiled once at startup.
type Validator struct {
	schemas map[string]*jsonschema.Schema
}

// New compiles all embedded schemas. A schema that does not compile is a build defect, so this fails rather than skipping it.
func New() (*Validator, error) {
	entries, err := fs.ReadDir(schemaFiles, "schemas")
	if err != nil {
		return nil, fmt.Errorf("read embedded schemas: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	// Formats such as "email" are enforced, not just annotations: the export relies on it where it drops a pattern Go cannot run.
	compiler.AssertFormat()
	urls := make(map[string]string, len(entries))
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".json")
		raw, err := schemaFiles.ReadFile(path.Join("schemas", e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read schema %s: %w", name, err)
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("parse schema %s: %w", name, err)
		}
		url := "mem://schemas/" + e.Name()
		if err := compiler.AddResource(url, doc); err != nil {
			return nil, fmt.Errorf("add schema %s: %w", name, err)
		}
		urls[name] = url
	}

	v := &Validator{schemas: make(map[string]*jsonschema.Schema, len(urls))}
	for name, url := range urls {
		s, err := compiler.Compile(url)
		if err != nil {
			return nil, fmt.Errorf("compile schema %s: %w", name, err)
		}
		v.schemas[name] = s
	}
	return v, nil
}

// Names lists the schemas the validator knows, sorted.
func (v *Validator) Names() []string {
	names := make([]string, 0, len(v.schemas))
	for n := range v.schemas {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Validate checks a JSON document against the named schema (for example "survey.blocks"). The document must be raw JSON as it arrived or as it will be stored.
func (v *Validator) Validate(name string, document []byte) error {
	s, ok := v.schemas[name]
	if !ok {
		return fmt.Errorf("jsonschema: unknown schema %q", name)
	}
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(document))
	if err != nil {
		return fmt.Errorf("%s: invalid JSON: %w", name, err)
	}
	if err := s.Validate(value); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// Violation is one way a document fails its schema: where in the document, and what.
type Violation struct {
	// Location is the path of the offending value: object keys, and array indexes in decimal.
	Location []string
	Message  string
}

var english = message.NewPrinter(language.English)

// Violations checks a document against the named schema and lists the failures, or none when it conforms. A value that matches none of a union's alternatives is reported inside the one alternative its type or "type" discriminator selects; when no single alternative is selected it is one violation, "Invalid input", at the value, as zod reports a failed union. Only an unknown schema or malformed JSON is an error.
func (v *Validator) Violations(name string, document []byte) ([]Violation, error) {
	err := v.Validate(name, document)
	if err == nil {
		return nil, nil
	}
	var verr *jsonschema.ValidationError
	if !errors.As(err, &verr) {
		return nil, err
	}
	var out []Violation
	collectViolations(verr, &out)
	return out, nil
}

func collectViolations(e *jsonschema.ValidationError, out *[]Violation) {
	switch e.ErrorKind.(type) {
	case *kind.AnyOf, *kind.OneOf:
		var selected []*jsonschema.ValidationError
		for _, c := range e.Causes {
			if !rejectedOnShape(c, len(e.InstanceLocation)) {
				selected = append(selected, c)
			}
		}
		if len(selected) == 1 {
			collectViolations(selected[0], out)
			return
		}
		*out = append(*out, Violation{Location: e.InstanceLocation, Message: "Invalid input"})
		return
	}
	if len(e.Causes) == 0 {
		*out = append(*out, Violation{Location: e.InstanceLocation, Message: e.ErrorKind.LocalizedString(english)})
		return
	}
	for _, c := range e.Causes {
		collectViolations(c, out)
	}
}

// rejectedOnShape reports whether a union alternative failed only because the value is of another type, or because its "type" discriminator names another alternative: such an alternative was never the one the value meant.
func rejectedOnShape(e *jsonschema.ValidationError, depth int) bool {
	if len(e.Causes) == 0 {
		switch e.ErrorKind.(type) {
		case *kind.Type:
			return len(e.InstanceLocation) == depth
		case *kind.Const, *kind.Enum:
			loc := e.InstanceLocation
			return len(loc) == depth+1 && loc[depth] == "type"
		}
		return false
	}
	for _, c := range e.Causes {
		if rejectedOnShape(c, depth) {
			return true
		}
	}
	return false
}
