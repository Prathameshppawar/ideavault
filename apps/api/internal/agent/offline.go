package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/models"
)

// OfflinePlanner is a deterministic, rule-based stand-in for a language model. It maps
// common IdeaVault requests to real tool calls on real data and writes templated answers
// from the tool results. It is used when no AI provider is configured and in automated
// tests. It never pretends to be an LLM: answers are explicitly assembled from vault data.
type OfflinePlanner struct{}

// NewOfflinePlanner returns the planner provider ("mock").
func NewOfflinePlanner() *OfflinePlanner { return &OfflinePlanner{} }

// ID implements models.Provider.
func (p *OfflinePlanner) ID() string { return models.ProviderMock }

// Complete implements models.Provider.
func (p *OfflinePlanner) Complete(ctx context.Context, req models.Request) (*models.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resp := p.plan(req)
	resp.Model = models.MockModel
	resp.Usage = models.Usage{InputTokens: models.EstimateRequestTokens(req), OutputTokens: models.EstimateTokens(resp.Content), Estimated: true}
	return resp, nil
}

// Stream implements models.Provider by chunking the planned text.
func (p *OfflinePlanner) Stream(ctx context.Context, req models.Request, onDelta models.StreamHandler) (*models.Response, error) {
	resp, err := p.Complete(ctx, req)
	if err != nil {
		return nil, err
	}
	if onDelta != nil && resp.Content != "" {
		words := strings.SplitAfter(resp.Content, " ")
		for i := 0; i < len(words); i += 6 {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			end := i + 6
			if end > len(words) {
				end = len(words)
			}
			onDelta(strings.Join(words[i:end], ""))
		}
	}
	return resp, nil
}

// toolOutcome is a parsed tool result from the conversation so far.
type toolOutcome struct {
	name string
	ok   bool
	data map[string]any
	err  string
	sum  string
}

var (
	reURL          = regexp.MustCompile(`https?://[^\s)>\]]+`)
	reNewIdea      = regexp.MustCompile(`(?i)\b(new idea|i have an idea|i've got an idea|i got an idea|i had an idea|an idea for|idea:|idea about|thinking about building)\b`)
	reCreateAnyway = regexp.MustCompile(`(?i)\b(create (it )?(as )?(a )?new|new one|separate idea|create anyway|make it new)\b`)
	reContinue     = regexp.MustCompile(`(?i)\bcontinue\b(.*?)(?:\bfrom\b\s*(?:checkpoint|cp)\s*#?(\d+))?\s*[.!?]*$`)
	reCP           = regexp.MustCompile(`(?i)(?:checkpoint|cp)\s*#?(\d+)`)
	reWhy          = regexp.MustCompile(`(?i)\bwhy (did|do|have) (we|i) (decide|choose|chose|pick|go with|reject|drop|skip)\b(.*)`)
	reFirst        = regexp.MustCompile(`(?i)\b(where|when) did i (first|originally)\b|\bfind where i first\b|\bfirst (talk|mention|discuss)`)
	reFork         = regexp.MustCompile(`(?i)\bfork\b`)
	reBring        = regexp.MustCompile(`(?i)\bbring\b(.*?)(?:checkpoint|cp)\s*#?(\d+)`)
	reCheckpoint   = regexp.MustCompile(`(?i)\b(create|make|save|take|add)\b.{0,12}\bcheckpoint\b|\bcheckpoint (this|it|now)\b`)
	reRemember     = regexp.MustCompile(`(?i)\b(remember|record|save|log|note)\b.{0,30}?\b(as an? |this )?(decision|assumption|insight|evidence|question|action|next step|task|learning)\b`)
	reConclude     = regexp.MustCompile(`(?i)\b(done with this idea|we're done|we are done|i'm done|conclude|wrap (it )?up|close (this|the) idea|finished with)\b`)
	reContextPack  = regexp.MustCompile(`(?i)\bcontext pack\b|\bexport (my )?(thinking|context)\b|\bcontinue (this )?in (chatgpt|claude|gemini)\b`)
	reDelete       = regexp.MustCompile(`(?i)\bdelete\b.{0,20}\bidea\b`)
	rePrompt       = regexp.MustCompile(`(?i)\b(analy[sz]e|evaluate|rate|review|improve)\b.{0,15}\bprompt\b|\bhow good (is|was) my prompt\b`)
	reThinking     = regexp.MustCompile(`(?i)\bthinking patterns?\b|\bhow do i think\b|\bblind spots?\b|\bmy patterns\b|\bmy (prompting|thinking) habits\b`)
	reContra       = regexp.MustCompile(`(?i)\bcontradict`)
	reCompare      = regexp.MustCompile(`(?i)\bcompare\b.{0,20}\bbranch`)
	reEvolve       = regexp.MustCompile(`(?i)\bevol(ve|ved|ution)\b|\bhow (has|did) (it|this|the idea) change\b`)
	reList         = regexp.MustCompile(`(?i)\b(list|show|what are) (all )?(my )?ideas\b|\bwhich ideas\b`)
	reExternal     = regexp.MustCompile(`(?i)\b(chatgpt|claude|gemini|another ai|external ai)\b.{0,40}\b(said|says|suggested|conversation|chat)\b.*\b(compare|delta|merge|changes?|what changed)\b`)
)

