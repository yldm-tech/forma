package survey

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/yldm-tech/forma/server/internal/platform/v3api"
	"github.com/yldm-tech/forma/server/internal/survey/surveydoc"
)

// The v3 survey document is the public form of a survey: translatable text keyed by locale codes instead of the internal "default" key, strict about unknown fields, with app-survey distribution and targeting as their own objects. This file is forma2's apps/web/app/api/v3/surveys/schemas.ts: the strictness pre-check, the locale normalization and the document schema.

func keySet(keys ...string) map[string]bool {
	out := make(map[string]bool, len(keys))
	for _, k := range keys {
		out[k] = true
	}
	return out
}

func union(sets ...map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, s := range sets {
		for k := range s {
			out[k] = true
		}
	}
	return out
}

var (
	createRootKeys       = keySet("workspaceId", "name", "type", "status", "metadata", "defaultLanguage", "languages", "welcomeCard", "blocks", "endings", "hiddenFields", "variables", "distribution", "targeting")
	patchRootKeys        = keySet("name", "status", "metadata", "languages", "welcomeCard", "blocks", "endings", "hiddenFields", "variables", "distribution", "targeting")
	distributionScalars  = []string{"displayOption", "displayPercentage", "displayLimit", "recontactDays", "autoClose", "autoComplete", "delay"}
	distributionKeys     = union(keySet(distributionScalars...), keySet("triggers"))
	triggerKeys          = keySet("actionClassId")
	targetingKeys        = keySet("filters")
	languageKeys         = keySet("code", "default", "enabled")
	welcomeCardKeys      = keySet("enabled", "headline", "subheader", "fileUrl", "buttonLabel", "timeToFinish", "showResponseCount", "videoUrl")
	blockKeys            = keySet("id", "name", "elements", "logic", "logicFallback", "buttonLabel", "backButtonLabel")
	hiddenFieldKeys      = keySet("enabled", "fieldIds")
	variableKeys         = keySet("id", "name", "type", "value")
	endScreenKeys        = keySet("id", "type", "headline", "subheader", "buttonLabel", "buttonLink", "imageUrl", "videoUrl", "hideDefaultIcon")
	redirectEndingKeys   = keySet("id", "type", "url", "label")
	elementBaseKeys      = keySet("id", "type", "headline", "subheader", "imageUrl", "videoUrl", "required", "isDraft")
	labelChoiceKeys      = keySet("id", "label")
	pictureChoiceKeys    = keySet("id", "imageUrl")
	toggleInputKeys      = keySet("show", "required", "placeholder")
	charLimitKeys        = keySet("enabled", "min", "max")
	validationKeys       = keySet("rules", "logic")
	validationRuleKeys   = keySet("id", "type", "params", "field")
	logicKeys            = keySet("id", "conditions", "actions")
	conditionGroupKeys   = keySet("id", "connector", "conditions")
	conditionKeys        = keySet("id", "leftOperand", "operator", "rightOperand")
	dynamicReferenceKeys = keySet("type", "value", "meta")
	staticOperandKeys    = keySet("type", "value")
	calculateActionKeys  = keySet("id", "objective", "variableId", "operator", "value")
	targetActionKeys     = keySet("id", "objective", "target")

	welcomeCardI18nKeys = []string{"headline", "subheader", "buttonLabel"}
	blockI18nKeys       = []string{"buttonLabel", "backButtonLabel"}
	endingI18nKeys      = []string{"headline", "subheader", "buttonLabel"}
	elementI18nKeys     = []string{"headline", "subheader", "placeholder", "label", "otherOptionPlaceholder", "lowerLabel", "upperLabel", "ctaButtonLabel", "html"}
	toggleInputI18nKeys = []string{"addressLine1", "addressLine2", "city", "state", "zip", "country", "firstName", "lastName", "email", "phone", "company"}
	metadataI18nKeys    = []string{"title", "description"}
	allElementExtraKeys = keySet("allowMulti", "allowMultipleFiles", "allowedFileExtensions", "addressLine1", "addressLine2", "buttonExternal", "buttonUrl", "calHost", "calUserName", "charLimit", "choices", "city", "columns", "company", "country", "ctaButtonLabel", "displayType", "email", "firstName", "format", "html", "inputType", "insightsEnabled", "isColorCodingEnabled", "label", "lastName", "longAnswer", "lowerLabel", "maxSizeInMB", "otherOptionPlaceholder", "phone", "placeholder", "rows", "shuffleOption", "state", "upperLabel", "validation", "zip")
	elementKeysAnyType  = union(elementBaseKeys, allElementExtraKeys)
	elementKeysByType   = map[string]map[string]bool{
		"address":              union(elementBaseKeys, keySet("addressLine1", "addressLine2", "city", "state", "zip", "country", "validation")),
		"cal":                  union(elementBaseKeys, keySet("calUserName", "calHost")),
		"ces":                  union(elementBaseKeys, keySet("scale", "range", "lowerLabel", "upperLabel", "isColorCodingEnabled")),
		"consent":              union(elementBaseKeys, keySet("label", "validation")),
		"contactInfo":          union(elementBaseKeys, keySet("firstName", "lastName", "email", "phone", "company", "validation")),
		"csat":                 union(elementBaseKeys, keySet("scale", "range", "lowerLabel", "upperLabel", "isColorCodingEnabled")),
		"cta":                  union(elementBaseKeys, keySet("buttonExternal", "buttonUrl", "ctaButtonLabel")),
		"date":                 union(elementBaseKeys, keySet("html", "format", "validation")),
		"fileUpload":           union(elementBaseKeys, keySet("allowMultipleFiles", "maxSizeInMB", "allowedFileExtensions", "validation")),
		"matrix":               union(elementBaseKeys, keySet("rows", "columns", "shuffleOption", "validation")),
		"multipleChoiceMulti":  union(elementBaseKeys, keySet("choices", "shuffleOption", "otherOptionPlaceholder", "validation", "displayType")),
		"multipleChoiceSingle": union(elementBaseKeys, keySet("choices", "shuffleOption", "otherOptionPlaceholder", "displayType")),
		"nps":                  union(elementBaseKeys, keySet("lowerLabel", "upperLabel", "isColorCodingEnabled")),
		"openText":             union(elementBaseKeys, keySet("placeholder", "longAnswer", "inputType", "insightsEnabled", "charLimit", "validation")),
		"pictureSelection":     union(elementBaseKeys, keySet("allowMulti", "choices", "validation")),
		"ranking":              union(elementBaseKeys, keySet("choices", "otherOptionPlaceholder", "shuffleOption", "validation")),
		"rating":               union(elementBaseKeys, keySet("scale", "range", "lowerLabel", "upperLabel", "isColorCodingEnabled")),
	}
)

