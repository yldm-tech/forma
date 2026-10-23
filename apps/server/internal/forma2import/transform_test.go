package forma2import

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
	"github.com/yldm-tech/forma/server/internal/survey/surveydoc"
)

func TestRewriteNotificationSettings(t *testing.T) {
	got, err := rewriteNotificationSettings(`{"alert": {"clsurveyaaaaaaaaaaaaaaaaa": true, "019a0000-0000-7000-8000-000000000001": false}, "unsubscribedOrganizationIds": ["clorgbbbbbbbbbbbbbbbbbbbb"], "other": [1.50]}`)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"alert":{"019a0000-0000-7000-8000-000000000001":false,"` + legacyid.ID("clsurveyaaaaaaaaaaaaaaaaa").String() + `":true},"other":[1.50],"unsubscribedOrganizationIds":["` + legacyid.ID("clorgbbbbbbbbbbbbbbbbbbbb").String() + `"]}`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	for _, in := range []string{`{}`, `{"alert": null}`} {
		if _, err := rewriteNotificationSettings(in); err != nil {
			t.Errorf("%s: %v", in, err)
		}
	}
	for _, in := range []string{`[]`, `{"alert": []}`, `{"unsubscribedOrganizationIds": [1]}`} {
		if _, err := rewriteNotificationSettings(in); err == nil {
			t.Errorf("%s was accepted", in)
		}
	}
}

func TestRewriteSegmentFilters(t *testing.T) {
	seg, sv := "clsegaaaaaaaaaaaaaaaaaaaa", "clsurveyaaaaaaaaaaaaaaaaa"
	in := `[{"id":"a","connector":null,"resource":{"id":"l1","root":{"type":"attribute","contactAttributeKey":"email"},"value":"clsegaaaaaaaaaaaaaaaaaaaa","qualifier":{"operator":"equals"}}},
		{"id":"g","connector":"and","resource":[{"id":"b","connector":null,"resource":{"id":"l2","root":{"type":"segment","segmentId":"` + seg + `"},"value":"` + seg + `","qualifier":{"operator":"userIsIn"}}},
		{"id":"c","connector":"or","resource":{"id":"l3","root":{"type":"surveyInteraction"},"value":{"surveyScope":"specific","surveyIds":["` + sv + `"],"within":{"amount":7,"unit":"days"}},"qualifier":{"operator":"haveSeen"}}}]}]`
	got, err := rewriteSegmentFilters(in)
	if err != nil {
		t.Fatal(err)
	}
	var tree []map[string]any
	if err := json.Unmarshal([]byte(got), &tree); err != nil {
		t.Fatal(err)
	}
	// An attribute filter's value is the attribute's, whatever it looks like.
	if v := tree[0]["resource"].(map[string]any)["value"]; v != seg {
		t.Errorf("attribute value rewritten to %v", v)
	}
	group := tree[1]["resource"].([]any)
	ref := group[0].(map[string]any)["resource"].(map[string]any)
	if ref["root"].(map[string]any)["segmentId"] != legacyid.ID(seg).String() || ref["value"] != legacyid.ID(seg).String() {
		t.Errorf("segment reference %v", ref)
	}
	seen := group[1].(map[string]any)["resource"].(map[string]any)["value"].(map[string]any)
	if ids := seen["surveyIds"].([]any); len(ids) != 1 || ids[0] != legacyid.ID(sv).String() || seen["within"].(map[string]any)["amount"] != float64(7) {
		t.Errorf("survey interaction %v", seen)
	}
	if again, _ := rewriteSegmentFilters(got); again != got {
		t.Error("rewriting is not idempotent")
	}
}

func TestQuestionsToBlocksIsDeterministic(t *testing.T) {
	questions := `[{"id":"q1","type":"openText","headline":{"default":"One"},"required":true,"inputType":"text","charLimit":{"enabled":false},
		"logic":[{"id":"lg","conditions":{"id":"c","connector":"and","conditions":[]},"actions":[{"id":"a","objective":"jumpToQuestion","target":"q2"}]}]},
		{"id":"q2","type":"openText","headline":{"default":"Two"},"required":false,"inputType":"text","charLimit":{"enabled":false}}]`
	first, err := questionsToBlocks("clsurveyaaaaaaaaaaaaaaaaa", questions, "[]")
	if err != nil {
		t.Fatal(err)
	}
	second, _ := questionsToBlocks("clsurveyaaaaaaaaaaaaaaaaa", questions, "[]")
	if first != second {
		t.Fatalf("two conversions differ:\n%s\n%s", first, second)
	}
	other, _ := questionsToBlocks("clsurveybbbbbbbbbbbbbbbbb", questions, "[]")
	if other == first {
		t.Error("two surveys got the same block ids")
	}
	var blocks []map[string]any
	if err := json.Unmarshal([]byte(first), &blocks); err != nil || len(blocks) != 2 {
		t.Fatalf("%s: %v", first, err)
	}
	b2 := blocks[1]["id"].(string)
	if !surveydoc.IsCUID2(b2) || b2 != blockID("clsurveyaaaaaaaaaaaaaaaaa", "q2") {
		t.Errorf("block id %q", b2)
	}
	// The jump to the second question now jumps to its block, under the derived id.
	if !strings.Contains(first, `"target":"`+b2+`"`) {
		t.Errorf("logic does not target the derived block id: %s", first)
	}
}

func TestDecryptCredentialKey(t *testing.T) {
	key := []byte("0123456789abcdefghijklmnopqrstuv")
	sealed, err := secret.EncryptForma2("secret-token", key)
	if err != nil {
		t.Fatal(err)
	}
	foreign, _ := secret.EncryptForma2("x", []byte("ffffffffffffffffffffffffffffffff"))
	raw := `{"access_token":"` + sealed + `","refresh_token":"1//plain","token_type":"Bearer","expiry_date":1760000000000,"bot_id":"` + sealed + `","client_secret":"` + foreign + `"}`
	got, undecryptable, err := decryptCredentialKey(raw, key)
	if err != nil {
		t.Fatal(err)
	}
	var k map[string]any
	_ = json.Unmarshal(got, &k)
	// Only secret fields are decrypted; bot_id is not one, whatever it holds.
	if k["access_token"] != "secret-token" || k["refresh_token"] != "1//plain" || k["bot_id"] != sealed || k["client_secret"] != foreign || !strings.Contains(string(got), "1760000000000") {
		t.Errorf("decrypted %s", got)
	}
	if len(undecryptable) != 1 || undecryptable[0] != "client_secret" {
		t.Errorf("undecryptable %v", undecryptable)
	}
}

func TestClassifyCredential(t *testing.T) {
	iv := strings.Repeat("ab", 16)
	cases := map[string]credentialForm{
		"xoxb-1234":                              cleartext,
		"ya29.a0AfH6":                            cleartext,
		iv + ":00ff:" + strings.Repeat("cd", 16): gcm,
		iv + "::" + strings.Repeat("cd", 16):     gcm,
		iv + ":00ff":                             legacyCBC,
		iv + ":":                                 cleartext,
		iv + ":zz:" + strings.Repeat("cd", 16):   cleartext,
		"short:00ff:" + strings.Repeat("cd", 16): cleartext,
	}
	for v, want := range cases {
		if got := classifyCredential(v); got != want {
			t.Errorf("classifyCredential(%q) = %d, want %d", v, got, want)
		}
	}
}

func TestIntegrationLinks(t *testing.T) {
	for in, want := range map[string]string{
		`{"key":{"access_token":"x"},"email":"a@b.c","data":[{"surveyId":"clsurveyaaaaaaaaaaaaaaaaa"}]}`: `{"data":[{"surveyId":"clsurveyaaaaaaaaaaaaaaaaa"}]}`,
		`{"key":{}}`:             `{"data":[]}`,
		`{"key":{},"data":null}`: `{"data":[]}`,
	} {
		if got, err := integrationLinks(in); err != nil || got != want {
			t.Errorf("integrationLinks(%s) = %s, %v; want %s", in, got, err, want)
		}
	}
}

// The import writes this schema's tables only.
func TestAssertTargetTable(t *testing.T) {
	for _, name := range []string{"users", "surveys", "integration_credentials"} {
		if err := assertTargetTable(name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, name := range []string{"User", "Survey", "_prisma_migrations", "forma2_imports", "goose_db_version"} {
		if err := assertTargetTable(name); err == nil {
			t.Errorf("%s was allowed", name)
		}
	}
	for name := range targetTables {
		for _, f := range forma2Tables {
			if strings.EqualFold(name, f) {
				t.Errorf("target table %s is forma2's %s", name, f)
			}
		}
	}
}

func TestUpsertSQL(t *testing.T) {
	got := upsertSQL("memberships", `"stage"`, []string{"user_id", "organization_id", "role"}, []string{"user_id", "organization_id"})
	for _, want := range []string{`ON CONFLICT ("user_id", "organization_id") DO UPDATE SET "role" = EXCLUDED."role" WHERE t."role" IS DISTINCT FROM EXCLUDED."role"`, "RETURNING (xmax = 0)"} {
		if !strings.Contains(got, want) {
			t.Errorf("%s\nlacks %s", got, want)
		}
	}
	if got := upsertSQL("response_tags", `"stage"`, []string{"response_id", "tag_id"}, []string{"response_id", "tag_id"}); !strings.Contains(got, "DO NOTHING") {
		t.Errorf("a key-only table: %s", got)
	}
}
