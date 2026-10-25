package clientapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/yldm-tech/forma/apps/server/internal/contact"
	"github.com/yldm-tech/forma/apps/server/internal/langcode"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
)

const (
	// userStateExpiry is when the SDK is told to identify again.
	userStateExpiry = 30 * time.Minute
	// languagesTTL is how long a workspace's languages are cached for resolving a language alias, as long as the state that serves them.
	languagesTTL = time.Minute
)

// segmentDevice is the device a segment's device filter is evaluated for on this identify, read from the request's User-Agent. A recognised device type decides it: a phone for a mobile one, a desktop for any other, so tablets count as desktops for now, as do consoles, TVs and headsets, since the filter offers only phone and desktop. An unrecognised User-Agent is a desktop when it comes from a browser (every browser's starts with "Mozilla/") or is missing, and a phone otherwise: native apps call through their platform's HTTP client, whose User-Agent names no device ("MyApp/1 CFNetwork/1568 Darwin/24.0.0" on iOS, "okhttp/4.12.0" or "Dalvik/2.1.0 (Linux; U; Android 14; ...)" on Android), and the iOS, Android and React Native SDKs run on phones. forma2 always chose phone (its `device ? "phone" : "desktop"` tested an object that is never empty), so a desktop visitor matched phone segments and never desktop ones.
func segmentDevice(userAgent string) string {
	switch deviceType(userAgent) {
	case "mobile":
		return contact.DevicePhone
	case "":
		if userAgent != "" && !strings.HasPrefix(userAgent, "Mozilla/") {
			return contact.DevicePhone
		}
	}
	return contact.DeviceDesktop
}

// existenceRevealingMessages are withheld from the public identify: they would tell anyone holding the public workspace id whether an address or user id is a contact there.
var existenceRevealingMessages = map[string]bool{"email_already_exists": true, "userid_already_exists": true}

// identify is forma2's client user route (v1 and v2), the SDK's setUserId and setAttributes: the contact with that userId, created on first sight, its attributes merged with the ones sent, and its state for the SDK's targeting: the segments it is in, its displays and responses, and its language in the legacy form older SDKs match.
func (h *Handler) identify(c *gin.Context) {
	ctx := c.Request.Context()
	raw := jsTrim(c.Param("workspaceId"))
	if !isID(raw) {
		writeError(c, badRequest("Invalid workspace ID format", nil))
		return
	}
	ws, ok := h.workspace(c, raw, msgUserState)
	if !ok {
		return
	}
	body, text, err := parseBody(c.Request)
	if err != nil {
		h.bodyError(c, err, msgMalformedJSON)
		return
	}
	obj, isObject := body.(map[string]any)
	userID, _ := obj["userId"].(string)
	if !isObject || userID == "" {
		writeError(c, badRequest("userId is required and must be a string", nil))
		return
	}
	var attrs []jsvalue.Member
	if a, ok := obj["attributes"]; ok && a != nil {
		switch list := a.(type) {
		case map[string]any:
			members, err := jsvalue.DecodeObject(rawMember(text, "attributes"))
			if err != nil {
				h.fail(c, err, msgUserState)
				return
			}
			attrs = members
		case []any:
			// An array is an object too: its indexes are its keys.
			for i, v := range list {
				attrs = append(attrs, jsvalue.Member{Key: fmt.Sprint(i), Value: v})
			}
		}
		for _, m := range attrs {
			if m.Key != "email" {
				continue
			}
			email, isString := m.Value.(string)
			if !isString || !strings.Contains(email, "@") || len(utf16.Encode([]rune(email))) < 3 {
				writeError(c, badRequest("Invalid email format", nil))
				return
			}
		}
		kept := attrs[:0]
		for _, m := range attrs {
			if m.Key != "userId" && m.Key != "id" {
				kept = append(kept, m)
			}
		}
		attrs = kept
	}
	state, err := h.updateUser(ctx, ws.ID, userID, attrs, segmentDevice(c.Request.UserAgent()))
	if err != nil {
		h.fail(c, err, msgUserState)
		return
	}
	writeData(c, state, noStore)
}