func joinPath(base, key string) string {
	if base == "" {
		return key
	}
	return base + "." + key
}

// sortedKeys orders field names as JavaScript's localeCompare does for these ASCII names: case-insensitively first.
func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := strings.ToLower(keys[i]), strings.ToLower(keys[j])
		if a != b {
			return a < b
		}
		return keys[i] > keys[j]
	})
	return keys
}

// strictness is forma2's getUnsupportedV3SurveyDocumentFields: every unknown field, everywhere in the document, and every translatable field whose keys are not locales or miss the default language.
type strictness struct {
	params  []v3api.InvalidParam
	allowed []string
}

func (s *strictness) unknownKeys(value any, allowed map[string]bool, path, context string) {
	o := asObject(value)
	if o == nil {
		return
	}
	fields := ""
	if context != "" {
		fields = strings.Join(sortedKeys(allowed), ", ")
	}
	for _, key := range o.keys {
		if allowed[key] {
			continue
		}
		reason := fmt.Sprintf("Unsupported field '%s'", key)
		if context != "" {
			reason = fmt.Sprintf("Unsupported field '%s' for %s. Allowed fields: %s", key, context, fields)
		}
		s.params = append(s.params, v3api.InvalidParam{Name: joinPath(path, key), Reason: reason, Code: v3api.CodeUnsupportedField})
	}
}

func (s *strictness) missingKeys(value any, required []string, path, context string) {
	o := asObject(value)
	if o == nil {
		return
	}
	for _, key := range required {
		if _, ok := o.get(key); !ok {
			s.params = append(s.params, v3api.InvalidParam{Name: joinPath(path, key), Reason: fmt.Sprintf("Missing required field '%s' for %s", key, context), Code: v3api.CodeMissingRequiredField})
		}
	}
}

// translatable is forma2's validateTranslatableField; defaultLanguage "" stands for forma2's null, an unparseable default language.
func (s *strictness) translatable(value any, path, defaultLanguage string) {
	o := asObject(value)
	if o == nil {
		return
	}
	if _, ok := o.get("default"); ok {
		s.params = append(s.params, v3api.InvalidParam{Name: path + ".default", Reason: "Use the defaultLanguage locale code instead of the internal 'default' translation key"})
	}
	seen := map[string]string{}
	includesDefault := defaultLanguage == ""
	for _, key := range o.keys {
		if key == "default" {
			continue
		}
		normalized := writeLanguageCode(key, s.allowed)
		if normalized == "" {
			s.params = append(s.params, v3api.InvalidParam{Name: path + "." + key, Reason: fmt.Sprintf("Language key '%s' is not a valid locale code", key), Code: v3api.CodeInvalidLocale})
			continue
		}
		lookup := strings.ToLower(normalized)
		if previous, ok := seen[lookup]; ok {
			s.params = append(s.params, v3api.InvalidParam{Name: path + "." + key, Reason: fmt.Sprintf("Language key '%s' duplicates '%s' after locale normalization", key, previous), Code: v3api.CodeDuplicateLocale})
			continue
		}
		seen[lookup] = key
		if lookup == strings.ToLower(defaultLanguage) {
			includesDefault = true
		}
	}
	if !includesDefault {
		s.params = append(s.params, v3api.InvalidParam{Name: path, Reason: fmt.Sprintf("Translatable field must include the defaultLanguage locale '%s'", defaultLanguage)})
	}
}

func (s *strictness) choice(value any, path, defaultLanguage string, allowed map[string]bool) {
	context := "choice"
	if len(allowed) == len(pictureChoiceKeys) && allowed["imageUrl"] {
		context = "pictureSelection choice"
	}
	s.unknownKeys(value, allowed, path, context)
	if o := asObject(value); o != nil {
		label, _ := o.get("label")
		s.translatable(label, path+".label", defaultLanguage)
	}
}

