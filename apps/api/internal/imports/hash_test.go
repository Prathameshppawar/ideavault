package imports

import "testing"

func conv(title string, msgs ...NormalizedMessage) NormalizedConversation {
	return NormalizedConversation{Title: title, Messages: msgs}
}

func TestContentHash(t *testing.T) {
	base := conv("Trip ideas",
		NormalizedMessage{Role: RoleUser, Content: "How do riders create trips?"},
		NormalizedMessage{Role: RoleAssistant, Content: "With a start point and waypoints."},
	)
	h := ContentHash(base)
	if len(h) != 64 {
		t.Fatalf("hash %q is not hex sha256", h)
	}
	same := []NormalizedConversation{
		conv("Another title", base.Messages...),
		conv("", NormalizedMessage{Role: "User", Content: "  How do riders\ncreate trips? "},
			NormalizedMessage{Role: RoleAssistant, Content: "With a start point and   waypoints.", Model: "gpt-5"},
			NormalizedMessage{Role: RoleAssistant, Content: "   "}),
	}
	for i, c := range same {
		if got := ContentHash(c); got != h {
			t.Errorf("variant %d: hash changed", i)
		}
	}
	different := []NormalizedConversation{
		conv("", base.Messages[1], base.Messages[0]),
		conv("", NormalizedMessage{Role: RoleAssistant, Content: base.Messages[0].Content}, base.Messages[1]),
		conv("", base.Messages[0]),
		conv("", NormalizedMessage{Role: RoleUser, Content: "How do riders create trips?With a start point and waypoints."}),
	}
	for i, c := range different {
		if ContentHash(c) == h {
			t.Errorf("different conversation %d has the same hash", i)
		}
	}
}
