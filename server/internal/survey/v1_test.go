package survey

import (
	"encoding/json"
	"reflect"
	"testing"
)

func decodeAny(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func blockAt(t *testing.T, blocks []any, i int) map[string]any {
	t.Helper()
	b, ok := blocks[i].(map[string]any)
	if !ok {
		t.Fatalf("block %d is %T", i, blocks[i])
	}
	return b
}

func firstElement(t *testing.T, block map[string]any) map[string]any {
	t.Helper()
	return block["elements"].([]any)[0].(map[string]any)
}

// forma2's transformQuestionsToBlocks (apps/web/lib/api/survey-transformation.test.ts): one block per question, named in order, with the question's logic, fallback and labels moved onto the block.
func TestQuestionsToBlocks(t *testing.T) {
	if got := QuestionsToBlocks(nil, nil); len(got) != 0 {
		t.Errorf("no questions = %v", got)
	}

	t.Run("one block per question", func(t *testing.T) {
		blocks := QuestionsToBlocks(decodeAny(t, `[{"id":"q1","type":"openText","headline":{"default":"One"},"required":false,"buttonLabel":{"default":"Next"},"backButtonLabel":{"default":"Back"}},{"id":"q2","type":"openText","headline":{"default":"Two"},"required":false}]`).([]any), nil)
		if len(blocks) != 2 || blockAt(t, blocks, 0)["name"] != "Block 1" || blockAt(t, blocks, 1)["name"] != "Block 2" {
			t.Fatalf("blocks = %v", blocks)
		}
		b := blockAt(t, blocks, 0)
		if id, _ := b["id"].(string); !isCuid(id) || b["id"] == blockAt(t, blocks, 1)["id"] {
			t.Errorf("block ids %v, %v", b["id"], blockAt(t, blocks, 1)["id"])
		}
		if !reflect.DeepEqual(b["buttonLabel"], map[string]any{"default": "Next"}) || !reflect.DeepEqual(b["backButtonLabel"], map[string]any{"default": "Back"}) {
			t.Errorf("labels did not move to the block: %v", b)
		}
		if el := firstElement(t, b); el["buttonLabel"] != nil || el["backButtonLabel"] != nil || el["id"] != "q1" {
			t.Errorf("element = %v", el)
		}
	})

	t.Run("CTA questions", func(t *testing.T) {
		blocks := QuestionsToBlocks(decodeAny(t, `[
			{"id":"ext","type":"cta","headline":{"default":"Click me"},"buttonLabel":{"default":"Click"},"buttonUrl":"https://example.com","buttonExternal":true,"dismissButtonLabel":{"default":"No"}},
			{"id":"int","type":"cta","headline":{"default":"Continue"},"buttonLabel":{"default":"Next"}},
			{"id":"noUrl","type":"cta","headline":{"default":"Continue"},"buttonExternal":true}]`).([]any), nil)
		ext := firstElement(t, blockAt(t, blocks, 0))
		if !reflect.DeepEqual(ext["ctaButtonLabel"], map[string]any{"default": "Click"}) || ext["buttonExternal"] != true || ext["buttonUrl"] != "https://example.com" || ext["buttonLabel"] != nil || ext["dismissButtonLabel"] != nil {
			t.Errorf("external CTA = %v", ext)
		}
		for i := 1; i <= 2; i++ {
			el := firstElement(t, blockAt(t, blocks, i))
			if _, ok := el["buttonExternal"]; ok || el["buttonUrl"] != nil || el["buttonLabel"] != nil {
				t.Errorf("CTA without an external button = %v", el)
			}
		}
	})

	t.Run("logic", func(t *testing.T) {
		blocks := QuestionsToBlocks(decodeAny(t, `[
			{"id":"q1","type":"openText","headline":{"default":"One"},"required":false,"logicFallback":"end1","logic":[{"id":"l1",
				"conditions":{"id":"g1","connector":"and","conditions":[{"id":"c1","leftOperand":{"value":"q1","type":"question"},"operator":"equals","rightOperand":{"type":"question","value":"q2"}}]},
				"actions":[{"id":"a1","objective":"jumpToQuestion","target":"q2"},{"id":"a2","objective":"jumpToQuestion","target":"end1"},{"id":"a3","objective":"jumpToQuestion","target":"nowhere"},{"id":"a4","objective":"requireAnswer","target":"q2"}]}]},
			{"id":"q2","type":"openText","headline":{"default":"Two"},"required":false,"logicFallback":"nowhere"}]`).([]any), decodeAny(t, `[{"id":"end1","type":"endScreen"}]`))
		first, second := blockAt(t, blocks, 0), blockAt(t, blocks, 1)
		logic := first["logic"].([]any)[0].(map[string]any)
		condition := logic["conditions"].(map[string]any)["conditions"].([]any)[0].(map[string]any)
		if condition["leftOperand"].(map[string]any)["type"] != "element" || condition["rightOperand"].(map[string]any)["type"] != "element" {
			t.Errorf("operands = %v", condition)
		}
		actions := logic["actions"].([]any)
		want := []map[string]any{
			{"id": "a1", "objective": "jumpToBlock", "target": second["id"]},
			{"id": "a2", "objective": "jumpToBlock", "target": "end1"},
			{"id": "a3", "objective": "jumpToBlock", "target": "nowhere"},
			{"id": "a4", "objective": "requireAnswer", "target": "q2"},
		}
		for i, w := range want {
			if !reflect.DeepEqual(actions[i], w) {
				t.Errorf("action %d = %v, want %v", i, actions[i], w)
			}
		}
		if first["logicFallback"] != "end1" {
			t.Errorf("a fallback to an ending is kept: %v", first["logicFallback"])
		}
		if _, ok := second["logicFallback"]; ok {
			t.Errorf("a fallback to nothing is dropped: %v", second)
		}
	})

	t.Run("CTA logic", func(t *testing.T) {
		questions := func(external bool) []any {
			cta := `{"id":"cta1","type":"cta","headline":{"default":"CTA"},"required":false,"buttonExternal":false}`
			if external {
				cta = `{"id":"cta1","type":"cta","headline":{"default":"CTA"},"required":false,"buttonLabel":{"default":"Click"},"buttonUrl":"https://example.com","buttonExternal":true}`
			}
			return decodeAny(t, `[`+cta+`,
				{"id":"q1","type":"openText","headline":{"default":"Q"},"required":false,"logic":[
					{"id":"l1","conditions":{"id":"g1","connector":"and","conditions":[{"id":"c1","leftOperand":{"value":"cta1","type":"question"},"operator":"isClicked"}]},"actions":[]},
					{"id":"l2","conditions":{"id":"g2","connector":"and","conditions":[
						{"id":"c2","leftOperand":{"value":"cta1","type":"question"},"operator":"isSkipped"},
						{"id":"c3","leftOperand":{"value":"q1","type":"question"},"operator":"isSubmitted"}]},"actions":[]}]}]`).([]any)
		}
		internal := blockAt(t, QuestionsToBlocks(questions(false), nil), 1)["logic"].([]any)
		if len(internal) != 1 || len(internal[0].(map[string]any)["conditions"].(map[string]any)["conditions"].([]any)) != 1 {
			t.Errorf("without an external button, click and skip tests go: %v", internal)
		}
		external := blockAt(t, QuestionsToBlocks(questions(true), nil), 1)["logic"].([]any)
		if len(external) != 2 || len(external[1].(map[string]any)["conditions"].(map[string]any)["conditions"].([]any)) != 1 {
			t.Errorf("with an external button, only skip tests go: %v", external)
		}
		only := decodeAny(t, `[{"id":"cta1","type":"cta","headline":{"default":"CTA"}},{"id":"q1","type":"openText","headline":{"default":"Q"},"logic":[
			{"id":"l1","conditions":{"id":"g1","connector":"and","conditions":[{"id":"c1","leftOperand":{"value":"cta1","type":"question"},"operator":"isClicked"}]},"actions":[]}]}]`).([]any)
		if _, ok := blockAt(t, QuestionsToBlocks(only, nil), 1)["logic"]; ok {
			t.Error("a block whose every rule tested the CTA keeps no logic")
		}
	})
}

// forma2's transformBlocksToQuestions: each element becomes a question; a block's logic, fallback and labels go to its last element, and a jump to a block jumps to its first element.
func TestBlocksToQuestions(t *testing.T) {
	got, err := blocksToQuestions(json.RawMessage(`[]`), nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("no blocks = %v, %v", got, err)
	}
	blocks := `[
		{"id":"b1","name":"Block 1","elements":[],"logic":[]},
		{"id":"b2","name":"Block 2","buttonLabel":{"default":"Next"},"backButtonLabel":{"default":"Back"},"logicFallback":"b3",
			"elements":[{"id":"cta","type":"cta","headline":{"default":"CTA"},"ctaButtonLabel":{"default":"Go"}},{"id":"q2","type":"openText","headline":{"default":"Two"}}],
			"logic":[{"id":"l1","conditions":{"id":"g1","connector":"or","conditions":[{"id":"c1","leftOperand":{"value":"q2","type":"element"},"operator":"isSubmitted"}]},
				"actions":[{"id":"a1","objective":"jumpToBlock","target":"b3"},{"id":"a2","objective":"jumpToBlock","target":"end1"}]}]},
		{"id":"b3","name":"Block 3","logicFallback":"end1","elements":[{"id":"q3","type":"openText","headline":{"default":"Three"}}]},
		{"id":"b4","name":"Block 4","logicFallback":"gone","elements":[{"id":"q4","type":"openText","headline":{"default":"Four"}}]}]`
	questions, err := blocksToQuestions(json.RawMessage(blocks), json.RawMessage(`[{"id":"end1","type":"endScreen"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(questions) != 4 {
		t.Fatalf("questions = %v", questions)
	}
	cta, q2, q3, q4 := questions[0].(map[string]any), questions[1].(map[string]any), questions[2].(map[string]any), questions[3].(map[string]any)
	if !reflect.DeepEqual(cta["buttonLabel"], map[string]any{"default": "Go"}) || cta["logic"] != nil || cta["backButtonLabel"] != nil {
		t.Errorf("a CTA that is not the block's last element only resolves its label: %v", cta)
	}
	if !reflect.DeepEqual(q2["buttonLabel"], map[string]any{"default": "Next"}) || !reflect.DeepEqual(q2["backButtonLabel"], map[string]any{"default": "Back"}) || q2["logicFallback"] != "q3" {
		t.Errorf("the last element takes the block's labels and fallback: %v", q2)
	}
	logic := q2["logic"].([]any)[0].(map[string]any)
	if logic["conditions"].(map[string]any)["conditions"].([]any)[0].(map[string]any)["leftOperand"].(map[string]any)["type"] != "question" {
		t.Errorf("operand types = %v", logic)
	}
	actions := logic["actions"].([]any)
	if !reflect.DeepEqual(actions[0], map[string]any{"id": "a1", "objective": "jumpToQuestion", "target": "q3"}) ||
		!reflect.DeepEqual(actions[1], map[string]any{"id": "a2", "objective": "jumpToQuestion", "target": "end1"}) {
		t.Errorf("actions = %v", actions)
	}
	if q3["logicFallback"] != "end1" {
		t.Errorf("a fallback to an ending is kept: %v", q3)
	}
	if _, ok := q4["logicFallback"]; ok {
		t.Errorf("a fallback to nothing is dropped: %v", q4)
	}

	// The legacy questions survive a round trip through blocks.
	original := decodeAny(t, `[{"id":"q1","type":"openText","headline":{"default":"One"},"required":true},{"id":"q2","type":"rating","headline":{"default":"Two"},"required":false,"scale":"number","range":5}]`).([]any)
	encoded, err := json.Marshal(QuestionsToBlocks(original, nil))
	if err != nil {
		t.Fatal(err)
	}
	back, err := blocksToQuestions(encoded, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, original) {
		t.Errorf("round trip = %v, want %v", back, original)
	}
}