func (s *strictness) operand(value any, path string) {
	o := asObject(value)
	if o == nil {
		return
	}
	if t, _ := o.get("type"); t == "static" {
		s.unknownKeys(value, staticOperandKeys, path, "")
		return
	}
	s.unknownKeys(value, dynamicReferenceKeys, path, "")
}

func (s *strictness) conditionGroup(value any, path string) {
	s.unknownKeys(value, conditionGroupKeys, path, "")
	o := asObject(value)
	if o == nil {
		return
	}
	conditions, _ := o.get("conditions")
	list, _ := conditions.([]any)
	for i, c := range list {
		cp := fmt.Sprintf("%s.conditions.%d", path, i)
		if co := asObject(c); co != nil {
			if nested, ok := co.get("conditions"); ok {
				if _, isList := nested.([]any); isList {
					s.conditionGroup(c, cp)
					continue
				}
			}
		}
		s.unknownKeys(c, conditionKeys, cp, "")
		if co := asObject(c); co != nil {
			left, _ := co.get("leftOperand")
			s.unknownKeys(left, dynamicReferenceKeys, cp+".leftOperand", "")
			right, _ := co.get("rightOperand")
			s.operand(right, cp+".rightOperand")
		}
	}
}

func (s *strictness) action(value any, path string) {
	o := asObject(value)
	if o == nil {
		return
	}
	switch objective, _ := o.get("objective"); objective {
	case "calculate":
		s.unknownKeys(value, calculateActionKeys, path, "")
		v, _ := o.get("value")
		s.operand(v, path+".value")
	case "requireAnswer", "jumpToBlock":
		s.unknownKeys(value, targetActionKeys, path, "")
	}
}

func (s *strictness) element(value any, path, defaultLanguage string) {
	o := asObject(value)
	keys := elementKeysAnyType
	context := "survey element"
	if o != nil {
		if t, ok := o.values["type"].(string); ok {
			context = fmt.Sprintf("element type '%s'", t)
			if byType, ok := elementKeysByType[t]; ok {
				keys = byType
			}
		}
	}
	s.missingKeys(value, []string{"id", "type", "headline", "required"}, path, context)
	s.unknownKeys(value, keys, path, context)
	if o == nil {
		return
	}
	for _, key := range elementI18nKeys {
		v, _ := o.get(key)
		s.translatable(v, path+"."+key, defaultLanguage)
	}
	for _, key := range toggleInputI18nKeys {
		v, _ := o.get(key)
		s.unknownKeys(v, toggleInputKeys, path+"."+key, "")
		if t := asObject(v); t != nil {
			p, _ := t.get("placeholder")
			s.translatable(p, path+"."+key+".placeholder", defaultLanguage)
		}
	}
	if cl := asObject(o.values["charLimit"]); cl != nil {
		s.unknownKeys(cl, charLimitKeys, path+".charLimit", "")
	}
	validation, _ := o.get("validation")
	s.unknownKeys(validation, validationKeys, path+".validation", "")
	if vo := asObject(validation); vo != nil {
		rules, _ := vo.values["rules"].([]any)
		for i, r := range rules {
			s.unknownKeys(r, validationRuleKeys, fmt.Sprintf("%s.validation.rules.%d", path, i), "")
		}
	}
	choiceKeys := labelChoiceKeys
	if o.values["type"] == "pictureSelection" {
		choiceKeys = pictureChoiceKeys
	}
	for _, list := range []struct {
		key  string
		keys map[string]bool
	}{{"choices", choiceKeys}, {"rows", labelChoiceKeys}, {"columns", labelChoiceKeys}} {
		items, _ := o.values[list.key].([]any)
		for i, item := range items {
			s.choice(item, fmt.Sprintf("%s.%s.%d", path, list.key, i), defaultLanguage, list.keys)
		}
	}
}

func (s *strictness) block(value any, path, defaultLanguage string) {
	s.unknownKeys(value, blockKeys, path, "")
	o := asObject(value)
	if o == nil {
		return
	}
	for _, key := range blockI18nKeys {
		v, _ := o.get(key)
		s.translatable(v, path+"."+key, defaultLanguage)
	}
	elements, _ := o.values["elements"].([]any)
	for i, e := range elements {
		s.element(e, fmt.Sprintf("%s.elements.%d", path, i), defaultLanguage)
	}
	logic, _ := o.values["logic"].([]any)
	for i, l := range logic {
		lp := fmt.Sprintf("%s.logic.%d", path, i)
		s.unknownKeys(l, logicKeys, lp, "")
		if lo := asObject(l); lo != nil {
			conditions, _ := lo.get("conditions")
			s.conditionGroup(conditions, lp+".conditions")
			actions, _ := lo.values["actions"].([]any)
			for a, act := range actions {
				s.action(act, fmt.Sprintf("%s.actions.%d", lp, a))
			}
		}
	}
}

