package chatgptv2

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func decodeRoot(t *testing.T, payload string) any {
	t.Helper()
	ts, err := parseTurboStream(payload)
	if err != nil {
		t.Fatalf("parseTurboStream: %v", err)
	}
	return ts.root()
}

func TestTurboStreamDecode(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    any
	}{
		{"object", `[{"_1":2,"_3":-5},"title","Trip",["x"]]`, map[string]any{"title": "Trip"}},
		{"nested and shared", `[{"_1":2,"_3":2},"a",{"_4":5},"b","n",7]`,
			map[string]any{"a": map[string]any{"n": float64(7)}, "b": map[string]any{"n": float64(7)}}},
		{"array of indices with hole", `[[1,-1,2],"x",true]`, []any{"x", nil, true}},
		{"special constants", `[[-2,-3,-4,-5,-6,-7]]`, []any{nil, nil, float64(0), nil, nil, nil}},
		{"date", `[{"_1":2},"at",["D",1767398450000]]`, map[string]any{"at": "2026-01-03T00:00:50Z"}},
		{"promise and error are nil", `[[1,2],["P",1],["E","boom"]]`, []any{nil, nil}},
		{"url and bigint", `[[1,2],["U","https://example.com/"],["B","123"]]`, []any{"https://example.com/", "123"}},
		{"set and map", `[[1,4],["S",2,3],"a","b",["M",2,3]]`, []any{[]any{"a", "b"}, map[string]any{"a": "b"}}},
		{"null object", `[["N",{"_1":2}],"k","v"]`, map[string]any{"k": "v"}},
		{"previously resolved", `[[1],["Z",2],"done"]`, []any{"done"}},
		{"out of range indices", `[{"_1":99,"_77":1},"k"]`, map[string]any{"k": nil}},
		{"non index values ignored", `[[1,"str",{}],"x"]`, []any{"x", nil, nil}},
		{"promise lines ignored", "[{\"_1\":2},\"k\",\"v\"]\nP2:[\"resolved\"]\n", map[string]any{"k": "v"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeRoot(t, tt.payload)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v\nwant %#v", got, tt.want)
			}
		})
	}
}

func TestTurboStreamCycles(t *testing.T) {
	// Object 0 references itself; array 2 contains itself.
	root := decodeRoot(t, `[{"_1":0,"_3":2},"self",[2],"list"]`).(map[string]any)
	self, ok := root["self"].(map[string]any)
	if !ok || reflect.ValueOf(self).Pointer() != reflect.ValueOf(root).Pointer() {
		t.Fatalf("self reference not preserved")
	}
	list := root["list"].([]any)
	if inner, ok := list[0].([]any); !ok || &inner[0] != &list[0] {
		t.Fatalf("array cycle not preserved")
	}
	// Re-encoding a cyclic value must fail cleanly, not loop forever.
	if _, err := marshalBounded(root, 1<<20); !errors.Is(err, errMarshalTooDeep) && !errors.Is(err, errMarshalTooLarge) {
		t.Fatalf("marshalBounded(cycle) err = %v", err)
	}
}

func TestMarshalBoundedLimitsSharedReferenceBlowup(t *testing.T) {
	// Each level references the next twice: 2^40 leaves when expanded.
	var b strings.Builder
	b.WriteString("[")
	const levels = 40
	for i := 0; i < levels; i++ {
		b.WriteString("[")
		b.WriteString(itoa(i + 1))
		b.WriteString(",")
		b.WriteString(itoa(i + 1))
		b.WriteString("],")
	}
	b.WriteString(`"leaf"]`)
	root := decodeRoot(t, b.String())
	if _, err := marshalBounded(root, 1<<20); !errors.Is(err, errMarshalTooLarge) {
		t.Fatalf("err = %v, want errMarshalTooLarge", err)
	}
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

func TestParseTurboStreamErrors(t *testing.T) {
	for _, payload := range []string{"", "P1:[]", "{}", "[", "[]"} {
		if _, err := parseTurboStream(payload); err == nil {
			t.Errorf("parseTurboStream(%q) succeeded", payload)
		}
	}
}

func TestFindObjectWithKey(t *testing.T) {
	ts, err := parseTurboStream(`[{"_1":2},"wrapper",{"_3":4},"linear_conversation",[5],"node"]`)
	if err != nil {
		t.Fatal(err)
	}
	obj, ok := ts.findObjectWithKey("linear_conversation")
	if !ok || !reflect.DeepEqual(obj["linear_conversation"], []any{"node"}) {
		t.Fatalf("got %v %v", obj, ok)
	}
	if _, ok := ts.findObjectWithKey("missing"); ok {
		t.Fatal("found missing key")
	}
}