// updateUser is forma2's updateUser: it answers {state, messages?, errors?}, the state's segments evaluated for device.
func (h *Handler) updateUser(ctx context.Context, workspaceID uuid.UUID, userID string, attrs []jsvalue.Member, device string) (fields, error) {
	found, err := h.contacts.FindOrCreateByUserID(ctx, workspaceID, userID)
	if err != nil {
		return nil, err
	}
	language, hasLanguage := found.Attributes["language"]

	// The contact's language is stored in its canonical form (de becomes de-DE) or as a language alias the workspace configured; anything else is dropped, and a value that was not blank is reported.
	var dropped *string
	normalized := attrs
	if i := memberIndex(attrs, "language"); i >= 0 {
		text := jsvalue.String(attrs[i].Value)
		trimmed := jsTrim(text)
		value := ""
		if trimmed != "" {
			value = langcode.Normalize(text)
			if value == "" {
				if value, err = h.workspaceLanguage(ctx, workspaceID, trimmed); err != nil {
					return nil, err
				}
			}
		}
		normalized = append([]jsvalue.Member(nil), attrs...)
		if value != "" {
			normalized[i].Value = value
		} else {
			normalized = append(normalized[:i], normalized[i+1:]...)
			if trimmed != "" {
				dropped = &text
			}
		}
	}

	var messages, errs []string
	if len(normalized) > 0 && changes(normalized, found.Attributes) {
		in, err := attributeInputs(normalized)
		if err != nil {
			return nil, err
		}
		updateMessages, updateErrors, err := h.contacts.IdentifyAttributes(ctx, workspaceID, found.ID, in)
		if err != nil {
			return nil, err
		}
		for _, m := range updateMessages {
			if !existenceRevealingMessages[m.Code] {
				messages = append(messages, m.Text())
			}
		}
		for _, m := range updateErrors {
			errs = append(errs, m.Text())
		}
		if i := memberIndex(normalized, "language"); i >= 0 {
			language, hasLanguage = jsvalue.String(normalized[i].Value), true
		}
	}
	if dropped != nil {
		messages = append(messages, contact.Message{Code: "invalid_language_ignored", Params: map[string]string{"language": *dropped}}.Text())
	}

	data, err := h.userState(ctx, workspaceID, found.ID, userID, device)
	if err != nil {
		return nil, err
	}
	if hasLanguage && language != "" {
		echo := language
		if canonical := langcode.Normalize(language); canonical != "" {
			echo = canonical
			if legacy := langcode.LegacyCodes(canonical); len(legacy) > 0 {
				echo = legacy[0]
			}
		}
		data = append(data, field{"language", echo})
	}
	out := fields{{"state", fields{{"data", data}, {"expiresAt", jsvalue.ISOString(h.now().Add(userStateExpiry))}}}}
	if len(messages) > 0 {
		out = append(out, field{"messages", messages})
	}
	if len(errs) > 0 {
		out = append(out, field{"errors", errs})
	}
	return out, nil
}

func memberIndex(members []jsvalue.Member, key string) int {
	for i, m := range members {
		if m.Key == key {
			return i
		}
	}
	return -1
}

// changes is forma2's check before writing: some value differs, with ===, from the stored text, so a number or boolean always counts as a change.
func changes(attrs []jsvalue.Member, stored map[string]string) bool {
	for _, m := range attrs {
		current, ok := stored[m.Key]
		s, isString := m.Value.(string)
		if !ok || !isString || s != current {
			return true
		}
	}
	return false
}

// attributeInputs is ZContactAttributesInput: every value a string, a number or a boolean (sent on as its text). Anything else fails as forma2's validateInputs failed, "Validation failed: " followed by each key and zod's message.
func attributeInputs(attrs []jsvalue.Member) ([]contact.AttributeInput, error) {
	var bad []string
	out := make([]contact.AttributeInput, 0, len(attrs))
	for _, m := range attrs {
		switch v := m.Value.(type) {
		case string:
			out = append(out, contact.AttributeInput{Key: m.Key, Value: contact.Value{String: v}})
		case float64:
			out = append(out, contact.AttributeInput{Key: m.Key, Value: contact.Value{Number: v, IsNumber: true}})
		case bool:
			out = append(out, contact.AttributeInput{Key: m.Key, Value: contact.Value{String: fmt.Sprint(v)}})
		default:
			bad = append(bad, m.Key+"Invalid input")
		}
	}
	if len(bad) > 0 {
		return nil, badRequest("Validation failed: "+strings.Join(bad, "; "), nil)
	}
	return out, nil
}