func (s *strictness) ending(value any, path, defaultLanguage string) {
	o := asObject(value)
	if o == nil {
		return
	}
	keys := endScreenKeys
	if o.values["type"] == "redirectToUrl" {
		keys = redirectEndingKeys
	}
	context := "survey ending"
	if t, ok := o.values["type"].(string); ok {
		context = fmt.Sprintf("ending type '%s'", t)
	}
	s.missingKeys(value, []string{"id", "type"}, path, context)
	s.unknownKeys(value, keys, path, context)
	for _, key := range endingI18nKeys {
		v, _ := o.get(key)
		s.translatable(v, path+"."+key, defaultLanguage)
	}
}

// normalizedDefaultLanguage is forma2's getNormalizedDefaultLanguage for a document that carries defaultLanguage: the write code of a string, "" for anything else.
func normalizedDefaultLanguage(value any, allowed []string) string {
	s, ok := value.(string)
	if !ok {
		return ""
	}
	return writeLanguageCode(s, allowed)
}

// unsupportedFields runs the strictness pre-check over a document.
func unsupportedFields(value any, rootKeys map[string]bool, fallbackDefault string, allowed []string) []v3api.InvalidParam {
	s := &strictness{allowed: allowed}
	s.unknownKeys(value, rootKeys, "", "the survey document")
	o := asObject(value)
	if o == nil {
		return s.params
	}
	defaultLanguage := fallbackDefault
	if dl, ok := o.get("defaultLanguage"); ok {
		defaultLanguage = normalizedDefaultLanguage(dl, allowed)
	}
	if langs, ok := o.values["languages"].([]any); ok {
		for i, l := range langs {
			s.unknownKeys(l, languageKeys, fmt.Sprintf("languages.%d", i), "")
		}
	}
	wc, _ := o.get("welcomeCard")
	s.unknownKeys(wc, welcomeCardKeys, "welcomeCard", "")
	if wco := asObject(wc); wco != nil {
		for _, key := range welcomeCardI18nKeys {
			v, _ := wco.get(key)
			s.translatable(v, "welcomeCard."+key, defaultLanguage)
		}
	}
	hf, _ := o.get("hiddenFields")
	s.unknownKeys(hf, hiddenFieldKeys, "hiddenFields", "")
	if vars, ok := o.values["variables"].([]any); ok {
		for i, v := range vars {
			s.unknownKeys(v, variableKeys, fmt.Sprintf("variables.%d", i), "")
		}
	}
	if blocks, ok := o.values["blocks"].([]any); ok {
		for i, b := range blocks {
			s.block(b, fmt.Sprintf("blocks.%d", i), defaultLanguage)
		}
	}
	if endings, ok := o.values["endings"].([]any); ok {
		for i, e := range endings {
			s.ending(e, fmt.Sprintf("endings.%d", i), defaultLanguage)
		}
	}
	if meta := asObject(o.values["metadata"]); meta != nil {
		for _, key := range metadataI18nKeys {
			v, _ := meta.get(key)
			s.translatable(v, "metadata."+key, defaultLanguage)
		}
	}
	if d, ok := o.get("distribution"); ok {
		s.unknownKeys(d, distributionKeys, "distribution", "survey distribution")
		if do := asObject(d); do != nil {
			triggers, _ := do.values["triggers"].([]any)
			for i, t := range triggers {
				p := fmt.Sprintf("distribution.triggers.%d", i)
				s.missingKeys(t, []string{"actionClassId"}, p, "survey trigger")
				s.unknownKeys(t, triggerKeys, p, "survey trigger")
			}
		}
	}
	if t, ok := o.get("targeting"); ok {
		s.unknownKeys(t, targetingKeys, "targeting", "survey targeting")
	}
	return s.params
}

// normalizer is forma2's createV3SurveyDocumentNormalizer: locale-keyed translatable maps become internal maps whose default-language entry is "default".
type normalizer struct {
	allowed                 []string
	allowInternalDefaultKey bool
	defaultLanguage         string
}

func (n *normalizer) isPublicI18nMap(o *object) bool {
	if len(o.keys) == 0 {
		return false
	}
	for _, k := range o.keys {
		if _, ok := o.values[k].(string); !ok {
			return false
		}
		if k == "default" && n.allowInternalDefaultKey {
			continue
		}
		if writeLanguageCode(k, n.allowed) == "" {
			return false
		}
	}
	return true
}

func (n *normalizer) field(value any) any {
	o := asObject(value)
	if o == nil || !n.isPublicI18nMap(o) {
		return value
	}
	type entry struct{ key, text string }
	entries := make([]entry, len(o.keys))
	var defaultEntry *entry
	for i, k := range o.keys {
		key := writeLanguageCode(k, n.allowed)
		if key == "" {
			key = k
		}
		entries[i] = entry{key, o.values[k].(string)}
		if defaultEntry == nil && strings.EqualFold(key, n.defaultLanguage) {
			defaultEntry = &entries[i]
		}
	}
	out := newObject()
	if defaultEntry == nil {
		for _, e := range entries {
			out.set(e.key, e.text)
		}
		return out
	}
	out.set("default", defaultEntry.text)
	for _, e := range entries {
		if e.key != "default" && !strings.EqualFold(e.key, n.defaultLanguage) {
			out.set(e.key, e.text)
		}
	}
	return out
}

