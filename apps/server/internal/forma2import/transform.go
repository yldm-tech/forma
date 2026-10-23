package forma2import

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/yldm-tech/forma/apps/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
	"github.com/yldm-tech/forma/apps/server/internal/survey"
)

// resolveString is the new id of a forma2 id, as the string this server compares.
func resolveString(old string) string {
	resolved, ok := legacyid.Resolve(old)
	if !ok {
		return old
	}
	return resolved.String()
}

// decode reads JSON keeping numbers exactly as written, so a document written back is the same document.
func decode(raw []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	return d.Decode(v)
}

// rewriteNotificationSettings rewrites User.notificationSettings for this server, which compares UUID strings: every key of alert is a survey id and every element of unsubscribedOrganizationIds an organization id. Other keys are kept as they are.
func rewriteNotificationSettings(raw string) (string, error) {
	doc := map[string]json.RawMessage{}
	if err := decode([]byte(raw), &doc); err != nil {
		return "", fmt.Errorf("notification settings are not a JSON object: %w", err)
	}
	if v, ok := doc["alert"]; ok && string(v) != "null" {
		alerts := map[string]json.RawMessage{}
		if err := decode(v, &alerts); err != nil {
			return "", fmt.Errorf("notification settings: alert is not an object: %w", err)
		}
		out := make(map[string]json.RawMessage, len(alerts))
		for surveyID, on := range alerts {
			out[resolveString(surveyID)] = on
		}
		b, err := json.Marshal(out)
		if err != nil {
			return "", err
		}
		doc["alert"] = b
	}
	if v, ok := doc["unsubscribedOrganizationIds"]; ok && string(v) != "null" {
		var ids []string
		if err := decode(v, &ids); err != nil {
			return "", fmt.Errorf("notification settings: unsubscribedOrganizationIds is not a list of ids: %w", err)
		}
		for i, orgID := range ids {
			ids[i] = resolveString(orgID)
		}
		b, err := json.Marshal(ids)
		if err != nil {
			return "", err
		}
		doc["unsubscribedOrganizationIds"] = b
	}
	b, err := json.Marshal(doc)
	return string(b), err
}

// rewriteSegmentFilters rewrites a segment's filter tree for this server, which parses the ids in it as UUIDs: a segment filter's root.segmentId (and its value, which repeats it) and a survey-interaction filter's value.surveyIds. Attribute filters name their key and node ids are the tree's own, so both stay.
func rewriteSegmentFilters(raw string) (string, error) {
	var tree any
	if err := decode([]byte(raw), &tree); err != nil {
		return "", fmt.Errorf("segment filters are not JSON: %w", err)
	}
	nodes, ok := tree.([]any)
	if !ok {
		return "", errors.New("segment filters are not a list")
	}
	rewriteFilterNodes(nodes)
	b, err := json.Marshal(nodes)
	return string(b), err
}

func rewriteFilterNodes(nodes []any) {
	for _, n := range nodes {
		node, ok := n.(map[string]any)
		if !ok {
			continue
		}
		switch resource := node["resource"].(type) {
		case []any:
			rewriteFilterNodes(resource)
		case map[string]any:
			if root, ok := resource["root"].(map[string]any); ok && root["type"] == "segment" {
				if id, ok := root["segmentId"].(string); ok && id != "" {
					root["segmentId"] = resolveString(id)
					// forma2's editor wrote the referenced segment's id as the filter's value too.
					if v, ok := resource["value"].(string); ok && v == id {
						resource["value"] = resolveString(id)
					}
				}
			}
			if value, ok := resource["value"].(map[string]any); ok {
				if ids, ok := value["surveyIds"].([]any); ok {
					for i, v := range ids {
						if s, ok := v.(string); ok && s != "" {
							ids[i] = resolveString(s)
						}
					}
				}
			}
		}
	}
}

// questionsToBlocks is forma2's transformQuestionsToBlocks for a survey that still has only its legacy questions. The conversion names every block with a random id; those are replaced with ids derived from the survey and the question, so a second run writes the same document.
func questionsToBlocks(surveyID, questionsRaw, endingsRaw string) (string, error) {
	var questions []any
	if err := decode([]byte(questionsRaw), &questions); err != nil {
		return "", fmt.Errorf("questions are not a list: %w", err)
	}
	var endings any
	if err := decode([]byte(endingsRaw), &endings); err != nil {
		return "", fmt.Errorf("endings are not JSON: %w", err)
	}
	blocks := survey.QuestionsToBlocks(questions, endings)
	raw, err := json.Marshal(blocks)
	if err != nil {
		return "", err
	}
	for i, b := range blocks {
		block, ok := b.(map[string]any)
		if !ok {
			return "", errors.New("questions converted to something other than blocks")
		}
		random, _ := block["id"].(string)
		if random == "" {
			continue
		}
		questionID := fmt.Sprint(i)
		if elements, ok := block["elements"].([]any); ok && len(elements) > 0 {
			if e, ok := elements[0].(map[string]any); ok {
				if id, ok := e["id"].(string); ok {
					questionID = id
				}
			}
		}
		raw = bytes.ReplaceAll(raw, []byte(`"`+random+`"`), []byte(`"`+blockID(surveyID, questionID)+`"`))
	}
	return string(raw), nil
}

