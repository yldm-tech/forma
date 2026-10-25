package survey

import (
	"maps"
	"slices"
	"testing"
)

// The published request bodies list exactly the keys the document parser accepts, which refuses every other key.
func TestV3OpenAPIBodiesMatchTheParser(t *testing.T) {
	spec := V3OpenAPI()
	for name, keys := range map[string]map[string]bool{"SurveyDocument": createRootKeys, "SurveyPatch": patchRootKeys} {
		got := slices.Sorted(maps.Keys(spec.Schemas[name].Properties))
		if want := slices.Sorted(maps.Keys(keys)); !slices.Equal(got, want) {
			t.Errorf("%s properties = %q, want %q", name, got, want)
		}
	}
}