func (n *normalizer) fields(value any, keys []string) {
	o := asObject(value)
	if o == nil {
		return
	}
	for _, k := range keys {
		if v, ok := o.get(k); ok && v != nil {
			o.set(k, n.field(v))
		}
	}
}

func (n *normalizer) choices(value any, key string) {
	o := asObject(value)
	if o == nil {
		return
	}
	items, _ := o.values[key].([]any)
	for _, item := range items {
		n.fields(item, []string{"label"})
	}
}

func (n *normalizer) element(value any) {
	o := asObject(value)
	if o == nil {
		return
	}
	n.fields(o, elementI18nKeys)
	for _, key := range toggleInputI18nKeys {
		if v, ok := o.get(key); ok {
			n.fields(v, []string{"placeholder"})
		}
	}
	for _, key := range []string{"choices", "rows", "columns"} {
		n.choices(o, key)
	}
}

// normalizeDocument rewrites a v3 document in place. With applyDefault a missing defaultLanguage is filled with the fallback; with generateIDs blocks and variables without an id get one.
func normalizeDocument(value any, fallback string, applyDefault, allowInternalDefaultKey, generateIDs bool, allowed []string) {
	o := asObject(value)
	if o == nil {
		return
	}
	n := &normalizer{allowed: allowed, allowInternalDefaultKey: allowInternalDefaultKey, defaultLanguage: fallback}
	if dl, ok := o.get("defaultLanguage"); ok {
		if normalized := normalizedDefaultLanguage(dl, allowed); normalized != "" {
			n.defaultLanguage = normalized
			o.set("defaultLanguage", normalized)
		}
	} else if applyDefault {
		o.set("defaultLanguage", fallback)
	}
	if meta, ok := o.get("metadata"); ok {
		n.fields(meta, metadataI18nKeys)
	}
	if wc, ok := o.get("welcomeCard"); ok {
		n.fields(wc, welcomeCardI18nKeys)
	}
	if blocks, ok := o.values["blocks"].([]any); ok {
		for _, b := range blocks {
			n.fields(b, blockI18nKeys)
			if bo := asObject(b); bo != nil {
				elements, _ := bo.values["elements"].([]any)
				for _, e := range elements {
					n.element(e)
				}
			}
		}
	}
	if endings, ok := o.values["endings"].([]any); ok {
		for _, e := range endings {
			n.fields(e, endingI18nKeys)
		}
	}
	if generateIDs {
		for _, key := range []string{"blocks", "variables"} {
			items, _ := o.values[key].([]any)
			for _, item := range items {
				if io := asObject(item); io != nil {
					if _, ok := io.get("id"); !ok {
						io.set("id", surveydoc.NewID())
					}
				}
			}
		}
	}
}

// v3Distribution is an app survey's display settings and triggers.
type v3Distribution struct {
	DisplayOption     string      `json:"displayOption"`
	DisplayPercentage *float64    `json:"displayPercentage"`
	DisplayLimit      *int        `json:"displayLimit"`
	RecontactDays     *int        `json:"recontactDays"`
	AutoClose         *int        `json:"autoClose"`
	AutoComplete      *int        `json:"autoComplete"`
	Delay             int         `json:"delay"`
	Triggers          []v3Trigger `json:"triggers"`
}

type v3Trigger struct {
	ActionClassID string `json:"actionClassId"`
}

type v3Targeting struct {
	Filters json.RawMessage `json:"filters"`
}

type v3LanguageInput struct {
	Code    string
	Default *bool
	Enabled bool
}

// v3Document is a parsed v3 survey document. The document parts are kept as ordered JSON values in the internal form, ready to be stored.
type v3Document struct {
	// Create only.
	WorkspaceID string
	Type        string

	Name            string
	Status          string
	DefaultLanguage string
	Languages       []v3LanguageInput
	Metadata        any
	WelcomeCard     any
	Blocks          any
	Endings         any
	HiddenFields    any
	Variables       any
	Distribution    *v3Distribution
	Targeting       *v3Targeting

	// present records which keys a patch carried.
	present map[string]bool
}

func (d *v3Document) document() surveydoc.Document {
	return surveydoc.Document{
		WelcomeCard: encodeValue(d.WelcomeCard), Blocks: encodeValue(d.Blocks), Endings: encodeValue(d.Endings),
		HiddenFields: encodeValue(d.HiddenFields), Variables: encodeValue(d.Variables), Metadata: encodeValue(d.Metadata),
	}
}

// typed is the typed view of the document parts.
func (d *v3Document) typed() (*surveydoc.Survey, error) {
	return surveydoc.Decode(d.document(), nil, nil)
}

func jsType(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case json.Number:
		return "number"
	case bool:
		return "boolean"
	case []any:
		return "array"
	}
	return "object"
}

// parser accumulates the document schema's issues.
type parser struct {
	issues []surveydoc.Issue
	docs   *surveydoc.Validator
}

func (p *parser) add(path []any, message string, code string) {
	p.issues = append(p.issues, surveydoc.Issue{Path: path, Message: message, Code: code})
}

func (p *parser) expected(path []any, expected string, v any, present bool) {
	received := jsType(v)
	if !present {
		received = "undefined"
	}
	p.add(path, fmt.Sprintf("Invalid input: expected %s, received %s", expected, received), "")
}