var artifactPhrases = []struct {
	re  *regexp.Regexp
	typ string
}{
	{regexp.MustCompile(`(?i)\b(prompt|brief)\b.{0,60}\b(coding|implementation|developer|engineering) agent\b|\bimplementation prompt\b|\bcoding prompt\b`), "IMPLEMENTATION_PROMPT"},
	{regexp.MustCompile(`(?i)\baction plan\b|\bmvp plan\b`), "ACTION_PLAN"},
	{regexp.MustCompile(`(?i)\bproduct brief\b`), "PRODUCT_BRIEF"},
	{regexp.MustCompile(`(?i)\bresearch report\b`), "RESEARCH_REPORT"},
	{regexp.MustCompile(`(?i)\bstrategy (doc|document)\b|\bstrategy\b`), "STRATEGY"},
	{regexp.MustCompile(`(?i)\b(tech(nical)? spec(ification)?|spec)\b`), "TECHNICAL_SPEC"},
	{regexp.MustCompile(`(?i)\barchitecture (doc|document)\b`), "ARCHITECTURE"},
	{regexp.MustCompile(`(?i)\bdecision memo\b`), "DECISION_MEMO"},
	{regexp.MustCompile(`(?i)\bproposal\b`), "PROPOSAL"},
	{regexp.MustCompile(`(?i)\bchecklist\b`), "CHECKLIST"},
	{regexp.MustCompile(`(?i)\bmeeting brief\b`), "MEETING_BRIEF"},
	{regexp.MustCompile(`(?i)\bexecutive summary\b`), "EXECUTIVE_SUMMARY"},
	{regexp.MustCompile(`(?i)\bexperiment plan\b`), "EXPERIMENT_PLAN"},
	{regexp.MustCompile(`(?i)\brequirements (doc|document)\b|\bprd\b`), "REQUIREMENTS"},
	{regexp.MustCompile(`(?i)\breference doc(ument)?\b`), "REFERENCE"},
}

func detectArtifact(msg string) string {
	if !regexp.MustCompile(`(?i)\b(create|generate|give me|write|make|turn|draft|produce|build|prepare|i need)\b`).MatchString(msg) {
		return ""
	}
	for _, a := range artifactPhrases {
		if a.re.MatchString(msg) {
			return a.typ
		}
	}
	return ""
}

func detectOutcome(msg string) string {
	l := strings.ToLower(msg)
	switch {
	case strings.Contains(l, "action plan"):
		return "ACTION_PLAN"
	case regexp.MustCompile(`\b(implement|build it|coding agent|implementation)\b`).MatchString(l):
		return "IMPLEMENT"
	case strings.Contains(l, "park"):
		return "PARK"
	case regexp.MustCompile(`\b(abandon|drop it|kill it|give up)\b`).MatchString(l):
		return "ABANDON"
	case strings.Contains(l, "research"):
		return "RESEARCH_COMPLETE"
	case strings.Contains(l, "proposal"):
		return "PROPOSAL"
	case strings.Contains(l, "reference"):
		return "REFERENCE"
	case strings.Contains(l, "merge"):
		return "MERGE"
	}
	return ""
}

