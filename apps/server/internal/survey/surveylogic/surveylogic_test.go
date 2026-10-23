package surveylogic

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

// testdata/parity.json is forma2's logic parity corpus (packages/types/surveys/logic-parity-fixtures.ts), the table both of forma2's engines were held to, exported as JSON. Every case runs here against this engine with the same expectation.
type parityFile struct {
	Survey struct {
		Blocks         []surveydoc.Block `json:"blocks"`
		EmbeddedFields []struct {
			Field struct {
				Name     string `json:"name"`
				Source   string `json:"source"`
				DataType string `json:"dataType"`
				Locked   bool   `json:"locked"`
			} `json:"field"`
			Link struct {
				StorageKey string `json:"storageKey"`
			} `json:"link"`
		} `json:"embeddedFields"`
	} `json:"survey"`
	Language  string         `json:"language"`
	Data      map[string]any `json:"data"`
	Variables map[string]any `json:"variables"`
	Cases     []struct {
		Name           string              `json:"name"`
		Condition      surveydoc.Condition `json:"condition"`
		Data           map[string]any      `json:"data"`
		Variables      map[string]any      `json:"variables"`
		EmbeddedValues map[string]any      `json:"embeddedValues"`
		Expected       bool                `json:"expected"`
	} `json:"cases"`
}

func merged(base, over map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		out[k] = v
	}
	return out
}

func TestLogicParityCorpus(t *testing.T) {
	raw, err := os.ReadFile("testdata/parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var f parityFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	var fields []embeddeddata.Linked
	for _, e := range f.Survey.EmbeddedFields {
		fields = append(fields, embeddeddata.Linked{Field: embeddeddata.Field{StorageKey: e.Link.StorageKey, Name: e.Field.Name, Source: e.Field.Source, DataType: e.Field.DataType}, Locked: e.Field.Locked})
	}
	s := NewSurvey(f.Survey.Blocks, fields)
	operators := map[string]bool{}
	for _, c := range f.Cases {
		operators[c.Condition.Operator] = true
		t.Run(c.Name, func(t *testing.T) {
			in := Input{Data: merged(f.Data, c.Data), Variables: merged(f.Variables, c.Variables), Language: f.Language, Reserved: merged(nil, c.EmbeddedValues)}
			group := surveydoc.Condition{ID: "parity-group", Connector: "and", IsGroup: true, Conditions: []surveydoc.Condition{c.Condition}}
			if got := Evaluate(s, in, group); got != c.Expected {
				t.Fatalf("Evaluate = %v, want %v", got, c.Expected)
			}
		})
	}
	if len(operators) < 32 {
		t.Fatalf("the corpus covers %d operators, want all 32 of ZSurveyLogicConditionsOperator", len(operators))
	}
}

func condition(t *testing.T, raw string) surveydoc.Condition {
	t.Helper()
	var c surveydoc.Condition
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestGroupsCombineWithTheirConnector(t *testing.T) {
	s := Survey{}
	in := Input{Data: map[string]any{"hf": "a"}}
	yes := `{"id":"y","leftOperand":{"type":"hiddenField","value":"hf"},"operator":"equals","rightOperand":{"type":"static","value":"a"}}`
	no := `{"id":"n","leftOperand":{"type":"hiddenField","value":"hf"},"operator":"equals","rightOperand":{"type":"static","value":"b"}}`
	cases := map[string]bool{
		`{"id":"g","connector":"and","conditions":[` + yes + `,` + no + `]}`:                                                         false,
		`{"id":"g","connector":"or","conditions":[` + yes + `,` + no + `]}`:                                                          true,
		`{"id":"g","connector":"and","conditions":[` + yes + `,{"id":"g2","connector":"or","conditions":[` + no + `,` + yes + `]}]}`: true,
		`{"id":"g","connector":"and","conditions":[]}`:                                                                               true,
		`{"id":"g","connector":"or","conditions":[]}`:                                                                                false,
	}
	for raw, want := range cases {
		if got := Evaluate(s, in, condition(t, raw)); got != want {
			t.Errorf("%s = %v, want %v", raw, got, want)
		}
	}
}

func TestReservedOperandsReadTheServerValues(t *testing.T) {
	in := Input{Data: map[string]any{}, Reserved: map[string]any{"country": "DE", "finished": "true"}}
	group := condition(t, `{"id":"g","connector":"and","conditions":[
		{"id":"a","leftOperand":{"type":"reserved","value":"country"},"operator":"equals","rightOperand":{"type":"static","value":"DE"}},
		{"id":"b","leftOperand":{"type":"reserved","value":"finished"},"operator":"equals","rightOperand":{"type":"static","value":"true"}}]}`)
	if !Evaluate(Survey{}, in, group) {
		t.Fatal("reserved operands did not resolve")
	}
	if Evaluate(Survey{}, Input{}, group) {
		t.Fatal("an unresolved reserved operand matched")
	}
}

func TestAMatrixRowReadOffANullAnswerIsFalseForEveryOperator(t *testing.T) {
	blocks := []surveydoc.Block{}
	if err := json.Unmarshal([]byte(`[{"id":"b","name":"b","elements":[{"id":"m","type":"matrix","headline":{"default":"M"},"required":false,
		"rows":[{"id":"r1","label":{"default":"Row 1"}}],"columns":[{"id":"c1","label":{"default":"Col 1"}}]}]}]`), &blocks); err != nil {
		t.Fatal(err)
	}
	s := NewSurvey(blocks, nil)
	in := Input{Data: map[string]any{"m": nil}, Language: "default"}
	for _, op := range []string{"isSkipped", "doesNotEqual", "isEmpty"} {
		group := condition(t, `{"id":"g","connector":"and","conditions":[{"id":"c","leftOperand":{"type":"element","value":"m","meta":{"row":"0"}},"operator":"`+op+`","rightOperand":{"type":"static","value":"0"}}]}`)
		if Evaluate(s, in, group) {
			t.Errorf("%s matched a null matrix answer", op)
		}
	}
	in.Data["m"] = map[string]any{"Row 1": "Col 1"}
	group := condition(t, `{"id":"g","connector":"and","conditions":[{"id":"c","leftOperand":{"type":"element","value":"m","meta":{"row":"0"}},"operator":"equals","rightOperand":{"type":"static","value":"0"}}]}`)
	if !Evaluate(s, in, group) {
		t.Error("a matrix row did not resolve to its column index")
	}
}
