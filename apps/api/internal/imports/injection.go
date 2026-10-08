package imports

import (
	"regexp"
	"sort"
	"strings"
)

// Prompt-injection flag ids returned by ScanInjection. They are stable and
// may be persisted, shown in the UI and asserted by evaluations.
const (
	FlagIgnoreInstructions = "ignore_instructions"
	FlagRoleOverride       = "role_override"
	FlagSystemPromptProbe  = "system_prompt_probe"
	FlagDestructiveCommand = "destructive_command"
	FlagToolCallMarkup     = "tool_call_markup"
	FlagChatTemplateTokens = "chat_template_tokens"
	FlagExfiltration       = "exfiltration"
)

// injectionRule flags text matching re, unless the text immediately before
// the match matches negated (e.g. "never share your password"). Every match
// contains at least one of triggers (lowercase literals), so on long texts
// the regexp only runs on windows around trigger occurrences.
type injectionRule struct {
	flag     string
	re       *regexp.Regexp
	negated  *regexp.Regexp
	triggers []string
}

// Scanning limits.
const (
	// negationWindow is how many bytes before a match are checked for negation.
	negationWindow = 48
	// directScanLimit: texts up to this size are matched whole.
	directScanLimit = 4096
	// windowBefore/windowAfter bound the text examined around a trigger.
	windowBefore = 512
	windowAfter  = 1024
)

var negationPrefix = regexp.MustCompile(`(?i)(?:\b(?:never|not|without|avoid|prevent|cannot)|n['’]t)\s+(?:\w+\s+){0,2}$`)

