package eval

import "testing"

func TestCloneMetadataCopiesJSONLikeValuesAndAliasesOpaqueReferences(t *testing.T) {
	type mutableState struct {
		Count int
	}

	custom := &mutableState{Count: 1}
	metadata := map[string]any{
		"custom": custom,
		"json": map[string]any{
			"items": []any{
				map[string]any{"value": "original"},
			},
		},
	}

	cloned := cloneMetadata(metadata)
	clonedCustom, ok := cloned["custom"].(*mutableState)
	if !ok {
		t.Fatalf("cloned custom value has type %T", cloned["custom"])
	}
	if clonedCustom != custom {
		t.Fatalf("custom pointer was deep-copied; opaque references should be copied by reference")
	}
	clonedCustom.Count = 2
	if custom.Count != 2 {
		t.Fatalf("custom pointer mutation did not reach original; got %d", custom.Count)
	}

	clonedJSON := cloned["json"].(map[string]any)
	clonedItems := clonedJSON["items"].([]any)
	clonedNested := clonedItems[0].(map[string]any)
	clonedNested["value"] = "changed"

	originalJSON := metadata["json"].(map[string]any)
	originalItems := originalJSON["items"].([]any)
	originalNested := originalItems[0].(map[string]any)
	if originalNested["value"] == "changed" {
		t.Fatalf("JSON-like nested metadata was aliased into the clone")
	}
}
