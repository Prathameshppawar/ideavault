package imports

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
)

// Timestamp is a lenient JSON time: it accepts unix seconds (float or int),
// unix milliseconds (values above 1e12), numeric strings, RFC 3339 strings and
// null. Unparseable values decode to the zero Timestamp instead of failing, so
// one odd timestamp never rejects a whole conversation.
type Timestamp struct {
	time.Time
}

// UnmarshalJSON implements json.Unmarshaler and never returns an error.
func (t *Timestamp) UnmarshalJSON(b []byte) error {
	t.Time = time.Time{}
	b = bytes.TrimSpace(b)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		return nil
	}
	if b[0] == '"' {
		var s string
		if json.Unmarshal(b, &s) == nil {
			if p := ParseTime(s); p != nil {
				t.Time = *p
			}
		}
		return nil
	}
	if f, err := strconv.ParseFloat(string(b), 64); err == nil {
		if p := UnixSeconds(f); p != nil {
			t.Time = *p
		}
	}
	return nil
}

// MarshalJSON encodes the timestamp as RFC 3339 (or null when zero).
func (t Timestamp) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(t.UTC().Format(time.RFC3339Nano))
}

// Ptr returns a pointer to the UTC time, or nil when the timestamp is zero.
func (t Timestamp) Ptr() *time.Time {
	if t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}

// UnixSeconds converts unix seconds (or milliseconds when the value is larger
// than 1e12) to a UTC time. It returns nil for zero, negative, NaN or
// absurdly large values.
func UnixSeconds(f float64) *time.Time {
	if math.IsNaN(f) || math.IsInf(f, 0) || f <= 0 {
		return nil
	}
	if f > 1e12 { // milliseconds
		f /= 1000
	}
	if f > 1e11 { // beyond year 5000: garbage
		return nil
	}
	sec, frac := math.Modf(f)
	t := time.Unix(int64(sec), int64(frac*1e9)).UTC()
	// Round to microseconds: float seconds carry noise below that.
	t = t.Round(time.Microsecond)
	return &t
}

var timeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// ParseTime parses common timestamp encodings (RFC 3339 variants, SQL-style
// datetimes, numeric unix seconds/ms) and returns a UTC time, or nil.
func ParseTime(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return UnixSeconds(f)
	}
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			u := t.UTC()
			return &u
		}
	}
	return nil
}