var injectionRules = []injectionRule{
	// "ignore all previous instructions", "disregard the system prompt".
	{flag: FlagIgnoreInstructions, re: regexp.MustCompile(`(?i)\b(?:ignore|disregard|forget|override|bypass)\s+(?:(?:the|of|these|those|my|our)\s+)*(?:all|every|previous|prior|above|earlier|preceding|initial|original|system|developer|your)\b(?:\s+(?:the|of|all|any|previous|prior|above|earlier|preceding|initial|original|system|developer|your|these|those|safety|other|existing|current))*\s+(?:instructions?|prompts?|rules|directives?|guidelines|commands?|context|guardrails|constraints|programming|messages?)\b`), triggers: []string{"ignore", "disregard", "forget", "override", "bypass"}},
	// "ignore everything above", "forget everything you were told".
	{flag: FlagIgnoreInstructions, re: regexp.MustCompile(`(?i)\b(?:ignore|disregard|forget)\s+(?:everything|anything|all)\s+(?:above|previously\s+said|(?:that\s+)?you\s+(?:were|have\s+been|'ve\s+been)\s+told)\b`), triggers: []string{"ignore", "disregard", "forget"}},

	// "you are now DAN", "you are now an unrestricted AI".
	{flag: FlagRoleOverride, re: regexp.MustCompile(`(?i)\byou\s+are\s+now\s+(?:(?:an?|the|in|my)\s+)?(?:\w+\s+){0,3}?(?:unrestricted|unfiltered|uncensored|jailbroken|dan|evil|rogue|admin(?:istrator)?|root|superuser|developer\s+mode|god\s+mode|no\s+longer|free\s+(?:from|of)\s+(?:all\s+)?(?:rules|restrictions|guidelines))\b`), triggers: []string{"you are now"}},
	{flag: FlagRoleOverride, re: regexp.MustCompile(`(?i)\bfrom\s+now\s+on,?\s+(?:you\s+)?(?:will\s+|must\s+|shall\s+|are\s+to\s+)?(?:ignore|disregard|only\s+obey|obey\s+only|only\s+follow|follow\s+only|no\s+longer|act\s+as\s+(?:an?\s+)?(?:unrestricted|unfiltered|jailbroken|dan))\b`), triggers: []string{"from now on"}},
	{flag: FlagRoleOverride, re: regexp.MustCompile(`(?i)\b(?:act|behave|respond|roleplay)\s+as\s+(?:if\s+you\s+(?:are|were)\s+)?(?:an?\s+)?(?:unrestricted|unfiltered|uncensored|jailbroken|evil|dan)\b`), triggers: []string{"unrestricted", "unfiltered", "uncensored", "jailbroken", "evil", "dan"}},
	{flag: FlagRoleOverride, re: regexp.MustCompile(`(?i)\b(?:enable|enter|activate|switch\s+to)\s+(?:developer|god|dan|jailbreak|sudo)\s+mode\b`), triggers: []string{"mode"}},
	{flag: FlagRoleOverride, re: regexp.MustCompile(`(?i)\b(?:new|updated)\s+system\s+(?:instructions?|prompt|directives?)\b|\b(?:new|updated)\s+instructions\s+from\s+(?:the\s+)?(?:system|developer|admin|openai|anthropic)\b`), triggers: []string{"system", "instructions"}},
	{flag: FlagRoleOverride, re: regexp.MustCompile(`(?i)\byou\s+(?:are|must)\s+no\s+longer\s+(?:bound|restricted|limited|an?\s+(?:ai|assistant|language\s+model))\b|\bpretend\s+(?:that\s+)?you\s+(?:have|had)\s+no\s+(?:restrictions|rules|guidelines|limits|filters)\b`), triggers: []string{"no longer", "pretend"}},

	// "reveal your system prompt", "what is your initial prompt".
	{flag: FlagSystemPromptProbe, re: regexp.MustCompile(`(?i)\b(?:reveal|show|print|output|repeat|display|tell|leak|dump|give|share|disclose|expose|recite|paste|write\s+out|spell\s+out)\s+(?:me\s+|us\s+)?(?:all\s+)?(?:of\s+)?(?:your|the)\s+(?:(?:full|entire|exact|original|hidden|initial|complete|secret|internal|verbatim)\s+)*(?:system\s+(?:prompt|message|instructions?)|initial\s+(?:prompt|instructions?)|hidden\s+(?:prompt|instructions?)|developer\s+(?:prompt|message|instructions?)|(?:instructions|prompt|text|words)\s+above|pre-?prompt|meta-?prompt)\b`), triggers: []string{"system", "initial", "hidden", "developer", "above", "prompt"}},
	{flag: FlagSystemPromptProbe, re: regexp.MustCompile(`(?i)\bwhat\s+(?:is|was|are|were)\s+your\s+(?:system\s+prompt|initial\s+(?:prompt|instructions)|(?:hidden|secret|original)\s+(?:prompt|instructions))\b|\brepeat\s+(?:the\s+)?(?:words|text|everything)\s+above\b`), triggers: []string{"prompt", "instructions", "above"}},

	// "delete every idea in the vault", "delete everything", "rm -rf /".
	{flag: FlagDestructiveCommand, negated: negationPrefix, re: regexp.MustCompile(`(?i)\b(?:delete|remove|erase|wipe|destroy|purge|drop|nuke|clear)\s+(?:out\s+)?(?:all|every|each|the\s+entire|the\s+whole|any)\b(?:\s+(?:of|the|my|your|our|their|this|these|single|existing|stored|saved|other|old|previous|current)){0,3}\s+(?:ideas?|notes?|vault|data(?:base)?|db|records?|files?|memor(?:y|ies)|conversations?|projects?|tables?|users?|documents?|accounts?|entries|rows|backups?|messages?|history|repos|repositor(?:y|ies))\b`), triggers: []string{"delete", "remove", "erase", "wipe", "destroy", "purge", "drop", "nuke", "clear"}},
	{flag: FlagDestructiveCommand, negated: negationPrefix, re: regexp.MustCompile(`(?i)\b(?:delete|erase|wipe|destroy|purge|nuke)\s+everything\b|\bdrop\s+(?:database|schema)\b|\brm\s+-(?:rf|fr)\s+(?:/|~|\*|\$home|--no-preserve-root)`), triggers: []string{"delete", "erase", "wipe", "destroy", "purge", "nuke", "drop", "rm -"}},

	// Fake tool invocations / function-call payloads.
	{flag: FlagToolCallMarkup, re: regexp.MustCompile(`(?i)"(?:tool_calls|tool_call|function_call|tool_use)"\s*:|<\s*/?\s*(?:tool_call|tool_use|tool_result|function_calls?|function_results?)\s*>|<\s*invoke\s+name\s*=|\[(?:TOOL_CALLS|TOOL_RESULTS)\]`), triggers: []string{"tool_", "function_", "invoke"}},

	// Chat-template control tokens and fake system delimiters.
	{flag: FlagChatTemplateTokens, re: regexp.MustCompile(`(?i)<\|[a-z_]{2,32}\|>|\[/?INST\]|<</?SYS>>|<\s*/?\s*(?:system|system_prompt)\s*>`), triggers: []string{"<|", "inst]", "<<", "system"}},

	// "send your API key to ...", markdown-image beacons carrying data.
	{flag: FlagExfiltration, negated: negationPrefix, re: regexp.MustCompile(`(?i)\b(?:send|e-?mail|post|upload|forward|transmit|leak|exfiltrate|share|reveal|disclose|dump|give\s+me|tell\s+me)\s+(?:\w+\s+){0,2}?(?:(?:your|the|all|my|any|their)\s+)?(?:api[\s_-]?keys?|secret[\s_-]?keys?|access[\s_-]?tokens?|auth(?:entication)?[\s_-]?tokens?|bearer\s+tokens?|credentials|passwords?|env(?:ironment)?\s+variables|\.env|private[\s_-]?keys?|session\s+(?:cookies|tokens)|ssh\s+keys?)\b`), triggers: []string{"key", "token", "credential", "password", "env", "cookie"}},
	{flag: FlagExfiltration, re: regexp.MustCompile(`(?i)!\[[^\]]{0,100}\]\(https?://[^)\s]{1,300}[?&][a-z_]*(?:key|token|secret|data|prompt|history|conversation)[a-z_]*=`), triggers: []string{"!["}},
}