func (p *OfflinePlanner) plan(req models.Request) *models.Response {
	// Find the latest user message and the tool outcomes that followed it.
	last := -1
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == models.RoleUser {
			last = i
			break
		}
	}
	if last < 0 {
		return &models.Response{Content: "How can I help with your ideas?", FinishReason: "stop"}
	}
	msg := req.Messages[last].Content
	prevUser := ""
	for i := last - 1; i >= 0; i-- {
		if req.Messages[i].Role == models.RoleUser {
			prevUser = req.Messages[i].Content
			break
		}
	}
	var done []toolOutcome
	for _, m := range req.Messages[last+1:] {
		if m.Role != models.RoleTool {
			continue
		}
		var payload struct {
			OK      bool           `json:"ok"`
			Summary string         `json:"summary"`
			Data    map[string]any `json:"data"`
			Error   string         `json:"error"`
		}
		_ = json.Unmarshal([]byte(m.Content), &payload)
		done = append(done, toolOutcome{name: m.Name, ok: payload.OK, data: payload.Data, err: payload.Error, sum: payload.Summary})
	}
	step := len(done)
	call := func(name string, args map[string]any) *models.Response {
		b, _ := json.Marshal(args)
		return &models.Response{ToolCalls: []models.ToolCall{{ID: fmt.Sprintf("call_%d_%s", step+1, name), Name: name, Arguments: b}}, FinishReason: "tool_calls"}
	}
	say := func(text string) *models.Response { return &models.Response{Content: text, FinishReason: "stop"} }
	lastOut := func() *toolOutcome {
		if len(done) == 0 {
			return nil
		}
		return &done[len(done)-1]
	}
	if lo := lastOut(); lo != nil && !lo.ok {
		return say(fmt.Sprintf("I couldn't complete that step (%s): %s", strings.ReplaceAll(lo.name, "_", " "), lo.err))
	}
	focus := strings.Contains(req.System, "CURRENT FOCUS: idea")
	urls := reURL.FindAllString(msg, -1)
	pasted := looksLikePastedConversation(msg)

	switch {
	// ---------- delete (destructive; confirmation is enforced by the supervisor) ----------
	case reDelete.MatchString(msg):
		if step == 0 {
			return call("delete_idea", map[string]any{"idea": ideaNameAfter(msg, "delete")})
		}
		return say(fmt.Sprintf("Deleted **%s** and its history.", str(done[0].data, "deleted")))

	// ---------- create anyway after duplicate warning ----------
	case reCreateAnyway.MatchString(msg) && prevUser != "":
		if step == 0 {
			return call("create_idea", map[string]any{"origin_text": prevUser, "force_new": true})
		}
		return say(createdText(done[0].data, nil))

	// ---------- new idea ----------
	case reNewIdea.MatchString(msg) && !reContinue.MatchString(msg):
		body := strings.TrimSpace(reURL.ReplaceAllString(msg, ""))
		if len([]rune(body)) < 30 && len(urls) == 0 && !pasted {
			return say("I'd love to hear it. What's the idea — and what made you think of it?\n\nIf you already discussed it somewhere (ChatGPT, Claude, Gemini…), paste the conversation or a public share link and I'll preserve it as the idea's origin.")
		}
		origin := body
		if pasted {
			origin = strings.TrimSpace(firstParagraph(msg))
		}
		switch step {
		case 0:
			return call("create_idea", map[string]any{"origin_text": origin})
		case 1:
			if created, _ := done[0].data["created"].(bool); !created {
				return say(duplicateText(done[0].data))
			}
			if len(urls) > 0 {
				return call("import_conversation", map[string]any{"url": urls[0], "extract": true})
			}
			if pasted {
				return call("import_conversation", map[string]any{"text": msg, "extract": true})
			}
			return say(createdText(done[0].data, nil))
		default:
			return say(createdText(done[0].data, &done[1]))
		}

	// ---------- import a conversation into the focused idea ----------
	case (len(urls) > 0 || pasted) && regexp.MustCompile(`(?i)\b(conversation|chat|import|discussed|transcript|share)\b`).MatchString(msg) && !reExternal.MatchString(msg):
		if step == 0 {
			args := map[string]any{"extract": true}
			if len(urls) > 0 {
				args["url"] = urls[0]
			} else {
				args["text"] = msg
			}
			if !focus {
				args["new_idea"] = true
			}
			return call("import_conversation", args)
		}
		return say(importText(done[0]))

	// ---------- external AI delta ----------
	case reExternal.MatchString(msg):
		if step == 0 {
			args := map[string]any{}
			if len(urls) > 0 {
				args["url"] = urls[0]
			} else {
				args["text"] = msg
			}
			return call("analyze_external_conversation", args)
		}
		d := done[0].data
		return say(fmt.Sprintf("I compared the external conversation with your current thinking: %s.\n\nNothing has been merged yet — [review the changes](%s) and merge the ones you agree with.", str(done[0].data, "summary"), str(d, "review_link")))

	// ---------- fork ----------
	case reFork.MatchString(msg) && reCP.MatchString(msg):
		if step == 0 {
			cps := reCP.FindAllStringSubmatch(msg, -1)
			args := map[string]any{"from_checkpoint": "CP" + cps[0][1]}
			if name := ideaNameBetween(msg, "fork", "from"); name != "" {
				args["idea"] = name
			}
			if b := reBring.FindStringSubmatch(msg); b != nil {
				kinds := kindsFromText(b[1])
				args["bring"] = []map[string]any{{"checkpoint": "CP" + b[2], "kinds": kinds}}
			}
			if n := regexp.MustCompile(`(?i)\b(?:called|named)\s+["“]?([^"”.]+)`).FindStringSubmatch(msg); n != nil {
				args["name"] = strings.TrimSpace(n[1])
			}
			return call("fork_idea", args)
		}
		d := done[0].data
		br, _ := d["branch"].(map[string]any)
		text := fmt.Sprintf("Forked a new branch **[%s](%s)** from %s.\n\n- Inherited **%v** item(s) exactly as they were at that checkpoint.", str(br, "name"), str(br, "link"), str(d, "forked_from"), d["inherited_from_checkpoint"])
		if n, _ := d["selected_later_items"].(float64); n > 0 {
			text += fmt.Sprintf("\n- Brought **%v** selected item(s) from later thinking.", n)
		}
		text += fmt.Sprintf("\n- Base checkpoint: %s. The original branch is unchanged.", str(d, "base_checkpoint"))
		return say(text)

	// ---------- continue from checkpoint ----------
	case reContinue.MatchString(msg) && !reContextPack.MatchString(msg): // "continue this in ChatGPT" asks for a context pack
		m := reContinue.FindStringSubmatch(msg)
		name := cleanIdeaName(m[1])
		if m[2] != "" {
			switch step {
			case 0:
				args := map[string]any{"checkpoint": "CP" + m[2]}
				if name != "" {
					args["idea"] = name
				}
				return call("get_checkpoint", args)
			case 1:
				return call("get_context", map[string]any{"checkpoint": "CP" + m[2], "query": msg})
			default:
				return say(continueText(done[0].data, done[1].data))
			}
		}
		if step == 0 {
			args := map[string]any{}
			if name != "" {
				args["idea"] = name
			}
			return call("get_idea", args)
		}
		return say(ideaText(done[0].data))

	// ---------- why did we decide ----------
	case reWhy.MatchString(msg):
		topic := strings.Trim(reWhy.FindStringSubmatch(msg)[4], " ?.!")
		switch step {
		case 0:
			return call("search_vault", map[string]any{"query": topic, "types": []string{"decision"}})
		case 1:
			res, _ := done[0].data["results"].([]any)
			if len(res) == 0 {
				return say(fmt.Sprintf("I couldn't find a recorded decision about “%s” in your vault. If you decided it, tell me and I'll record it with the rationale.", topic))
			}
			top, _ := res[0].(map[string]any)
			return call("get_decisions", map[string]any{"idea": str(top, "idea_id"), "item": str(top, "id")})
		default:
			return say(whyText(done[1].data))
		}

	// ---------- where did I first talk about ----------
	case reFirst.MatchString(msg):
		if step == 0 {
			return call("search_vault", map[string]any{"query": msg, "order": "oldest"})
		}
		return say(firstText(done[0].data))

	// ---------- checkpoint ----------
	case reCheckpoint.MatchString(msg):
		if step == 0 {
			args := map[string]any{}
			if n := regexp.MustCompile(`(?i)\b(?:called|named|titled)\s+["“]?([^"”]+)`).FindStringSubmatch(msg); n != nil {
				args["title"] = strings.TrimSpace(strings.Trim(n[1], ".!"))
			}
			return call("create_checkpoint", args)
		}
		d := done[0].data
		return say(fmt.Sprintf("Created checkpoint **[%s](%s)** — %s\n\n%s\n\nCheckpoints are immutable; you can always come back to or fork from %s.", str(d, "label"), str(d, "link"), str(d, "title"), str(d, "summary"), str(d, "label")))

	// ---------- explicit memory commands ----------
	case reRemember.MatchString(msg):
		kind := strings.ToLower(reRemember.FindStringSubmatch(msg)[3])
		tool := map[string]string{"decision": "record_decision", "assumption": "record_assumption", "insight": "record_insight", "learning": "record_insight",
			"evidence": "record_evidence", "question": "record_question", "action": "record_action", "next step": "record_action", "task": "record_action"}[kind]
		stmt := statementFrom(msg)
		if stmt == "" {
			stmt = prevUser
		}
		if step == 0 {
			return call(tool, map[string]any{"statement": stmt, "origin": "SOURCE", "source_excerpt": stmt})
		}
		d := done[0].data
		return say(fmt.Sprintf("Saved as %s **[%s](%s)**: %s", kind, str(d, "label"), str(d, "link"), str(d, "statement")))

	// ---------- conclude ----------
	case reConclude.MatchString(msg):
		outcome := detectOutcome(msg)
		if outcome == "" {
			if step == 0 {
				return call("get_idea", map[string]any{})
			}
			sc, _ := done[0].data["suggested_conclusion"].(map[string]any)
			idea, _ := done[0].data["idea"].(map[string]any)
			return say(fmt.Sprintf("Before closing **%s**, here's my suggestion: conclude it as **%s** — %s\n\nShall I go ahead? You can also choose: implement, action plan, research complete, decision, proposal, reference, park, abandon or merge into another idea. Concluding never deletes anything.",
				str(idea, "title"), str(sc, "outcome"), str(sc, "why")))
		}
		if step == 0 {
			args := map[string]any{"outcome": outcome}
			if t := detectArtifact(msg); t != "" {
				args["generate_artifacts"] = []string{t}
			} else if outcome == "ACTION_PLAN" {
				args["generate_artifacts"] = []string{"ACTION_PLAN"}
			}
			return call("conclude_idea", args)
		}
		return say(concludeText(done[0].data))

	// ---------- artifacts ----------
	case detectArtifact(msg) != "":
		if step == 0 {
			return call("create_artifact", map[string]any{"type": detectArtifact(msg), "instructions": msg})
		}
		d := done[0].data
		return say(fmt.Sprintf("Created **[%s](%s)** (%s, v1) from your recorded thinking — %v item(s) of provenance, %v cited.\n\nYou can view, edit, regenerate, copy or download it as Markdown.\n\n%s",
			str(d, "title"), str(d, "link"), strings.ReplaceAll(strings.ToLower(str(d, "type")), "_", " "), d["provenance_items"], d["cited_items"], quote(str(d, "preview"), 600)))

	case reContextPack.MatchString(msg):
		if step == 0 {
			return call("build_context_pack", map[string]any{"objective": msg})
		}
		d := done[0].data
		return say(fmt.Sprintf("Built a context pack: **[%s](%s)** (~%v tokens). Copy or download it (Markdown/JSON) and paste it into ChatGPT, Claude or Gemini. When you come back, paste that conversation here and I'll show what changed before merging.", str(d, "title"), str(d, "link"), d["token_estimate"]))

	case rePrompt.MatchString(msg):
		if step == 0 {
			args := map[string]any{}
			if q := regexp.MustCompile(`(?s)["“](.{10,})["”]`).FindStringSubmatch(msg); q != nil {
				args["text"] = q[1]
			}
			return call("analyze_prompt", args)
		}
		return say(promptText(done[0].data))

	case reThinking.MatchString(msg):
		if step == 0 {
			return call("find_thinking_patterns", map[string]any{})
		}
		return say(thinkingText(done[0].data))

	case reContra.MatchString(msg):
		if step == 0 {
			return call("detect_contradictions", map[string]any{})
		}
		return say(contradictionText(done[0]))

	case reCompare.MatchString(msg):
		names := regexp.MustCompile(`(?i)compare\s+(?:the\s+)?(.+?)\s+(?:and|with|vs\.?|versus)\s+(.+?)(?:\s+branch(?:es)?)?[.?!]*$`).FindStringSubmatch(msg)
		if names == nil {
			if step == 0 {
				return call("get_idea", map[string]any{})
			}
			return say("Which two branches should I compare? " + branchList(done[0].data))
		}
		if step == 0 {
			return call("compare_branches", map[string]any{"branch_a": strings.TrimSpace(strings.TrimSuffix(names[1], " branch")), "branch_b": strings.TrimSpace(strings.TrimSuffix(names[2], " branch"))})
		}
		return say(str(done[0].data, "narrative"))

	case reEvolve.MatchString(msg):
		if step == 0 {
			return call("analyze_idea_evolution", map[string]any{})
		}
		return say(str(done[0].data, "narrative"))

	case reList.MatchString(msg):
		if step == 0 {
			return call("list_ideas", map[string]any{})
		}
		return say(listText(done[0].data))
	}

	// ---------- fallback: retrieve and report ----------
	if focus {
		if step == 0 {
			return call("get_context", map[string]any{"query": msg})
		}
		return say("Here is what your vault records that relates to this (offline mode assembles answers from your data; add an AI provider under **Models** for conversational answers):\n\n```\n" + trimLines(str(done[0].data, "context"), 60) + "\n```")
	}
	if step == 0 {
		return call("search_vault", map[string]any{"query": msg})
	}
	return say(searchText(done[0].data))
}