// workspaceLanguage is forma2's resolveWorkspaceLanguageIdentifier: a language code or alias the workspace configured, matched case-insensitively and returned as configured (the alias when the value is the alias), or "" for none. The workspace's languages are cached for a minute.
func (h *Handler) workspaceLanguage(ctx context.Context, workspaceID uuid.UUID, value string) (string, error) {
	type language struct {
		Code  string  `json:"code"`
		Alias *string `json:"alias"`
	}
	var langs []language
	key := "fb:env:" + workspaceID.String() + ":languages"
	b, err := h.cache.Get(ctx, key).Bytes()
	if err != nil || json.Unmarshal(b, &langs) != nil {
		if err != nil && !errors.Is(err, redis.Nil) {
			h.log.WarnContext(ctx, "workspace languages cache read failed", "err", err)
		}
		langs = nil
		if err := h.db.WithContext(ctx).Raw(`SELECT code, alias FROM languages WHERE workspace_id = ? ORDER BY created_at, id`, workspaceID).Scan(&langs).Error; err != nil {
			return "", fmt.Errorf("load workspace languages: %w", err)
		}
		if b, err := json.Marshal(langs); err == nil {
			if err := h.cache.Set(ctx, key, b, languagesTTL).Err(); err != nil {
				h.log.WarnContext(ctx, "workspace languages cache write failed", "err", err)
			}
		}
	}
	target := strings.ToLower(value)
	for _, l := range langs {
		aliasMatch := l.Alias != nil && strings.ToLower(*l.Alias) == target
		if strings.ToLower(l.Code) == target || aliasMatch {
			if aliasMatch {
				return *l.Alias, nil
			}
			return l.Code, nil
		}
	}
	return "", nil
}

// userState is the identified contact's targeting data: the segments it is in on device (none when they cannot be evaluated), its displays newest first, the surveys it responded to, and when it was last shown a survey.
func (h *Handler) userState(ctx context.Context, workspaceID, contactID uuid.UUID, userID, device string) (fields, error) {
	db := h.db.WithContext(ctx)
	var displays []contact.DisplayRow
	if err := db.Raw(`SELECT survey_id, created_at FROM displays WHERE contact_id = ? ORDER BY created_at DESC, id DESC`, contactID).Scan(&displays).Error; err != nil {
		return nil, fmt.Errorf("load contact displays: %w", err)
	}
	var responses []contact.ResponseRow
	if err := db.Raw(`SELECT survey_id, created_at, finished FROM responses WHERE contact_id = ? ORDER BY created_at, id`, contactID).Scan(&responses).Error; err != nil {
		return nil, fmt.Errorf("load contact responses: %w", err)
	}
	segments, err := h.segments.SegmentIDsForContact(ctx, workspaceID, contactID, device, contact.Interactions{Displays: displays, Responses: responses})
	if err != nil {
		h.log.WarnContext(ctx, "Failed to get person segment IDs, returning empty array", "workspaceId", workspaceID, "contactId", contactID, "err", err)
		segments = []uuid.UUID{}
	}
	displayRefs := make([]any, len(displays))
	for i, d := range displays {
		displayRefs[i] = fields{{"surveyId", d.SurveyID}, {"createdAt", jsvalue.ISOString(d.CreatedAt)}}
	}
	responded := make([]uuid.UUID, len(responses))
	for i, r := range responses {
		responded[i] = r.SurveyID
	}
	var lastDisplayAt any
	if len(displays) > 0 {
		lastDisplayAt = jsvalue.ISOString(displays[0].CreatedAt)
	}
	return fields{
		{"contactId", contactID}, {"userId", userID}, {"segments", segments}, {"displays", displayRefs},
		{"responses", responded}, {"lastDisplayAt", lastDisplayAt},
	}, nil
}
