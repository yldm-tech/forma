package mgmtapi

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/apps/server/internal/platform/v1api"
)

// forma2 serialized every Date with toISOString: UTC with exactly three fractional digits. The rows the management APIs return carry time.Time values that encoding/json would print with up to nine digits, so every answer passes through jsView.

// timestampKeys are the members that hold a row's Date values.
var timestampKeys = map[string]bool{
	"createdAt": true, "updatedAt": true, "publishOn": true, "closeOn": true, "archivedAt": true,
	"lastLoginAt": true, "valueDate": true, "emailVerified": true,
}

// opaqueKeys are the members that hold a caller's or respondent's own JSON, which is never rewritten.
var opaqueKeys = map[string]bool{
	"data": true, "variables": true, "ttc": true, "meta": true, "contactAttributes": true, "attributes": true,
	"blocks": true, "questions": true, "endings": true, "welcomeCard": true, "hiddenFields": true, "styling": true, "metadata": true,
	"surveyClosedMessage": true, "singleUse": true, "recaptcha": true, "workspaceOverwrites": true, "projectOverwrites": true,
	"inlineTriggers": true, "noCodeConfig": true, "filters": true, "trigger": true, "action": true, "notificationSettings": true,
}

// jsView is v as forma2 would have encoded it: its timestamps as toISOString wrote them.
func jsView(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var decoded any
	if err := dec.Decode(&decoded); err != nil {
		return v
	}
	return isoTimestamps(decoded)
}

func isoTimestamps(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			switch {
			case opaqueKeys[k]:
			case timestampKeys[k]:
				if s, ok := e.(string); ok {
					if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
						x[k] = jsvalue.ISOString(t)
					}
				}
			default:
				x[k] = isoTimestamps(e)
			}
		}
	case []any:
		for i, e := range x {
			x[i] = isoTimestamps(e)
		}
	}
	return v
}

// v1OK is v1api.Success with forma2's timestamps.
func v1OK(c *gin.Context, data any) {
	v1api.Success(c, jsView(data))
}