// ---------- text helpers ----------

func str(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[k]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return ""
}

func looksLikePastedConversation(msg string) bool {
	return regexp.MustCompile(`(?im)^\s*(you said:|chatgpt said:|user:|assistant:|human:|ai:|claude:|gemini:|\*\*user\*\*|\*\*assistant\*\*|## user|## assistant)`).MatchString(msg) && len(msg) > 120
}

func firstParagraph(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "\n\n"); i > 0 {
		return s[:i]
	}
	if i := strings.Index(s, "\n"); i > 0 {
		return s[:i]
	}
	return s
}

func cleanIdeaName(s string) string {
	s = strings.TrimSpace(s)
	s = regexp.MustCompile(`(?i)^(with |working on |on |the |my )+`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`(?i)\b(idea|from|please)\b\s*$`).ReplaceAllString(s, "")
	return strings.Trim(strings.TrimSpace(s), ".,!?")
}

func ideaNameBetween(msg, after, before string) string {
	re := regexp.MustCompile(`(?i)\b` + after + `\b\s+(.*?)\s+\b` + before + `\b`)
	m := re.FindStringSubmatch(msg)
	if m == nil {
		return ""
	}
	name := cleanIdeaName(m[1])
	if regexp.MustCompile(`(?i)^(this|it|this idea|that)$`).MatchString(name) {
		return ""
	}
	return name
}