// blockID is a cuid2-shaped id (a letter, then 23 letters or digits, as surveydoc.NewID makes them) derived from the survey and question a converted block holds.
func blockID(surveyID, questionID string) string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	sum := sha256.Sum256([]byte("forma2-block\x00" + surveyID + "\x00" + questionID))
	out := make([]byte, 24)
	out[0] = letters[int(sum[0])%len(letters)]
	for i := 1; i < len(out); i++ {
		out[i] = alphabet[int(sum[i])%len(alphabet)]
	}
	return string(out)
}

// integrationLinks is what integrations.config keeps of forma2's Integration.config: the survey links (config.data). The credentials beside them move to integration_credentials.
func integrationLinks(raw string) (string, error) {
	var config map[string]json.RawMessage
	if err := decode([]byte(raw), &config); err != nil {
		return "", fmt.Errorf("integration config is not a JSON object: %w", err)
	}
	data := config["data"]
	if len(data) == 0 || string(data) == "null" {
		data = json.RawMessage("[]")
	}
	b, err := json.Marshal(map[string]json.RawMessage{"data": data})
	return string(b), err
}

// The fields of an integration's token set forma2 encrypted at rest (apps/web/lib/integration/redact-credentials.ts isSecretKeyField): access_token and refresh_token always, and any field named like a secret.
var secretKeyFieldPattern = regexp.MustCompile(`(?i)(^token$|_token$|secret|password|credential|private_key|api_?key)`)

func isSecretKeyField(field string) bool {
	return field == "access_token" || field == "refresh_token" || secretKeyFieldPattern.MatchString(field)
}

var hexOnly = regexp.MustCompile(`^[0-9a-fA-F]*$`)

type credentialForm int

const (
	cleartext credentialForm = iota
	gcm
	legacyCBC
)

// classifyCredential is forma2's classifyCredential (apps/web/lib/integration/credential-encryption.ts): a 32-hex IV and a GCM tag make "iv:ct:tag", a 32-hex IV and one more hex part the pre-GCM CBC form, and anything else, every token the providers issue included, is cleartext.
func classifyCredential(v string) credentialForm {
	parts := strings.Split(v, ":")
	if len(parts[0]) != 32 || !hexOnly.MatchString(parts[0]) {
		return cleartext
	}
	if len(parts) == 3 && hexOnly.MatchString(parts[1]) && len(parts[2]) == 32 && hexOnly.MatchString(parts[2]) {
		return gcm
	}
	if len(parts) == 2 && parts[1] != "" && hexOnly.MatchString(parts[1]) {
		return legacyCBC
	}
	return cleartext
}

// decryptCredentialKey is forma2's decryptIntegrationCredentials for config.key: every secret string field in plaintext. A field that does not decrypt is left as stored, as forma2 left it (it then fails at the provider until the integration is reconnected); undecryptable names those fields.
func decryptCredentialKey(raw string, forma2Key []byte) (key json.RawMessage, undecryptable []string, err error) {
	fields := map[string]json.RawMessage{}
	if err := decode([]byte(raw), &fields); err != nil {
		return nil, nil, fmt.Errorf("integration key is not a JSON object: %w", err)
	}
	for name, v := range fields {
		var s string
		if !isSecretKeyField(name) || json.Unmarshal(v, &s) != nil {
			continue
		}
		form := classifyCredential(s)
		if form == cleartext {
			continue
		}
		plain, err := secret.DecryptForma2(s, forma2Key, form == legacyCBC)
		if err != nil {
			undecryptable = append(undecryptable, name)
			continue
		}
		if fields[name], err = json.Marshal(plain); err != nil {
			return nil, nil, err
		}
	}
	b, err := json.Marshal(fields)
	return b, undecryptable, err
}

// sameJSON reports whether two JSON documents are equal as values; numbers compare by value, so 1.0 equals 1.
func sameJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}
