package forma2import

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/yldm-tech/forma/server/internal/auth"
	"github.com/yldm-tech/forma/server/internal/integration"
	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
)

// The sample check does not trust the mapping in tables.go. It reads a sampled row's forma2 row again, whole (to_jsonb of the row, every stored column under forma2's own name), in a query of its own, and works out from it what every written column must hold:
//
//   - a column of this schema holds the forma2 column of the same name, compared without case and underscores (display_retention_days is "displayRetentionDays"), or the one an expectation renames it to; a forma2 id becomes legacyid.Resolve of it and a timestamp, stored without a zone, is read as UTC;
//   - a column computed by the mapping (a transform, a fallback, a decrypted and sealed secret) is computed again here, by an expectation, written apart from the mapping;
//   - a column with neither fails the check, so a new column cannot go unchecked.
//
// The transforms of transform.go that rewrite whole documents (segment filters, legacy questions to blocks) and forma2's own credential decryption are applied again to the row read here, so the check proves they received forma2's stored value and that their output is what was written; their own logic is covered by their unit tests.

// expectation is how the sample check finds and reads a table spec's forma2 row again, and what it computes on its own.
type expectation struct {
	// table is the forma2 table the row is read from; empty means the spec's.
	table string
	// locate is the condition (on the table, aliased t) that finds the row from the sample's forma2 key values; nil matches the spec's key columns to forma2's by name.
	locate func(forma2Key []string) (where string, args []any)
	// rename maps a column of this schema to the forma2 column it holds under another name.
	rename map[string]string
	// derive computes the columns that are not forma2 columns: from the forma2 row, the written row, and the sample's forma2 key values.
	derive func(ctx context.Context, im *importer, src, tgt map[string]any, forma2Key []string) (map[string]any, error)
}

// verdict is an expected value that is a check of its own: a ciphertext is opened, not compared.
type verdict bool

// mismatch is a sampled row that cannot be what forma2 holds; it fails the run rather than aborting it.
type mismatch struct{ msg string }

func (m *mismatch) Error() string { return m.msg }

func mismatchf(format string, args ...any) error { return &mismatch{fmt.Sprintf(format, args...)} }

func specName(s *tableSpec) string { return s.target + " from " + s.label() }