func ideaNameAfter(msg, word string) string {
	re := regexp.MustCompile(`(?i)\b` + word + `\b\s+(?:the\s+)?(?:idea\s+)?["“]?(.+?)["”]?(?:\s+idea)?[.!?]*$`)
	m := re.FindStringSubmatch(msg)
	if m == nil {
		return ""
	}
	return cleanIdeaName(m[1])
}

func kindsFromText(s string) []string {
	l := strings.ToLower(s)
	var out []string
	add := func(k string) {
		for _, x := range out {
			if x == k {
				return
			}
		}
		out = append(out, k)
	}
	if strings.Contains(l, "research") || strings.Contains(l, "evidence") || strings.Contains(l, "finding") {
		add("evidence")
		add("insight")
	}
	for _, k := range []string{"decision", "assumption", "insight", "question", "action"} {
		if strings.Contains(l, k) {
			add(k)
		}
	}
	if len(out) == 0 {
		out = []string{"evidence", "insight"}
	}
	return out
}

func statementFrom(msg string) string {
	if i := strings.Index(msg, ":"); i >= 0 && i < len(msg)-3 {
		return strings.TrimSpace(msg[i+1:])
	}
	if m := regexp.MustCompile(`(?i)\bthat\s+(.{8,})$`).FindStringSubmatch(msg); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func quote(s string, n int) string {
	s = strings.TrimSpace(trim(s, n))
	if s == "" {
		return ""
	}
	return "> " + strings.ReplaceAll(s, "\n", "\n> ")
}

func trimLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = append(lines[:n], "…")
	}
	return strings.Join(lines, "\n")
}

