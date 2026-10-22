package survey

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/yldm-tech/forma/server/internal/platform/v3api"
	"github.com/yldm-tech/forma/server/internal/survey/surveydoc"
)

// fieldLanguageMarker is the editor-only delimiter forma2's label rules put before a language list; v3 reasons drop it.
const fieldLanguageMarker = "-fLang-"

// invalidParamReason is forma2's toV3InvalidParamReason.
func invalidParamReason(message string) string {
	if !strings.Contains(message, fieldLanguageMarker) {
		return message
	}
	segments := strings.Split(message, fieldLanguageMarker)
	for i, s := range segments {
		segments[i] = strings.TrimSpace(s)
	}
	return strings.Join(segments, " ")
}

// invalidParams is forma2's formatV3ZodInvalidParams: one param per issue, named by its path or the fallback.
func invalidParams(issues []surveydoc.Issue, fallback string) []v3api.InvalidParam {
	out := make([]v3api.InvalidParam, len(issues))
	for i, is := range issues {
		name := is.Name()
		if name == "" {
			name = fallback
		}
		out[i] = v3api.InvalidParam{Name: name, Reason: invalidParamReason(is.Message), Code: is.Code}
	}
	return out
}

func indexPath(base string, i int) string {
	if base == "" {
		return strconv.Itoa(i)
	}
	return base + "." + strconv.Itoa(i)
}

// unsupportedLocales is forma2's addUnsupportedLocaleIssues: every key of an internal translatable map must be "default" or a declared language.
func unsupportedLocales(v any, path string, declared map[string]bool, out *[]v3api.InvalidParam) {
	switch x := v.(type) {
	case []any:
		for i, e := range x {
			unsupportedLocales(e, indexPath(path, i), declared, out)
		}
	case *object:
		if isInternalI18n(x) {
			for _, code := range x.keys {
				if code != "default" && !declared[strings.ToLower(code)] {
					*out = append(*out, v3api.InvalidParam{
						Name: path + "." + code, Reason: fmt.Sprintf("Language '%s' must be declared in languages before it can be used in translatable content", code),
						Code: v3api.CodeUnsupportedLocale, Identifier: code, ReferenceType: "language",
					})
				}
			}
			return
		}
		for _, k := range x.keys {
			unsupportedLocales(x.values[k], joinPath(path, k), declared, out)
		}
	}
}

// missingTranslations is forma2's addMissingTranslationIssues: every internal translatable map must carry each configured non-default language.
func missingTranslations(v any, path string, codes []string, out *[]v3api.InvalidParam) {
	if len(codes) == 0 {
		return
	}
	switch x := v.(type) {
	case []any:
		for i, e := range x {
			missingTranslations(e, indexPath(path, i), codes, out)
		}
	case *object:
		if isInternalI18n(x) {
			for _, code := range codes {
				if _, ok := x.get(code); !ok {
					*out = append(*out, v3api.InvalidParam{
						Name: path, Reason: fmt.Sprintf("Translatable field is missing configured language '%s'", code),
						Code: v3api.CodeMissingTranslation, Identifier: code, ReferenceType: "language", MissingID: code,
					})
				}
			}
			return
		}
		for _, k := range x.keys {
			missingTranslations(x.values[k], joinPath(path, k), codes, out)
		}
	}
}

// languageParams is forma2's getV3SurveyLanguageInvalidParams.
func languageParams(d *v3Document) []v3api.InvalidParam {
	declared := map[string]bool{strings.ToLower(d.DefaultLanguage): true}
	var configured []string
	seen := map[string]bool{}
	for _, l := range d.Languages {
		declared[strings.ToLower(l.Code)] = true
		if !strings.EqualFold(l.Code, d.DefaultLanguage) && !seen[l.Code] {
			seen[l.Code] = true
			configured = append(configured, l.Code)
		}
	}
	var out []v3api.InvalidParam
	meta := asObject(d.Metadata)
	for _, key := range metadataI18nKeys {
		v, _ := meta.get(key)
		unsupportedLocales(v, "metadata."+key, declared, &out)
	}
	unsupportedLocales(d.WelcomeCard, "welcomeCard", declared, &out)
	unsupportedLocales(d.Blocks, "blocks", declared, &out)
	unsupportedLocales(d.Endings, "endings", declared, &out)
	for _, key := range metadataI18nKeys {
		v, _ := meta.get(key)
		missingTranslations(v, "metadata."+key, configured, &out)
	}
	missingTranslations(d.WelcomeCard, "welcomeCard", configured, &out)
	missingTranslations(d.Blocks, "blocks", configured, &out)
	missingTranslations(d.Endings, "endings", configured, &out)
	return out
}