// expectations are keyed by specName; a spec without one has only columns named as forma2 names them.
var expectations = map[string]expectation{
	"users from User": {derive: func(_ context.Context, _ *importer, src, _ map[string]any, _ []string) (map[string]any, error) {
		return map[string]any{"notification_settings": expectNotificationSettings(src["notificationSettings"])}, nil
	}},
	"user_identities from Account": {derive: func(_ context.Context, _ *importer, src, _ map[string]any, _ []string) (map[string]any, error) {
		provider := identityProviders[text(src["provider"])]
		out := map[string]any{"provider": provider, "issuer": ""}
		if provider == "credential" {
			out["subject"] = resolveString(text(src["userId"]))
			out["password_hash"] = src["password"]
		} else {
			out["subject"] = src["providerAccountId"]
			out["password_hash"] = nil
		}
		return out, nil
	}},
	"user_identities from User.password, User.identityProvider": {
		table: "User",
		locate: func(k []string) (string, []any) {
			_, userID, _ := strings.Cut(k[0], ":")
			return "t.id = $1", []any{userID}
		},
		derive: func(_ context.Context, _ *importer, src, _ map[string]any, k []string) (map[string]any, error) {
			userID := text(src["id"])
			if strings.HasPrefix(k[0], "forma2-user-password:") {
				if src["password"] == nil {
					return nil, mismatchf("user %s has no password in forma2", userID)
				}
				return map[string]any{"id": "forma2-user-password:" + userID, "user_id": userID, "provider": "credential", "issuer": "",
					"subject": resolveString(userID), "password_hash": src["password"]}, nil
			}
			return map[string]any{"id": "forma2-user-identity:" + userID, "user_id": userID, "provider": identityProviders[text(src["identityProvider"])], "issuer": "",
				"subject": src["identityProviderAccountId"], "password_hash": nil}, nil
		},
	},
	"user_totp from TwoFactor": {
		rename: map[string]string{"failed_attempts": "failedVerificationCount"},
		derive: func(ctx context.Context, im *importer, src, tgt map[string]any, _ []string) (map[string]any, error) {
			user, err := im.forma2Row(ctx, "User", "t.id = $1", src["userId"])
			if err != nil {
				return nil, err
			}
			m, err := auth.ImportBetterAuthTwoFactor(im.opts.BetterAuth, text(src["secret"]), text(src["backupCodes"]))
			if err != nil {
				return nil, err
			}
			opens := verdict(totpOpens(im, text(src["userId"]), tgt, m))
			return map[string]any{"secret_ciphertext": opens, "backup_codes_ciphertext": opens, "verified": src["verified"] == true && user["twoFactorEnabled"] == true}, nil
		},
	},
	"user_totp from User.twoFactorSecret, User.backupCodes": {
		table:  "User",
		locate: func(k []string) (string, []any) { return "t.id = $1", []any{k[0]} },
		derive: func(_ context.Context, im *importer, src, tgt map[string]any, _ []string) (map[string]any, error) {
			if src["twoFactorEnabled"] != true {
				return nil, mismatchf("user %s does not have two-factor sign-in on in forma2", text(src["id"]))
			}
			m, err := auth.ImportForma2TwoFactor(im.opts.Forma2Key, text(src["twoFactorSecret"]), text(src["backupCodes"]))
			if err != nil {
				return nil, err
			}
			opens := verdict(totpOpens(im, text(src["id"]), tgt, m))
			return map[string]any{"user_id": src["id"], "secret_ciphertext": opens, "backup_codes_ciphertext": opens, "verified": true, "failed_attempts": 0, "locked_until": nil}, nil
		},
	},
	"invites from Invite": {derive: func(ctx context.Context, im *importer, src, _ map[string]any, _ []string) (map[string]any, error) {
		newer, err := im.forma2Value(ctx, `SELECT count(*) FROM "Invite" t WHERE t."organizationId" = $1 AND lower(t.email) = lower($2) AND (t."createdAt", t.id) > ($3::timestamp, $4)`,
			src["organizationId"], src["email"], src["createdAt"], src["id"])
		if err != nil {
			return nil, err
		}
		if newer != int64(0) {
			return nil, mismatchf("invite %s was kept although forma2 has a newer invite to its address", text(src["id"]))
		}
		return map[string]any{"email": strings.ToLower(text(src["email"]))}, nil
	}},
	"invite_teams from Invite.teamIds": {
		table:  "Invite",
		locate: func(k []string) (string, []any) { return `t.id = $1 AND $2 = ANY (t."teamIds")`, []any{k[0], k[1]} },
		derive: func(ctx context.Context, im *importer, src, _ map[string]any, k []string) (map[string]any, error) {
			team, err := im.forma2Row(ctx, "Team", `t.id = $1 AND t."organizationId" = $2`, k[1], src["organizationId"])
			if err != nil {
				return nil, err
			}
			if team == nil {
				return nil, mismatchf("invite %s names team %s, which is not one of its organization's", text(src["id"]), k[1])
			}
			return map[string]any{"invite_id": src["id"], "team_id": team["id"]}, nil
		},
	},
	"api_keys from ApiKey": {derive: func(ctx context.Context, im *importer, src, _ map[string]any, _ []string) (map[string]any, error) {
		var createdBy any
		if src["createdBy"] != nil {
			user, err := im.forma2Row(ctx, "User", "t.id = $1", src["createdBy"])
			if err != nil {
				return nil, err
			}
			if user != nil {
				createdBy = user["id"]
			}
		}
		lookup := src["lookupHash"]
		if lookup == nil {
			lookup = src["hashedKey"]
		}
		return map[string]any{"created_by": createdBy, "lookup_hash": lookup}, nil
	}},
	"contact_attributes from ContactAttribute": {derive: func(ctx context.Context, im *importer, src, _ map[string]any, _ []string) (map[string]any, error) {
		attributeKey, err := im.forma2Row(ctx, "ContactAttributeKey", "t.id = $1", src["attributeKeyId"])
		if err != nil {
			return nil, err
		}
		if attributeKey == nil {
			return nil, mismatchf("contact attribute %s has no attribute key in forma2", text(src["id"]))
		}
		out := map[string]any{"value_text": src["value"], "value_number": nil, "value_date": nil}
		switch attributeKey["dataType"] {
		case "number":
			out["value_number"] = src["valueNumber"]
			if src["valueNumber"] != nil {
				out["value_text"] = nil
			}
		case "date":
			out["value_date"] = src["valueDate"]
			if src["valueDate"] != nil {
				out["value_text"] = nil
			}
		}
		return out, nil
	}},
	"segments from Segment": {derive: func(ctx context.Context, im *importer, src, _ map[string]any, _ []string) (map[string]any, error) {
		title := src["title"]
		if src["isPrivate"] == true {
			s, err := im.forma2Row(ctx, "Survey", `t.id = $1 AND t."workspaceId" = $2`, src["title"], src["workspaceId"])
			if err != nil {
				return nil, err
			}
			if s != nil {
				title = resolveString(text(src["title"]))
			}
		}
		filters, err := rewriteSegmentFilters(jsonText(src["filters"]))
		if err != nil {
			return nil, err
		}
		return map[string]any{"title": title, "filters": jsonValue(filters)}, nil
	}},
	"surveys from Survey": {derive: func(_ context.Context, _ *importer, src, _ map[string]any, _ []string) (map[string]any, error) {
		endings := src["endings"]
		if endings == nil {
			endings = []any{}
		}
		blocks := src["blocks"]
		if b, _ := blocks.([]any); len(b) == 0 {
			blocks = []any{}
			if q, _ := src["questions"].([]any); len(q) > 0 {
				converted, err := questionsToBlocks(text(src["id"]), jsonText(src["questions"]), jsonText(endings))
				if err != nil {
					return nil, err
				}
				blocks = jsonValue(converted)
			}
		}
		var percentage any
		if p, ok := exactNumber(src["displayPercentage"]); ok {
			percentage = roundHalfAway(p, 2)
		}
		return map[string]any{"blocks": blocks, "endings": endings, "display_percentage": percentage}, nil
	}},
	"survey_languages from SurveyLanguage": {derive: func(ctx context.Context, im *importer, src, _ map[string]any, _ []string) (map[string]any, error) {
		first, err := im.forma2Value(ctx, `SELECT min(t."languageId") FROM "SurveyLanguage" t WHERE t."surveyId" = $1 AND t."default"`, src["surveyId"])
		if err != nil {
			return nil, err
		}
		return map[string]any{"is_default": src["default"] == true && first == src["languageId"]}, nil
	}},
	"survey_embedded_data from SurveyEmbeddedData": {rename: map[string]string{"position": "order"}},
	"webhooks from Webhook": {derive: func(_ context.Context, _ *importer, src, _ map[string]any, _ []string) (map[string]any, error) {
		triggers := src["triggers"]
		if triggers == nil {
			triggers = []any{}
		}
		surveyIDs, _ := src["surveyIds"].([]any)
		return map[string]any{"triggers": triggers, "all_surveys": len(surveyIDs) == 0}, nil
	}},
	"webhook_surveys from Webhook.surveyIds": {
		table:  "Webhook",
		locate: func(k []string) (string, []any) { return `t.id = $1 AND $2 = ANY (t."surveyIds")`, []any{k[0], k[1]} },
		derive: func(_ context.Context, _ *importer, src, _ map[string]any, k []string) (map[string]any, error) {
			return map[string]any{"webhook_id": src["id"], "survey_id": k[1]}, nil
		},
	},
	"integrations from Integration": {derive: func(_ context.Context, _ *importer, src, _ map[string]any, _ []string) (map[string]any, error) {
		config, _ := src["config"].(map[string]any)
		data := config["data"]
		if data == nil {
			data = []any{}
		}
		out := map[string]any{"config": map[string]any{"data": data}, "last_error_at": nil, "last_error_message": nil, "consecutive_failures": 0}
		if src["lastErrorAt"] != nil && src["lastErrorMessage"] != nil {
			out["last_error_at"], out["last_error_message"] = src["lastErrorAt"], src["lastErrorMessage"]
		}
		if n, ok := exactNumber(src["consecutiveFailures"]); ok && n.Sign() > 0 {
			out["consecutive_failures"] = n
		}
		return out, nil
	}},
	"integration_credentials from Integration.config.key": {
		table:  "Integration",
		locate: func(k []string) (string, []any) { return "t.id = $1", []any{k[0]} },
		derive: func(_ context.Context, im *importer, src, tgt map[string]any, _ []string) (map[string]any, error) {
			config, _ := src["config"].(map[string]any)
			if _, ok := config["key"].(map[string]any); !ok {
				return nil, mismatchf("integration %s has no credentials in forma2", text(src["id"]))
			}
			key, _, err := decryptCredentialKey(jsonText(config["key"]), im.opts.Forma2Key)
			if err != nil {
				return nil, err
			}
			email, _ := config["email"].(string)
			id, _ := legacyid.Resolve(text(src["id"]))
			ciphertext, _ := tgt["ciphertext"].([]byte)
			creds, err := integration.OpenCredentials(im.opts.Cipher, id, ciphertext)
			return map[string]any{"integration_id": src["id"], "ciphertext": verdict(err == nil && creds.Email == email && sameJSON(creds.Key, key))}, nil
		},
	},
	"workflows from Workflow": {derive: func(ctx context.Context, im *importer, src, _ map[string]any, _ []string) (map[string]any, error) {
		version, err := im.forma2Row(ctx, "WorkflowVersion", `t."workflowId" = $1 ORDER BY t.version DESC`, src["id"])
		if err != nil {
			return nil, err
		}
		var survey any
		definition, _ := version["definition"].(map[string]any)
		trigger, _ := definition["trigger"].(map[string]any)
		config, _ := trigger["config"].(map[string]any)
		if id, ok := config["surveyId"].(string); ok && id != "" && trigger["triggerType"] == "response.completed" {
			survey = id
		}
		return map[string]any{"trigger_survey_id": survey}, nil
	}},
	"workflow_runs from WorkflowRun": {derive: func(_ context.Context, _ *importer, src, _ map[string]any, _ []string) (map[string]any, error) {
		if src["status"] != "queued" && src["status"] != "running" {
			return nil, nil
		}
		failure, finished := src["error"], src["finishedAt"]
		if failure == nil {
			failure = unfinishedRun
		}
		if finished == nil {
			finished = src["updated_at"]
		}
		return map[string]any{"status": "canceled", "next_attempt_at": nil, "error": failure, "finished_at": finished}, nil
	}},
	"workflow_run_logs from WorkflowRunLog": {derive: func(ctx context.Context, im *importer, src, _ map[string]any, _ []string) (map[string]any, error) {
		run, err := im.forma2Row(ctx, "WorkflowRun", "t.id = $1", src["runId"])
		if err != nil {
			return nil, err
		}
		if (run["status"] == "queued" || run["status"] == "running") && (src["status"] == "pending" || src["status"] == "running") {
			return map[string]any{"status": "skipped"}, nil
		}
		return nil, nil
	}},
}