func createdText(d map[string]any, imp *toolOutcome) string {
	idea, _ := d["idea"].(map[string]any)
	text := fmt.Sprintf("Created a new Idea Space: **[%s](%s)** with a **Main** branch. I preserved your original words as the idea's origin.", str(idea, "title"), str(idea, "link"))
	if imp != nil {
		text += "\n\n" + importText(*imp)
	}
	text += "\n\nTo explore it:\n1. Who exactly is this for, and what do they do today instead?\n2. What's the one problem it must solve well?\n3. What's the smallest version you could test?\n\nSay “remember this as a decision: …” whenever you settle something, or “create a checkpoint” to snapshot your thinking."
	return text
}

func duplicateText(d map[string]any) string {
	dups, _ := d["possible_duplicates"].([]any)
	var lines []string
	for _, x := range dups {
		m, _ := x.(map[string]any)
		lines = append(lines, fmt.Sprintf("- **[%s](%s)** (%s)", str(m, "idea"), str(m, "link"), strings.ToLower(str(m, "status"))))
	}
	return "This looks very similar to an idea you already have:\n\n" + strings.Join(lines, "\n") + "\n\nShould I continue there, or **create it as a new idea**?"
}

func importText(o toolOutcome) string {
	d := o.data
	convs, _ := d["conversations"].([]any)
	props, _ := d["proposals_PENDING_REVIEW"].([]any)
	var b strings.Builder
	if len(convs) == 0 {
		dups, _ := d["duplicates_skipped"].([]any)
		if len(dups) > 0 {
			m, _ := dups[0].(map[string]any)
			if idea := str(m, "idea"); idea != "" {
				return fmt.Sprintf("That conversation is already in your vault — it belongs to **[%s](%s)**, so I didn't import a duplicate. Let's continue there.", idea, str(m, "idea_link"))
			}
			return fmt.Sprintf("That conversation is already in your vault ([%s](%s)), so I didn't import a duplicate.", str(m, "conversation"), str(m, "link"))
		}
		return "I couldn't find a conversation to import in that input."
	}
	c, _ := convs[0].(map[string]any)
	fmt.Fprintf(&b, "Imported the conversation **[%s](%s)** from %s, preserved as an untrusted source.", str(c, "title"), str(c, "link"), str(c, "provider"))
	if len(props) > 0 {
		fmt.Fprintf(&b, " I found **%d** candidate item(s); they're **proposals** until you accept them:\n", len(props))
		for i, p := range props {
			if i >= 8 {
				fmt.Fprintf(&b, "- …and %d more\n", len(props)-8)
				break
			}
			m, _ := p.(map[string]any)
			fmt.Fprintf(&b, "\n- %s **%s**: %s", str(m, "kind"), str(m, "label"), trim(str(m, "statement"), 140))
		}
		b.WriteString("\n\nSay “accept all proposals” or review them on the idea page.")
	}
	if flags, _ := d["injection_flags"].([]any); len(flags) > 0 {
		fmt.Fprintf(&b, "\n\n⚠️ The imported text contains instruction-like content (%v). I treated it purely as data and did not act on it.", flags)
	}
	return b.String()
}