type namedRef struct {
	id, path, namespace string
}

var recallPattern = regexp.MustCompile(`#recall:([A-Za-z0-9_-]+)`)

// referenceParams is forma2's getV3SurveyReferenceInvalidParams: duplicate and colliding identifiers, logic and fallbacks that point nowhere, and recall tokens that name nothing.
func referenceParams(d *v3Document, s *surveydoc.Survey) []v3api.InvalidParam {
	var out []v3api.InvalidParam
	var blocks, endings, elements, hidden, varIDs, varNames []namedRef
	for i, b := range s.Blocks {
		blocks = append(blocks, namedRef{b.ID, fmt.Sprintf("blocks.%d.id", i), "block"})
		for e, el := range b.Elements {
			elements = append(elements, namedRef{el.ID, fmt.Sprintf("blocks.%d.elements.%d.id", i, e), "element"})
		}
	}
	for i, e := range s.Endings {
		endings = append(endings, namedRef{e.ID, fmt.Sprintf("endings.%d.id", i), "ending"})
	}
	for i, id := range s.HiddenFields.FieldIDs {
		hidden = append(hidden, namedRef{id, fmt.Sprintf("hiddenFields.fieldIds.%d", i), "hiddenField"})
	}
	for i, v := range s.Variables {
		varIDs = append(varIDs, namedRef{v.ID, fmt.Sprintf("variables.%d.id", i), "variable"})
		varNames = append(varNames, namedRef{v.Name, fmt.Sprintf("variables.%d.name", i), "variableName"})
	}
	duplicates := func(refs []namedRef, label, refType string, value bool) {
		first := map[string]string{}
		for _, r := range refs {
			if p, ok := first[r.id]; ok {
				reason := fmt.Sprintf("%s id '%s' is duplicated; first used at %s", label, r.id, p)
				if value {
					reason = fmt.Sprintf("%s '%s' is duplicated; first used at %s", label, r.id, p)
				}
				out = append(out, v3api.InvalidParam{Name: r.path, Reason: reason, Code: v3api.CodeDuplicateIdentifier, Identifier: r.id, ReferenceType: refType, FirstUsedAt: p})
				continue
			}
			first[r.id] = r.path
		}
	}
	duplicates(blocks, "Block", "block", false)
	duplicates(endings, "Ending", "ending", false)
	duplicates(elements, "Element", "element", false)
	duplicates(varIDs, "Variable", "variable", false)
	duplicates(hidden, "Hidden field id", "hiddenField", true)
	duplicates(varNames, "Variable name", "variableName", true)

	firstByID := map[string]namedRef{}
	all := append(append(append(append(append(append([]namedRef{}, blocks...), elements...), endings...), hidden...), varIDs...), varNames...)
	for _, r := range all {
		key := strings.ToLower(r.id)
		f, ok := firstByID[key]
		if !ok {
			firstByID[key] = r
			continue
		}
		if f.namespace == r.namespace {
			continue
		}
		out = append(out, v3api.InvalidParam{
			Name: r.path, Reason: fmt.Sprintf("%s identifier '%s' conflicts with %s identifier at %s", r.namespace, r.id, f.namespace, f.path),
			Code: v3api.CodeDuplicateIdentifier, Identifier: r.id, ReferenceType: r.namespace, ConflictsWith: f.path,
		})
	}

	elementIDs, variableIDs, hiddenIDs, targets := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]string{}
	for _, r := range elements {
		elementIDs[r.id] = true
	}
	for _, r := range varIDs {
		variableIDs[r.id] = true
	}
	for _, r := range hidden {
		hiddenIDs[r.id] = true
	}
	for _, r := range blocks {
		targets[r.id] = "block"
	}
	for _, r := range endings {
		targets[r.id] = "ending"
	}
	dangling := func(name, reason, id, refType string) {
		out = append(out, v3api.InvalidParam{Name: name, Reason: reason, Code: v3api.CodeDanglingReference, Identifier: id, ReferenceType: refType, MissingID: id})
	}
	operand := func(o *surveydoc.Operand, path string) {
		if o == nil {
			return
		}
		value, _ := o.String()
		switch {
		case o.Type == "element" && !elementIDs[value]:
			dangling(path+".value", fmt.Sprintf("Element id '%s' is not defined in blocks", value), value, "element")
		case o.Type == "variable" && !variableIDs[value]:
			dangling(path+".value", fmt.Sprintf("Variable id '%s' is not defined in variables", value), value, "variable")
		case o.Type == "hiddenField" && !hiddenIDs[value]:
			dangling(path+".value", fmt.Sprintf("Hidden field id '%s' is not defined in hiddenFields.fieldIds", value), value, "hiddenField")
		}
	}
	var group func(c *surveydoc.Condition, path string)
	group = func(c *surveydoc.Condition, path string) {
		for i := range c.Conditions {
			cp := fmt.Sprintf("%s.conditions.%d", path, i)
			cond := &c.Conditions[i]
			if cond.IsGroup {
				group(cond, cp)
				continue
			}
			operand(cond.LeftOperand, cp+".leftOperand")
			if cond.RightOperand != nil && cond.RightOperand.Type != "" && cond.RightOperand.Type != "static" {
				operand(cond.RightOperand, cp+".rightOperand")
			}
		}
	}
	for i, b := range s.Blocks {
		fp := fmt.Sprintf("blocks.%d.logicFallback", i)
		if b.LogicFallback != "" && len(b.Logic) == 0 {
			refType := targets[b.LogicFallback]
			if refType == "" {
				refType = "block"
			}
			out = append(out, v3api.InvalidParam{Name: fp, Reason: "logicFallback requires at least one logic rule on the same block; omit logicFallback for normal sequential flow or add blocks[].logic", Code: v3api.CodeInvalidReference, Identifier: b.LogicFallback, ReferenceType: refType})
		}
		if b.LogicFallback != "" && b.LogicFallback == b.ID {
			out = append(out, v3api.InvalidParam{Name: fp, Reason: "logicFallback cannot target the same block", Code: v3api.CodeInvalidReference, Identifier: b.LogicFallback, ReferenceType: "block"})
		}
		if _, ok := targets[b.LogicFallback]; b.LogicFallback != "" && !ok {
			dangling(fp, fmt.Sprintf("Logic fallback target '%s' is not defined in blocks or endings", b.LogicFallback), b.LogicFallback, "block")
		}
		for l := range b.Logic {
			lp := fmt.Sprintf("blocks.%d.logic.%d", i, l)
			group(&b.Logic[l].Conditions, lp+".conditions")
			for a, act := range b.Logic[l].Actions {
				ap := fmt.Sprintf("%s.actions.%d", lp, a)
				switch act.Objective {
				case "calculate":
					if !variableIDs[act.VariableID] {
						dangling(ap+".variableId", fmt.Sprintf("Variable id '%s' is not defined in variables", act.VariableID), act.VariableID, "variable")
					}
					if act.Value != nil && act.Value.Type != "static" {
						operand(act.Value, ap+".value")
					}
				case "requireAnswer":
					if !elementIDs[act.Target] {
						dangling(ap+".target", fmt.Sprintf("Element id '%s' is not defined in blocks", act.Target), act.Target, "element")
					}
				case "jumpToBlock":
					if _, ok := targets[act.Target]; !ok {
						dangling(ap+".target", fmt.Sprintf("Jump target '%s' is not defined in blocks or endings", act.Target), act.Target, "block")
					}
				}
			}
		}
	}
	var recall func(v any, path string)
	recall = func(v any, path string) {
		switch x := v.(type) {
		case string:
			for _, m := range recallPattern.FindAllStringSubmatch(x, -1) {
				id := m[1]
				if !elementIDs[id] && !variableIDs[id] && !hiddenIDs[id] {
					out = append(out, v3api.InvalidParam{Name: path, Reason: fmt.Sprintf("Recall reference '%s' is not defined in blocks, variables, or hiddenFields.fieldIds", id), Code: v3api.CodeDanglingReference, Identifier: id, ReferenceType: "recall", MissingID: id})
				}
			}
		case []any:
			for i, e := range x {
				recall(e, indexPath(path, i))
			}
		case *object:
			for _, k := range x.keys {
				recall(x.values[k], joinPath(path, k))
			}
		}
	}
	recall(d.Blocks, "blocks")
	recall(d.Endings, "endings")
	recall(d.WelcomeCard, "welcomeCard")
	if meta := asObject(d.Metadata); meta != nil {
		title, _ := meta.get("title")
		recall(title, "metadata.title")
		description, _ := meta.get("description")
		recall(description, "metadata.description")
	}
	return out
}

// validateDocument is forma2's validateV3SurveyDocument: undeclared and missing translations, media, then references. All are semantic failures (422).
func validateDocument(d *v3Document) ([]v3api.InvalidParam, *surveydoc.Survey, error) {
	params := languageParams(d)
	s, err := d.typed()
	if err != nil {
		return nil, nil, err
	}
	if m := surveydoc.MediaIssue(s.Blocks); m != "" {
		params = append(params, v3api.InvalidParam{Name: "blocks", Reason: m})
	}
	params = append(params, referenceParams(d, s)...)
	return params, s, nil
}