// expectNotificationSettings is User.notificationSettings with its survey ids (the keys of alert) and organization ids (unsubscribedOrganizationIds) as this server's ids.
func expectNotificationSettings(v any) any {
	doc, ok := v.(map[string]any)
	if !ok {
		return v
	}
	out := make(map[string]any, len(doc))
	for k, val := range doc {
		out[k] = val
	}
	if alert, ok := doc["alert"].(map[string]any); ok {
		resolved := make(map[string]any, len(alert))
		for surveyID, on := range alert {
			resolved[resolveString(surveyID)] = on
		}
		out["alert"] = resolved
	}
	if ids, ok := doc["unsubscribedOrganizationIds"].([]any); ok {
		resolved := make([]any, len(ids))
		for i, orgID := range ids {
			resolved[i] = resolveString(text(orgID))
		}
		out["unsubscribedOrganizationIds"] = resolved
	}
	return out
}

// totpOpens reports whether the written ciphertexts open, for the user they were sealed for, to the material forma2's columns decrypt to.
func totpOpens(im *importer, forma2UserID string, tgt map[string]any, want auth.TOTPMaterial) bool {
	userID, _ := legacyid.Resolve(forma2UserID)
	secretCT, _ := tgt["secret_ciphertext"].([]byte)
	codesCT, _ := tgt["backup_codes_ciphertext"].([]byte)
	have, err := auth.OpenTOTP(im.opts.Cipher, userID, secretCT, codesCT)
	return err == nil && bytes.Equal(have.Key, want.Key) && slices.Equal(have.BackupCodes, want.BackupCodes)
}