func continueText(cp, ctx map[string]any) string {
	c, _ := cp["checkpoint"].(map[string]any)
	idea, _ := cp["idea_at_checkpoint"].(map[string]any)
	var b strings.Builder
	fmt.Fprintf(&b, "Continuing **%s** from **[%s](%s)** (%s, branch %s).\n\n", str(idea, "title"), str(c, "label"), str(c, "link"), str(c, "created_at"), str(c, "branch"))
	if s := str(c, "summary"); s != "" {
		b.WriteString(s + "\n\n")
	}
	know, _ := cp["knowledge_at_checkpoint"].(map[string]any)
	for _, k := range []string{"decision", "question", "assumption", "action"} {
		items, _ := know[k].([]any)
		if len(items) == 0 {
			continue
		}
		fmt.Fprintf(&b, "**%ss at %s**\n", capitalize(k), str(c, "label"))
		for i, it := range items {
			if i >= 6 {
				break
			}
			m, _ := it.(map[string]any)
			fmt.Fprintf(&b, "- [%s](%s) %s\n", str(m, "label"), str(m, "link"), str(m, "statement"))
		}
		b.WriteString("\n")
	}
	b.WriteString("What would you like to work on next from here? (Changes will continue on the current branch; say “fork from " + str(c, "label") + "” to explore a different direction.)")
	return b.String()
}