func (p *parser) enum(path []any, v any, present bool, options []string) string {
	s, ok := v.(string)
	if !ok || !slices.Contains(options, s) {
		quoted := make([]string, len(options))
		for i, o := range options {
			quoted[i] = `"` + o + `"`
		}
		if !present {
			p.expected(path, "string", v, false)
		} else {
			p.add(path, "Invalid option: expected one of "+strings.Join(quoted, "|"), "")
		}
		return ""
	}
	return s
}

func number(v any) (float64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	f, err := n.Float64()
	return f, err == nil
}

// intField reads an optional int field with a lower bound: absent is the default, null is nil when nullable.
func (p *parser) intField(o *object, key string, path []any, min int, nullable bool, def *int, minMessage string) *int {
	v, ok := o.get(key)
	if !ok {
		return def
	}
	if v == nil && nullable {
		return nil
	}
	f, isNum := number(v)
	switch {
	case !isNum:
		p.expected(path, "number", v, true)
		return nil
	case f != math.Trunc(f):
		p.add(path, "Invalid input: expected int, received number", "")
		return nil
	case f < float64(min):
		if minMessage == "" {
			minMessage = fmt.Sprintf("Too small: expected number to be >=%d", min)
		}
		p.add(path, minMessage, "")
		return nil
	}
	i := int(f)
	return &i
}

// language tag of the document, forma2's createZV3SurveyLanguageTag.
func (p *parser) languageTag(path []any, v any, allowed []string) string {
	s, ok := v.(string)
	if !ok {
		p.expected(path, "string", v, true)
		return ""
	}
	s = strings.TrimSpace(s)
	if s == "" {
		p.add(path, "Language code is required", "")
		return ""
	}
	code := writeLanguageCode(s, allowed)
	if code == "" {
		p.add(path, fmt.Sprintf("Language '%s' is not a valid locale code", s), v3api.CodeInvalidLocale)
	}
	return code
}

func (p *parser) languages(v any, allowed []string) []v3LanguageInput {
	list, ok := v.([]any)
	if !ok {
		p.expected([]any{"languages"}, "array", v, true)
		return nil
	}
	out := make([]v3LanguageInput, 0, len(list))
	for i, item := range list {
		path := []any{"languages", i}
		o := asObject(item)
		if o == nil {
			p.expected(path, "object", item, true)
			continue
		}
		code, present := o.get("code")
		if !present {
			p.expected(append(path, "code"), "string", nil, false)
			continue
		}
		l := v3LanguageInput{Code: p.languageTag(extend(path, "code"), code, allowed), Enabled: true}
		if d, ok := o.get("default"); ok {
			b, isBool := d.(bool)
			if !isBool {
				p.expected(extend(path, "default"), "boolean", d, true)
			}
			l.Default = &b
		}
		if e, ok := o.get("enabled"); ok {
			b, isBool := e.(bool)
			if !isBool {
				p.expected(extend(path, "enabled"), "boolean", e, true)
			}
			l.Enabled = b
		}
		out = append(out, l)
	}
	return out
}

func extend(base []any, parts ...any) []any {
	out := make([]any, 0, len(base)+len(parts))
	out = append(out, base...)
	return append(out, parts...)
}

// languageRules is forma2's addLanguageIssues.
func (p *parser) languageRules(defaultLanguage string, langs []v3LanguageInput) {
	normalizedDefault := strings.ToLower(defaultLanguage)
	seen := map[string]bool{}
	defaults := 0
	for i, l := range langs {
		code := strings.ToLower(l.Code)
		if seen[code] {
			p.add([]any{"languages", i, "code"}, fmt.Sprintf("Language '%s' is duplicated", l.Code), v3api.CodeDuplicateLocale)
		}
		seen[code] = true
		if l.Default != nil && *l.Default {
			defaults++
			if normalizedDefault != "" && code != normalizedDefault {
				p.add([]any{"languages", i, "default"}, "The default language entry must match defaultLanguage", "")
			}
		}
		if normalizedDefault != "" && code == normalizedDefault && !l.Enabled {
			p.add([]any{"languages", i, "enabled"}, "The default language cannot be disabled", "")
		}
	}
	if defaults > 1 {
		p.add([]any{"languages"}, "Only one language can be marked as default", "")
	}
}