// forma2Row reads one forma2 row whole, as to_jsonb names its columns, from the import's snapshot; nil when there is none. where may end in an ORDER BY, which picks the first.
func (im *importer) forma2Row(ctx context.Context, table, where string, args ...any) (map[string]any, error) {
	rows, err := im.src.Query(ctx, fmt.Sprintf(`SELECT to_jsonb(t)::text FROM public.%s t WHERE %s LIMIT 2`, pgx.Identifier{table}.Sanitize(), where), args...)
	if err != nil {
		return nil, fmt.Errorf("read forma2's %s again: %w", table, err)
	}
	docs, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("read forma2's %s again: %w", table, err)
	}
	if len(docs) == 0 {
		return nil, nil
	}
	if len(docs) > 1 && !strings.Contains(where, "ORDER BY") {
		return nil, mismatchf("more than one forma2 %s row matches %s", table, where)
	}
	var out map[string]any
	if err := decode([]byte(docs[0]), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// forma2Value runs a query of one value on the import's snapshot.
func (im *importer) forma2Value(ctx context.Context, query string, args ...any) (any, error) {
	var v any
	if err := im.src.QueryRow(ctx, query, args...).Scan(&v); err != nil {
		return nil, fmt.Errorf("read forma2 again: %w", err)
	}
	return v, nil
}

// forma2Column is the column of forma2's table that the column of this schema holds: the expectation's rename, else the one whose name is the same without case and underscores.
func (im *importer) forma2Column(table, name string, rename map[string]string) (string, bool) {
	if c, ok := rename[name]; ok {
		return c, im.schema.has(table, c)
	}
	norm := func(s string) string { return strings.ToLower(strings.ReplaceAll(s, "_", "")) }
	found := ""
	for c := range im.schema.columns[table] {
		if norm(c) == norm(name) {
			if found != "" {
				return "", false
			}
			found = c
		}
	}
	return found, found != ""
}

// laterColumn reports whether the column of this schema holds one of the columns forma2's later migrations added, which the source lacks: it then holds its default.
func laterColumn(table, name string) bool {
	norm := func(s string) string { return strings.ToLower(strings.ReplaceAll(s, "_", "")) }
	return slices.ContainsFunc(laterColumns[table], func(c string) bool { return norm(c) == norm(name) })
}

// checkSample compares one written row with its forma2 row, read again, column by column.
func (im *importer) checkSample(ctx context.Context, w *writtenTable, s sample, tgt map[string]any) error {
	exp := expectations[specName(w.spec)]
	table := exp.table
	if table == "" {
		table = w.spec.from
	}
	var where string
	var args []any
	if exp.locate != nil {
		where, args = exp.locate(s.forma2)
	} else {
		match := make([]string, len(w.spec.key))
		for i, k := range w.spec.key {
			c, ok := im.forma2Column(table, k, exp.rename)
			if !ok {
				im.fail("%s: no column of forma2's %s holds its key %s", specName(w.spec), table, k)
				return nil
			}
			match[i] = fmt.Sprintf("t.%s = $%d", pgx.Identifier{c}.Sanitize(), i+1)
			args = append(args, s.forma2[i])
		}
		where = strings.Join(match, " AND ")
	}
	src, err := im.forma2Row(ctx, table, where, args...)
	var mm *mismatch
	if errors.As(err, &mm) {
		im.fail("%s: row %s: %s", w.spec.target, s.key, mm.msg)
		return nil
	}
	if err != nil {
		return err
	}
	if src == nil {
		im.fail("%s: row %s was not written from a row forma2's %s holds", w.spec.target, s.key, table)
		return nil
	}
	derived := map[string]any{}
	if exp.derive != nil {
		d, err := exp.derive(ctx, im, src, tgt, s.forma2)
		if errors.As(err, &mm) {
			im.fail("%s: row %s: %s", w.spec.target, s.key, mm.msg)
			return nil
		}
		if err != nil {
			return fmt.Errorf("row %s: %w", s.key, err)
		}
		derived = d
	}
	for _, name := range w.spec.written() {
		want, ok := derived[name]
		if !ok {
			c, found := im.forma2Column(table, name, exp.rename)
			switch {
			case found:
				want = src[c]
			case laterColumn(table, name):
				want = nil
			default:
				im.fail("%s: nothing checks column %s: forma2's %s has no column of that name and no expectation derives it", specName(w.spec), name, table)
				continue
			}
		}
		if !sameAsForma2(want, tgt[name]) {
			im.fail("%s: row %s holds a %s that differs from what forma2's %s holds", w.spec.target, s.key, name, table)
		}
	}
	return nil
}

// sameAsForma2 compares a value worked out from forma2's row (JSON as to_jsonb renders it, numbers exact) with a value read back from this schema (as pgx decodes it).
func sameAsForma2(want, got any) bool {
	if v, ok := want.(verdict); ok {
		return bool(v)
	}
	if want == nil || got == nil {
		return want == nil && got == nil
	}
	switch g := got.(type) {
	case [16]byte:
		return sameID(want, uuid.UUID(g))
	case uuid.UUID:
		return sameID(want, g)
	case time.Time:
		w, ok := forma2Time(want)
		return ok && w.Equal(g)
	case float64:
		w, ok := exactNumber(want)
		if !ok {
			return false
		}
		f, _ := w.Float64()
		return f == g
	case pgtype.Numeric, int16, int32, int64, int:
		w, ok1 := exactNumber(want)
		h, ok2 := exactNumber(got)
		return ok1 && ok2 && w.Cmp(h) == 0
	case []any:
		w, ok := want.([]any)
		if !ok || len(w) != len(g) {
			return false
		}
		for i := range g {
			if !sameAsForma2(w[i], g[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		w, ok := want.(map[string]any)
		if !ok || len(w) != len(g) {
			return false
		}
		for k, v := range g {
			wv, ok := w[k]
			if !ok || !sameAsForma2(wv, v) {
				return false
			}
		}
		return true
	case string, bool:
		return want == got
	}
	return false
}

func sameID(want any, got uuid.UUID) bool {
	switch w := want.(type) {
	case string:
		id, ok := legacyid.Resolve(w)
		return ok && id == got
	case uuid.UUID:
		return w == got
	}
	return false
}

// forma2Time reads a timestamp as to_jsonb renders forma2's TIMESTAMP(3), which has no zone and holds UTC.
func forma2Time(v any) (time.Time, bool) {
	switch x := v.(type) {
	case time.Time:
		return x, true
	case string:
		t, err := time.Parse("2006-01-02T15:04:05.999999999", x)
		if err != nil {
			t, err = time.Parse(time.RFC3339Nano, x)
		}
		return t, err == nil
	}
	return time.Time{}, false
}

// exactNumber is a number as an exact rational.
func exactNumber(v any) (*big.Rat, bool) {
	switch x := v.(type) {
	case *big.Rat:
		return x, true
	case json.Number:
		return new(big.Rat).SetString(string(x))
	case float64:
		return new(big.Rat).SetString(fmt.Sprint(x))
	case int:
		return new(big.Rat).SetInt64(int64(x)), true
	case int16:
		return new(big.Rat).SetInt64(int64(x)), true
	case int32:
		return new(big.Rat).SetInt64(int64(x)), true
	case int64:
		return new(big.Rat).SetInt64(x), true
	case pgtype.Numeric:
		if !x.Valid || x.NaN || x.InfinityModifier != pgtype.Finite {
			return nil, false
		}
		r := new(big.Rat).SetInt(x.Int)
		scale := new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(abs(x.Exp))), nil))
		if x.Exp >= 0 {
			return r.Mul(r, scale), true
		}
		return r.Quo(r, scale), true
	}
	return nil, false
}

func abs(n int32) int32 {
	if n < 0 {
		return -n
	}
	return n
}

// roundHalfAway rounds to places decimals, halves away from zero, as PostgreSQL rounds a numeric.
func roundHalfAway(r *big.Rat, places int) *big.Rat {
	scale := new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(places)), nil))
	scaled := new(big.Rat).Mul(r, scale)
	num, den := new(big.Int).Abs(scaled.Num()), scaled.Denom()
	q, rem := new(big.Int).QuoRem(num, den, new(big.Int))
	if new(big.Int).Mul(rem, big.NewInt(2)).Cmp(den) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if scaled.Sign() < 0 {
		q.Neg(q)
	}
	return new(big.Rat).Quo(new(big.Rat).SetInt(q), scale)
}

func text(v any) string {
	s, _ := v.(string)
	return s
}

// jsonText is a value of a forma2 row as JSON text again; numbers keep the digits they were read with.
func jsonText(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// jsonValue reads JSON text the way a forma2 row is read.
func jsonValue(s string) any {
	var v any
	if err := decode([]byte(s), &v); err != nil {
		return s
	}
	return v
}