func ideaText(d map[string]any) string {
	idea, _ := d["idea"].(map[string]any)
	var b strings.Builder
	fmt.Fprintf(&b, "**[%s](%s)** — %s · branch %s\n\n", str(idea, "title"), str(idea, "link"), strings.ToLower(str(idea, "status")), str(d, "current_branch"))
	if s := str(idea, "summary_INTERPRETATION"); s != "" {
		b.WriteString(s + "\n\n")
	}
	know, _ := d["knowledge"].(map[string]any)
	for _, k := range []string{"decisions", "questions"} {
		items, _ := know[k].([]any)
		if len(items) == 0 {
			continue
		}
		fmt.Fprintf(&b, "**%s**\n", capitalize(k))
		for i, it := range items {
			if i >= 6 {
				break
			}
			m, _ := it.(map[string]any)
			fmt.Fprintf(&b, "- [%s](%s) %s\n", str(m, "label"), str(m, "link"), str(m, "statement"))
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func whyText(d map[string]any) string {
	it, _ := d["item"].(map[string]any)
	var b strings.Builder
	fmt.Fprintf(&b, "**[%s](%s)** — %s _(status: %s)_\n\n", str(it, "label"), str(it, "link"), str(it, "statement"), strings.ToLower(str(it, "status")))
	if r := str(it, "rationale"); r != "" {
		fmt.Fprintf(&b, "**Why:** %s\n\n", r)
	} else {
		b.WriteString("_No rationale was recorded with this decision._\n\n")
	}
	if alts, _ := it["alternatives"].([]any); len(alts) > 0 {
		b.WriteString("**Alternatives considered:**\n")
		for _, a := range alts {
			m, _ := a.(map[string]any)
			fmt.Fprintf(&b, "- %s", str(m, "option"))
			if r := str(m, "reason_rejected"); r != "" {
				b.WriteString(" — rejected: " + r)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	if ev, _ := d["supporting_evidence"].([]any); len(ev) > 0 {
		b.WriteString("**Evidence:**\n")
		for _, e := range ev {
			m, _ := e.(map[string]any)
			fmt.Fprintf(&b, "- [%s](%s) %s\n", str(m, "label"), str(m, "link"), str(m, "statement"))
		}
		b.WriteString("\n")
	}
	if src, _ := d["source_message"].(map[string]any); src != nil {
		fmt.Fprintf(&b, "**Source** (%s, %s):\n%s\n\n", str(src, "role"), str(src, "at"), quote(strings.NewReplacer("<untrusted_imported_content>", "", "</untrusted_imported_content>", "").Replace(str(src, "content_SOURCE")), 500))
	} else if ex := str(it, "source_excerpt"); ex != "" {
		fmt.Fprintf(&b, "**Source:**\n%s\n\n", quote(ex, 400))
	}
	if cps, _ := d["present_in_checkpoints"].([]any); len(cps) > 0 {
		fmt.Fprintf(&b, "Captured in checkpoints: %v\n", cps)
	}
	if hist, _ := d["history"].([]any); len(hist) > 1 {
		b.WriteString("\n**How it evolved:**\n")
		for _, h := range hist {
			m, _ := h.(map[string]any)
			fmt.Fprintf(&b, "- %s %s _(%s)_\n", str(m, "label"), str(m, "statement"), strings.ToLower(str(m, "status")))
		}
	}
	return strings.TrimSpace(b.String())
}

func firstText(d map[string]any) string {
	res, _ := d["results"].([]any)
	if len(res) == 0 {
		return "I couldn't find any earlier mention of that in your vault."
	}
	var b strings.Builder
	first, _ := res[0].(map[string]any)
	fmt.Fprintf(&b, "The earliest mention I found is **[%s](%s)** on **%s**", trim(str(first, "title"), 80), str(first, "link"), str(first, "created_at"))
	if i := str(first, "idea"); i != "" {
		fmt.Fprintf(&b, " (idea: %s)", i)
	}
	b.WriteString(":\n\n" + quote(str(first, "snippet"), 300) + "\n")
	if len(res) > 1 {
		b.WriteString("\nLater mentions:\n")
		for i, r := range res[1:] {
			if i >= 4 {
				break
			}
			m, _ := r.(map[string]any)
			fmt.Fprintf(&b, "- %s — [%s](%s)\n", str(m, "created_at"), trim(str(m, "title"), 80), str(m, "link"))
		}
	}
	return b.String()
}

func concludeText(d map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Concluded as **%s** (status now %s). Final checkpoint: **%s** — the full history is preserved.\n\n", str(d, "outcome"), strings.ToLower(str(d, "status")), str(d, "final_checkpoint"))
	fmt.Fprintf(&b, "**What we learned**\n%s\n\n**Key decisions**\n%s\n\n**Still unresolved**\n%s\n", str(d, "learned"), str(d, "decisions"), str(d, "unresolved"))
	if arts, _ := d["artifacts"].([]any); len(arts) > 0 {
		b.WriteString("\n**Artifacts**\n")
		for _, a := range arts {
			m, _ := a.(map[string]any)
			fmt.Fprintf(&b, "- [%s](%s)\n", str(m, "title"), str(m, "link"))
		}
	}
	return b.String()
}

func promptText(d map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**Prompt quality: %v/100**\n\n", d["overall"])
	list := func(title, key string) {
		if xs, _ := d[key].([]any); len(xs) > 0 {
			fmt.Fprintf(&b, "**%s**\n", title)
			for _, x := range xs {
				fmt.Fprintf(&b, "- %v\n", x)
			}
			b.WriteString("\n")
		}
	}
	list("What was good", "good")
	list("What was weak", "weak")
	list("Why it matters", "why_it_matters")
	if ip := str(d, "improved_prompt"); ip != "" {
		b.WriteString("**Improved prompt**\n```\n" + ip + "\n```")
	}
	return b.String()
}

func thinkingText(d map[string]any) string {
	var b strings.Builder
	pats, _ := d["patterns"].([]any)
	if len(pats) == 0 {
		return str(d, "narrative")
	}
	b.WriteString("Observable patterns in your recorded thinking:\n\n")
	for _, p := range pats {
		m, _ := p.(map[string]any)
		fmt.Fprintf(&b, "- **%s** (%s) — %s _%s_\n", str(m, "title"), str(m, "kind"), str(m, "description"), str(m, "metric"))
	}
	b.WriteString("\n_" + str(d, "disclaimer") + "_")
	return b.String()
}

func contradictionText(o toolOutcome) string {
	fs, _ := o.data["findings"].([]any)
	if len(fs) == 0 {
		return "I didn't find contradictions or changed decisions in this branch."
	}
	var b strings.Builder
	b.WriteString(o.sum + ":\n\n")
	for _, f := range fs {
		m, _ := f.(map[string]any)
		a, _ := m["a"].(map[string]any)
		c, _ := m["b"].(map[string]any)
		fmt.Fprintf(&b, "- **%s**: %s ↔ %s — %s\n", strings.ReplaceAll(str(m, "kind"), "_", " "), str(a, "label"), str(c, "label"), str(m, "explanation"))
	}
	return b.String()
}

func branchList(d map[string]any) string {
	brs, _ := d["branches"].([]any)
	var names []string
	for _, x := range brs {
		m, _ := x.(map[string]any)
		names = append(names, "**"+str(m, "name")+"**")
	}
	return "Branches: " + strings.Join(names, ", ")
}

func listText(d map[string]any) string {
	ideas, _ := d["ideas"].([]any)
	if len(ideas) == 0 {
		return "You don't have any ideas yet. Say “I have a new idea” to start one."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "You have %v idea(s):\n\n", d["total"])
	for _, x := range ideas {
		m, _ := x.(map[string]any)
		fmt.Fprintf(&b, "- [%s](%s) — %s · last active %s\n", str(m, "title"), str(m, "link"), strings.ToLower(str(m, "status")), str(m, "last_activity"))
	}
	return b.String()
}

func searchText(d map[string]any) string {
	res, _ := d["results"].([]any)
	if len(res) == 0 {
		return "I couldn't find anything in your vault about that. If it's a new idea, say “I have a new idea: …”."
	}
	var b strings.Builder
	b.WriteString("Here's what I found in your vault:\n\n")
	for i, r := range res {
		if i >= 8 {
			break
		}
		m, _ := r.(map[string]any)
		label := str(m, "label")
		if label != "" {
			label += " "
		}
		fmt.Fprintf(&b, "- %s [%s%s](%s) — %s", str(m, "type"), label, trim(str(m, "title"), 90), str(m, "link"), str(m, "created_at"))
		if idea := str(m, "idea"); idea != "" && str(m, "type") != "idea" {
			fmt.Fprintf(&b, " · %s", idea)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// capitalize upper-cases the first letter of a single ASCII word ("decision" → "Decision").
func capitalize(w string) string {
	if w == "" {
		return w
	}
	return strings.ToUpper(w[:1]) + w[1:]
}