func (p *parser) distribution(v any) *v3Distribution {
	o := asObject(v)
	if o == nil {
		p.expected([]any{"distribution"}, "object", v, true)
		return nil
	}
	d := &v3Distribution{DisplayOption: "displayOnce", Triggers: []v3Trigger{}}
	at := func(key string) []any { return []any{"distribution", key} }
	if dv, ok := o.get("displayOption"); ok {
		d.DisplayOption = p.enum(at("displayOption"), dv, true, DisplayOptions)
	}
	if pv, ok := o.get("displayPercentage"); ok && pv != nil {
		f, isNum := number(pv)
		switch {
		case !isNum:
			p.expected(at("displayPercentage"), "number", pv, true)
		case f < 0.01:
			p.add(at("displayPercentage"), "Too small: expected number to be >=0.01", "")
		case f > 100:
			p.add(at("displayPercentage"), "Too big: expected number to be <=100", "")
		default:
			d.DisplayPercentage = &f
		}
	}
	d.DisplayLimit = p.intField(o, "displayLimit", at("displayLimit"), 0, true, nil, "")
	d.RecontactDays = p.intField(o, "recontactDays", at("recontactDays"), 0, true, nil, "")
	d.AutoClose = p.intField(o, "autoClose", at("autoClose"), 0, true, nil, "")
	d.AutoComplete = p.intField(o, "autoComplete", at("autoComplete"), 1, true, nil, "Response limit must be greater than 0")
	zero := 0
	if delay := p.intField(o, "delay", at("delay"), 0, false, &zero, ""); delay != nil {
		d.Delay = *delay
	}
	if tv, ok := o.get("triggers"); ok {
		list, isList := tv.([]any)
		if !isList {
			p.expected(at("triggers"), "array", tv, true)
		}
		for i, t := range list {
			to := asObject(t)
			if to == nil {
				p.expected([]any{"distribution", "triggers", i}, "object", t, true)
				continue
			}
			id, present := to.get("actionClassId")
			s, isString := id.(string)
			if !isString {
				p.expected([]any{"distribution", "triggers", i, "actionClassId"}, "string", id, present)
				continue
			}
			d.Triggers = append(d.Triggers, v3Trigger{ActionClassID: s})
		}
	}
	return d
}

// documentPart validates one part against its exported schema and returns it with the defaults zod filled in.
func (p *parser) documentPart(name, schema string, v any) any {
	issues, err := p.docs.SchemaIssues(schema, encodeValue(v), name)
	if err != nil {
		p.add([]any{name}, "Invalid input", "")
		return v
	}
	p.issues = append(p.issues, issues...)
	return v
}

// parse is the v3 document schema over a normalized document. For a patch only the present keys are read, and none of the create defaults apply.
// parseMode is which of forma2's document schemas parse applies.
type parseMode int

const (
	// parseCreate is ZV3CreateSurveyBody: the workspace and type, defaults, and the app-only rules.
	parseCreate parseMode = iota
	// parseStored is createZV3SurveyDocumentBaseSchema over a stored survey: defaults, no workspace or type, no app-only rules.
	parseStored
	// parsePatch is the patch body: only the keys present, no defaults.
	parsePatch
)

func (p *parser) parse(o *object, mode parseMode, allowed []string, surveyType string, defaultLanguage string) *v3Document {
	patch := mode == parsePatch
	d := &v3Document{present: map[string]bool{}, Type: surveyType}
	for _, k := range o.keys {
		d.present[k] = true
	}
	if mode == parseCreate {
		wid, ok := o.get("workspaceId")
		if s, isString := wid.(string); isString {
			d.WorkspaceID = s
		} else {
			p.expected([]any{"workspaceId"}, "string", wid, ok)
		}
		d.Type = TypeLink
		if t, ok := o.get("type"); ok {
			d.Type = p.enum([]any{"type"}, t, true, []string{TypeLink, TypeApp})
		}
		surveyType = d.Type
	}
	if nv, ok := o.get("name"); ok || !patch {
		s, isString := nv.(string)
		switch {
		case !isString:
			p.expected([]any{"name"}, "string", nv, ok)
		case strings.TrimSpace(s) == "":
			p.add([]any{"name"}, "Survey name is required", "")
		default:
			d.Name = strings.TrimSpace(s)
		}
	}
	if !patch {
		d.Status = StatusDraft
	}
	if sv, ok := o.get("status"); ok {
		d.Status = p.enum([]any{"status"}, sv, true, Statuses)
	}
	if !patch {
		d.DefaultLanguage = defaultV3Language
		if dl, ok := o.get("defaultLanguage"); ok {
			d.DefaultLanguage = p.languageTag([]any{"defaultLanguage"}, dl, allowed)
		}
	} else {
		d.DefaultLanguage = defaultLanguage
	}
	if lv, ok := o.get("languages"); ok {
		d.Languages = p.languages(lv, allowed)
	} else if !patch {
		d.Languages = []v3LanguageInput{}
	}

	part := func(key, schema string, def string) any {
		v, ok := o.get(key)
		if !ok {
			if patch {
				return nil
			}
			parsed, _ := decodeValue([]byte(def))
			return parsed
		}
		return p.documentPart(key, schema, v)
	}
	d.Metadata = part("metadata", "survey.metadata", `{}`)
	d.WelcomeCard = part("welcomeCard", "survey.welcome_card", `{"enabled": false}`)
	if bv, ok := o.get("blocks"); ok {
		if list, isList := bv.([]any); isList && len(list) == 0 {
			p.add([]any{"blocks"}, "At least one block is required", "")
		}
		d.Blocks = p.documentPart("blocks", "survey.blocks", bv)
	} else if !patch {
		p.expected([]any{"blocks"}, "array", nil, false)
	}
	d.Endings = part("endings", "survey.endings", `[]`)
	d.HiddenFields = part("hiddenFields", "survey.hidden_fields", `{"enabled": false}`)
	d.Variables = part("variables", "survey.variables", `[]`)
	if dv, ok := o.get("distribution"); ok {
		d.Distribution = p.distribution(dv)
	}
	if tv, ok := o.get("targeting"); ok {
		var filters any
		present := false
		if to := asObject(tv); to != nil {
			filters, present = to.get("filters")
		}
		if !present {
			p.expected([]any{"targeting", "filters"}, "array", nil, false)
		} else {
			raw := encodeValue(filters)
			if m := segmentFilterBoundsViolation(raw); m != "" {
				p.add([]any{"targeting", "filters"}, m, "")
			} else if issues, err := p.docs.SchemaIssues("segment.filters", raw, "targeting", "filters"); err == nil && len(issues) > 0 {
				p.issues = append(p.issues, issues...)
			} else if !firstConnectorsAreNull(raw) {
				p.add([]any{"targeting", "filters"}, segmentFiltersInvalidConnection, "")
			} else {
				d.Targeting = &v3Targeting{Filters: raw}
			}
		}
	}
	if len(p.issues) == 0 {
		p.fieldRules(d)
	}
	p.languageRules(d.DefaultLanguage, d.Languages)
	if mode != parseStored {
		p.distributionRules(d, surveyType)
	}
	if patch && len(o.keys) == 0 {
		p.add(nil, "Request body must include at least one updatable field", "")
	}
	applyDocumentDefaults(d)
	return d
}

