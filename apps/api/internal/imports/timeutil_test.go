package imports

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTimestampUnmarshal(t *testing.T) {
	want := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		json string
		want time.Time
	}{
		{"unix float", `1767225600.0`, want},
		{"unix int", `1767225600`, want},
		{"unix ms", `1767225600000`, want},
		{"numeric string", `"1767225600"`, want},
		{"rfc3339", `"2026-01-01T00:00:00Z"`, want},
		{"rfc3339 nano with offset", `"2026-01-01T05:30:00.000000+05:30"`, want},
		{"sql style", `"2026-01-01 00:00:00"`, want},
		{"null", `null`, time.Time{}},
		{"garbage string", `"yesterday"`, time.Time{}},
		{"negative", `-5`, time.Time{}},
		{"object", `{"a":1}`, time.Time{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v struct {
				T Timestamp `json:"t"`
			}
			if err := json.Unmarshal([]byte(`{"t":`+tt.json+`}`), &v); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !v.T.Equal(tt.want) {
				t.Errorf("got %v, want %v", v.T.Time, tt.want)
			}
			if tt.want.IsZero() != (v.T.Ptr() == nil) {
				t.Errorf("Ptr() nil-ness mismatch")
			}
		})
	}
}

func TestUnixSecondsKeepsMicroseconds(t *testing.T) {
	got := UnixSeconds(1767225600.123456)
	if got == nil || got.Nanosecond() != 123456000 {
		t.Fatalf("got %v", got)
	}
}
