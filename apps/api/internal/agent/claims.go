package agent

import "regexp"

// actionClaims maps a reply that announces a write ("Checkpoint created", "Decision recorded")
// to the tools that could have performed it in this run.
var actionClaims = []struct {
	what  string
	re    *regexp.Regexp
	tools []string
}{
	{"checkpoint", regexp.MustCompile(`(?i)\b(created|saved|took|taken|made|captured)\b[^.\n]{0,30}\bcheckpoint\b|\bcheckpoint\b[^.\n]{0,20}\b(created|saved|captured)\b`),
		[]string{"create_checkpoint", "conclude_idea", "analyze_external_conversation", "merge_selected_context"}},
	{"knowledge item", regexp.MustCompile(`(?i)\b(recorded|saved|logged|noted|stored|added)\b[^.\n]{0,30}\b(decision|assumption|insight|evidence|question|action item|next action)\b|\b(decision|assumption|insight|evidence|question)\b[^.\n]{0,20}\b(recorded|saved|logged|stored)\b`),
		[]string{"record_decision", "record_assumption", "record_insight", "record_evidence", "record_question", "record_action", "update_knowledge_status", "review_proposals", "import_conversation", "merge_selected_context"}},
	{"idea", regexp.MustCompile(`(?i)\bcreated\b[^.\n]{0,20}\b(new )?idea( space)?\b|\bnew idea space\b`),
		[]string{"create_idea", "import_conversation"}},
	{"branch", regexp.MustCompile(`(?i)\bforked\b|\bcreated\b[^.\n]{0,20}\bbranch\b`),
		[]string{"fork_idea", "create_branch"}},
	{"artifact", regexp.MustCompile(`(?i)\b(created|generated|saved)\b[^.\n]{0,30}\b(artifact|action plan|implementation prompt|product brief|technical spec|decision memo)\b`),
		[]string{"create_artifact", "update_artifact", "conclude_idea"}},
}

// aboutEarlier filters replies that describe earlier history ("you created CP2 last week").
var aboutEarlier = regexp.MustCompile(`(?i)\b(you|was|were|had been|earlier|previously|last (week|time)|on \d)\b[^.\n]{0,12}\b(created|recorded|saved|forked)\b`)

// unperformedClaim returns what the reply claims to have written without a matching
// successful tool call in this run, or "" when the reply is consistent with what was done.
func unperformedClaim(reply string, done []doneAction) string {
	did := map[string]bool{}
	for _, d := range done {
		did[d.Tool] = true
	}
	for _, c := range actionClaims {
		loc := c.re.FindStringIndex(reply)
		if loc == nil {
			continue
		}
		performed := false
		for _, t := range c.tools {
			if did[t] {
				performed = true
				break
			}
		}
		if performed {
			continue
		}
		// Ignore references to earlier history around the match.
		from, to := max(0, loc[0]-40), min(len(reply), loc[1]+10)
		if aboutEarlier.MatchString(reply[from:to]) {
			continue
		}
		return c.what
	}
	return ""
}
