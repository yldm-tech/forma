// Package jsonschema validates the JSON documents the server stores (survey blocks, styling, segment filters, response data, ...) against JSON Schemas exported from the zod definitions in packages/types.
//
// The schemas carry structure only. Cross-field rules that zod expresses as refinements (unique ids, logic that must reference existing questions) are not in them and belong to the service layer.
package jsonschema

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
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