// invisibleRunes are removed before scanning so "ig\u200bnore" cannot dodge the rules.
var invisibleRunes = strings.NewReplacer(
	"\u200b", "", "\u200c", "", "\u200d", "", "\u2060", "", "\ufeff", "", "\u00ad", "",
)

// ScanInjection reports prompt-injection-looking patterns in text as a sorted
// list of stable flag ids (see the Flag* constants). It only FLAGS content for
// the UI and evaluations: imported content is never altered or executed, and
// an empty result does not make content trusted.
func ScanInjection(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	norm := strings.ToLower(strings.Join(strings.Fields(invisibleRunes.Replace(text)), " "))
	found := map[string]bool{}
	for _, rule := range injectionRules {
		if found[rule.flag] {
			continue
		}
		if ruleMatches(rule, norm) {
			found[rule.flag] = true
		}
	}
	if len(found) == 0 {
		return nil
	}
	flags := make([]string, 0, len(found))
	for f := range found {
		flags = append(flags, f)
	}
	sort.Strings(flags)
	return flags
}

func ruleMatches(rule injectionRule, text string) bool {
	if len(text) <= directScanLimit || len(rule.triggers) == 0 {
		return matchIn(rule, text, 0, len(text))
	}
	for _, w := range triggerWindows(text, rule.triggers) {
		if matchIn(rule, text, w[0], w[1]) {
			return true
		}
	}
	return false
}

// matchIn reports whether rule matches text[start:end], honouring negation
// (checked against the full text before the match).
func matchIn(rule injectionRule, text string, start, end int) bool {
	seg := text[start:end]
	if rule.negated == nil {
		return rule.re.MatchString(seg)
	}
	for _, loc := range rule.re.FindAllStringIndex(seg, -1) {
		abs := start + loc[0]
		if !rule.negated.MatchString(text[max(0, abs-negationWindow):abs]) {
			return true
		}
	}
	return false
}

// triggerWindows returns merged [start, end) windows around every trigger
// occurrence, widened to whitespace so no word is cut in half.
func triggerWindows(text string, triggers []string) [][2]int {
	var pos []int
	for _, t := range triggers {
		for off := 0; off < len(text); {
			i := strings.Index(text[off:], t)
			if i < 0 {
				break
			}
			pos = append(pos, off+i)
			off += i + len(t)
		}
	}
	sort.Ints(pos)
	var out [][2]int
	for _, p := range pos {
		s, e := snapStart(text, p-windowBefore), snapEnd(text, p+windowAfter)
		if n := len(out); n > 0 && s <= out[n-1][1] {
			out[n-1][1] = max(out[n-1][1], e)
			continue
		}
		out = append(out, [2]int{s, e})
	}
	return out
}

// snapStart moves i back to just after a space (at most 64 bytes back).
func snapStart(text string, i int) int {
	if i <= 0 {
		return 0
	}
	if j := strings.LastIndexByte(text[max(0, i-64):i], ' '); j >= 0 {
		return max(0, i-64) + j + 1
	}
	return i
}

// snapEnd moves i forward to a space (at most 64 bytes on).
func snapEnd(text string, i int) int {
	if i >= len(text) {
		return len(text)
	}
	if j := strings.IndexByte(text[i:min(len(text), i+64)], ' '); j >= 0 {
		return i + j
	}
	return i
}

// ScanConversation returns the union of ScanInjection flags over the title
// and every message of conv, sorted.
func ScanConversation(conv NormalizedConversation) []string {
	set := map[string]bool{}
	for _, f := range ScanInjection(conv.Title) {
		set[f] = true
	}
	for _, m := range conv.Messages {
		for _, f := range ScanInjection(m.Content) {
			set[f] = true
		}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}