// fieldRules are the refinements zod attaches to the document's own fields (element ids, URLs, per-element rules, uniqueness within one list).
func (p *parser) fieldRules(d *v3Document) {
	parts := surveydoc.Document{Metadata: json.RawMessage(`{}`), WelcomeCard: json.RawMessage(`{"enabled": false}`), Blocks: json.RawMessage(`[]`), Endings: json.RawMessage(`[]`), HiddenFields: json.RawMessage(`{"enabled": false}`), Variables: json.RawMessage(`[]`)}
	if d.Metadata != nil {
		parts.Metadata = encodeValue(d.Metadata)
	}
	if d.WelcomeCard != nil {
		parts.WelcomeCard = encodeValue(d.WelcomeCard)
	}
	if d.Blocks != nil {
		parts.Blocks = encodeValue(d.Blocks)
	}
	if d.Endings != nil {
		parts.Endings = encodeValue(d.Endings)
	}
	if d.HiddenFields != nil {
		parts.HiddenFields = encodeValue(d.HiddenFields)
	}
	if d.Variables != nil {
		parts.Variables = encodeValue(d.Variables)
	}
	s, err := surveydoc.Decode(parts, nil, nil)
	if err != nil {
		p.add(nil, "Invalid input", "")
		return
	}
	p.issues = append(p.issues, surveydoc.FieldIssues(s)...)
}

// distributionRules is forma2's addAppDistributionIssues.
func (p *parser) distributionRules(d *v3Document, surveyType string) {
	if surveyType == TypeLink {
		if d.present["distribution"] {
			p.add([]any{"distribution"}, "'distribution' is only supported for app surveys (type: 'app')", v3api.CodeUnsupportedField)
		}
		if d.present["targeting"] {
			p.add([]any{"targeting"}, "'targeting' is only supported for app surveys (type: 'app')", v3api.CodeUnsupportedField)
		}
		return
	}
	if dist := d.Distribution; dist != nil && dist.DisplayOption == "displaySome" && (dist.DisplayLimit == nil || *dist.DisplayLimit < 1) {
		p.add([]any{"distribution", "displayLimit"}, "displayLimit must be at least 1 when displayOption is 'displaySome'", v3api.CodeMissingRequiredField)
	}
}

// setDefault fills a key zod's prefault would have filled.
func setDefault(o *object, key string, v any) {
	if o == nil {
		return
	}
	if _, ok := o.get(key); !ok {
		o.set(key, v)
	}
}

// applyDocumentDefaults stores what zod's parse output carried: the defaults of the welcome card, elements, validation and variables.
func applyDocumentDefaults(d *v3Document) {
	wc := asObject(d.WelcomeCard)
	setDefault(wc, "timeToFinish", true)
	setDefault(wc, "showResponseCount", false)
	blocks, _ := d.Blocks.([]any)
	for _, b := range blocks {
		bo := asObject(b)
		if bo == nil {
			continue
		}
		elements, _ := bo.values["elements"].([]any)
		for _, e := range elements {
			el := asObject(e)
			if el == nil {
				continue
			}
			switch el.values["type"] {
			case "openText":
				setDefault(el, "inputType", "text")
				setDefault(el, "charLimit", func() any { o := newObject(); o.set("enabled", false); return o }())
			case "nps", "rating", "csat", "ces":
				setDefault(el, "isColorCodingEnabled", false)
			case "cta":
				setDefault(el, "buttonExternal", false)
			case "pictureSelection":
				setDefault(el, "allowMulti", false)
			case "matrix":
				setDefault(el, "shuffleOption", "none")
			}
			if v := asObject(el.values["validation"]); v != nil {
				setDefault(v, "logic", "and")
			}
		}
	}
	vars, _ := d.Variables.([]any)
	for _, v := range vars {
		vo := asObject(v)
		if vo == nil {
			continue
		}
		switch vo.values["type"] {
		case "number":
			setDefault(vo, "value", json.Number("0"))
		case "text":
			setDefault(vo, "value", "")
		}
	}
}
