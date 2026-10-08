package chatgptv2

import (
	"reflect"
	"testing"
)

func TestDecodeJSString(t *testing.T) {
	tests := []struct {
		lit, want string
	}{
		{`"plain"`, "plain"},
		{`"line\nbreak \"quoted\" <b>"`, "line\nbreak \"quoted\" <b>"},
		{`'single \'quoted\''`, "single 'quoted'"},
		{`"hex \x41\x42"`, "hex AB"},
		{`"brace \u{1F680}"`, "brace \U0001F680"},
		{`"pair 🚀"`, "pair \U0001F680"},
		{`"vertical\vtab"`, "vertical\vtab"},
		{`"identity \q escape"`, "identity q escape"},
		{"\"line \\\ncontinued\"", "line continued"},
	}
	for _, tt := range tests {
		got, err := decodeJSString(tt.lit)
		if err != nil || got != tt.want {
			t.Errorf("decodeJSString(%s) = %q, %v; want %q", tt.lit, got, err, tt.want)
		}
	}
	for _, bad := range []string{`"`, `"\x4"`, `"\u12"`, `"\u{110000}"`} {
		if _, err := decodeJSString(bad); err == nil {
			t.Errorf("decodeJSString(%s) succeeded", bad)
		}
	}
}

func TestExtractEnqueuePayloads(t *testing.T) {
	page := `<script>a.streamController.enqueue("first\n");</script>` +
		`<script>a.streamController.enqueue( 'second' );</script>` +
		`<script>a.streamController.enqueue(variable);</script>` +
		`<script>a.streamController.enqueue("unterminated`
	got := extractEnqueuePayloads(page)
	want := []string{"first\n", "second"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}
