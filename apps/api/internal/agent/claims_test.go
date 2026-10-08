package agent

import "testing"

func TestUnperformedClaim(t *testing.T) {
	cp := []doneAction{{Tool: "create_checkpoint"}}
	rec := []doneAction{{Tool: "record_decision"}}
	tests := []struct {
		reply string
		done  []doneAction
		want  string
	}{
		// The live failure: a model announced a checkpoint without calling the tool.
		{"**Checkpoint created**\n\n- **Label:** CP5", nil, "checkpoint"},
		{"I've created checkpoint CP2 — First shape.", nil, "checkpoint"},
		{"**Decision recorded** — D4: v1 supports SharePoint Online only.", nil, "knowledge item"},
		{"Created a new Idea Space: Biker Platform.", nil, "idea"},
		{"Forked a new branch from CP4.", nil, "branch"},
		{"I generated an implementation prompt for you.", nil, "artifact"},
		// Consistent with what was done.
		{"**Checkpoint created** — CP5.", cp, ""},
		{"Decision recorded as D4.", rec, ""},
		// Describing earlier history or answering questions is fine.
		{"You created CP2 last week, before the pricing change.", nil, ""},
		{"D2 was recorded on 3 March after the interviews.", nil, ""},
		{"Here is what CP4 contains: three decisions and two open questions.", nil, ""},
		{"Why: on-prem SharePoint uses different authentication, so it is out of scope.", nil, ""},
	}
	for _, tt := range tests {
		if got := unperformedClaim(tt.reply, tt.done); got != tt.want {
			t.Errorf("unperformedClaim(%q) = %q, want %q", tt.reply, got, tt.want)
		}
	}
}
