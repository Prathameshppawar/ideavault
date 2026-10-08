package agent

import (
	"regexp"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/models"
)

// coreTools are always offered to the model.
var coreTools = []string{
	"search_vault", "list_ideas", "get_idea", "get_context", "get_checkpoint", "get_decisions",
	"create_idea", "update_idea", "record_decision", "create_checkpoint", "import_conversation",
}

// toolGroups add tools when the request mentions their domain. Keeping the offered
// tool set small reduces prompt tokens (free-tier rate limits) and improves tool choice.
var toolGroups = []struct {
	re    *regexp.Regexp
	tools []string
}{
	// Recording knowledge other than decisions (record_decision is always offered).
	{regexp.MustCompile(`(?i)\b(remember|record|save|log|note|keep track|assum|evidence|insight|learn|lesson|question|unknown|action|task|next step|todo|risk|research|found|interview|data)`),
		[]string{"record_assumption", "record_insight", "record_question", "record_action", "record_evidence"}},
	{regexp.MustCompile(`(?i)\b(fork|branch|bring|merge|direction|alternative)`), []string{"fork_idea", "create_branch", "merge_selected_context", "get_branch", "compare_branches"}},
	{regexp.MustCompile(`(?i)\b(plan|prompt|brief|spec|document|doc|report|memo|proposal|checklist|summary|artifact|requirements|architecture|strategy|write up|markdown|agent)`), []string{"create_artifact", "update_artifact", "get_artifacts", "analyze_artifact"}},
	{regexp.MustCompile(`(?i)\b(done|conclude|wrap|park|abandon|finish|close|implement)`), []string{"conclude_idea", "create_artifact", "get_artifacts"}},
	{regexp.MustCompile(`(?i)\b(delete|remove|erase)\b`), []string{"delete_idea", "delete_artifact", "get_artifacts"}},
	{regexp.MustCompile(`(?i)\bprompt`), []string{"analyze_prompt"}},
	{regexp.MustCompile(`(?i)\b(pattern|habit|blind spot|how do i think|my thinking|tendenc)`), []string{"find_thinking_patterns"}},
	{regexp.MustCompile(`(?i)\b(contradict|conflict|inconsisten|changed (my|our) mind|revers)`), []string{"detect_contradictions", "compare_checkpoint"}},
	{regexp.MustCompile(`(?i)\b(evol|history|over time|timeline|how .* changed|compare checkpoint|cp\d+ (and|vs|to) cp\d+)`), []string{"analyze_idea_evolution", "compare_checkpoint"}},
	{regexp.MustCompile(`(?i)\b(context pack|export|continue (it |this )?in|chatgpt|claude|gemini)`), []string{"build_context_pack", "analyze_external_conversation"}},
	{regexp.MustCompile(`(?i)\b(said|suggested|external|other ai|delta|what changed)`), []string{"analyze_external_conversation"}},
	{regexp.MustCompile(`(?i)\b(conversation|transcript|message|where did i|first (talk|mention)|chat)`), []string{"get_conversation", "analyze_conversation"}},
	{regexp.MustCompile(`(?i)\b(answer|validat|invalidat|complete|done|status|resolved|drop)`), []string{"update_knowledge_status"}},
	{regexp.MustCompile(`(?i)\b(proposal|accept|reject|review|suggestion)`), []string{"review_proposals"}},
	{regexp.MustCompile(`(?i)\b(relate|link|similar|inspired|depends|connection|related)`), []string{"create_relationship", "get_related"}},
	{regexp.MustCompile(`(?i)\b(assumption|evidence|insight|lesson|question|action|task|next step|todo)`), []string{"get_assumptions", "get_evidence", "get_insights", "get_questions", "get_actions", "update_knowledge_status"}},
	{regexp.MustCompile(`(?i)(https?://|\bweb\b|\bpage\b|\bsite\b|\burl\b)`), []string{"web_fetch"}},
	{regexp.MustCompile(`(?i)\b(github|repo|issue)`), []string{"github_list_repos", "github_get_readme", "github_create_issue"}},
	{regexp.MustCompile(`(?i)\bcustom (connector|endpoint|api)`), []string{"custom_get"}},
}

// selectTools picks the tool definitions to offer for this turn.
func (s *Supervisor) selectTools(userMsg string, history []models.Message) []models.ToolDef {
	want := map[string]bool{}
	for _, n := range coreTools {
		want[n] = true
	}
	for _, g := range toolGroups {
		if g.re.MatchString(userMsg) {
			for _, n := range g.tools {
				want[n] = true
			}
		}
	}
	// Tools already used in this run stay available.
	for _, m := range history {
		for _, tc := range m.ToolCalls {
			want[tc.Name] = true
		}
	}
	var defs []models.ToolDef
	for _, t := range s.order {
		if want[t.Name] {
			defs = append(defs, models.ToolDef{Name: t.Name, Description: t.Description, Parameters: t.Params})
		}
	}
	return defs
}
