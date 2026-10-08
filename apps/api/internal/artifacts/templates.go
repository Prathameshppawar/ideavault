// Package artifacts defines artifact templates and generators. Artifacts are
// Markdown documents produced from an idea's recorded thinking; every claim is
// expected to cite the knowledge items it came from ([D3], [I2], CP4).
package artifacts

import "github.com/Prathameshppawar/ideavault/apps/api/internal/domain"

// Section is one part of an artifact template. Source selects which recorded
// knowledge fills the section in the deterministic renderer.
type Section struct {
	Title    string `json:"title"`
	Guidance string `json:"guidance"`
	Source   string `json:"source"` // purpose|origin|decisions|rejected|assumptions|evidence|insights|questions|actions|requirements|constraints|stack|architecture|ux|testing|deployment|risks|history|scope|metrics|summary|next
}

// Template describes an artifact type.
type Template struct {
	Type        domain.ArtifactType `json:"type"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Audience    string              `json:"audience"`
	Sections    []Section           `json:"sections"`
}

// Templates is the registry of artifact types.
var Templates = map[domain.ArtifactType]Template{
	domain.ArtifactActionPlan: {Name: "Action Plan", Description: "Concrete, sequenced next steps derived from decisions and open questions.",
		Audience: "the idea owner, to act on immediately",
		Sections: []Section{
			{"Goal", "One paragraph: what this plan achieves and why now.", "purpose"},
			{"Decisions this plan builds on", "The active decisions the plan depends on, with labels.", "decisions"},
			{"Phases & steps", "Sequenced, checkbox steps grouped into phases. Each step concrete and verifiable. Use recorded actions first.", "actions"},
			{"Risks & assumptions to validate first", "Highest-risk unvalidated assumptions and how to test them cheaply.", "assumptions"},
			{"Open questions blocking progress", "Questions that must be answered and who/how.", "questions"},
			{"Success criteria", "How we will know the plan worked.", "metrics"},
		}},
	domain.ArtifactImplementationPrompt: {Name: "Implementation Prompt", Description: "A complete, self-contained brief to hand to a coding agent.",
		Audience: "an autonomous coding agent with no other context",
		Sections: []Section{
			{"Product purpose", "What is being built, for whom, and the core problem it solves.", "purpose"},
			{"Requirements", "Functional requirements as a numbered list, each testable.", "requirements"},
			{"Decisions (binding)", "Every active decision with its rationale; the agent must follow these.", "decisions"},
			{"Architecture", "System shape, components and data flow as decided so far.", "architecture"},
			{"Constraints", "Hard constraints (cost, platform, privacy, performance, timeline).", "constraints"},
			{"Accepted scope", "What is in scope for this implementation.", "scope"},
			{"Rejected alternatives (do not build)", "Options that were considered and rejected, with reasons.", "rejected"},
			{"Desired stack", "Languages, frameworks, infrastructure as decided.", "stack"},
			{"UX requirements", "Interaction and interface expectations.", "ux"},
			{"Implementation requirements", "Code quality, structure, security and observability expectations.", "implementation"},
			{"Testing requirements", "What must be tested and how.", "testing"},
			{"Deployment requirements", "Where and how it ships.", "deployment"},
			{"Unresolved questions", "Open questions the agent must NOT silently decide; flag or choose conservatively and document.", "questions"},
		}},
	domain.ArtifactProductBrief: {Name: "Product Brief", Description: "Problem, users, value, scope and success metrics.",
		Audience: "collaborators or stakeholders new to the idea",
		Sections: []Section{
			{"Problem", "The problem and who has it.", "purpose"},
			{"Origin", "How the idea came to exist (quote the original framing).", "origin"},
			{"Target users", "Who it is for.", "users"},
			{"Value proposition", "Why this matters to them.", "insights"},
			{"Key decisions", "Decisions that shape the product.", "decisions"},
			{"Scope", "What's in and out.", "scope"},
			{"Assumptions & risks", "What must be true.", "assumptions"},
			{"Success metrics", "How success is measured.", "metrics"},
		}},
	domain.ArtifactResearchReport: {Name: "Research Report", Description: "Evidence gathered, what it supports or challenges, and what was learned.",
		Audience: "someone evaluating the idea's evidence base",
		Sections: []Section{
			{"Research question", "What we set out to learn.", "purpose"},
			{"Evidence", "Each evidence item with stance (supports/challenges) and source.", "evidence"},
			{"Insights", "What the evidence taught us.", "insights"},
			{"Assumptions status", "Which assumptions were validated, invalidated or remain open.", "assumptions"},
			{"Open questions", "What remains unknown.", "questions"},
			{"Implications", "How findings affect decisions.", "decisions"},
		}},
	domain.ArtifactStrategy: {Name: "Strategy Document", Description: "Direction, choices, trade-offs and how to win.",
		Audience: "the idea owner and advisors",
		Sections: []Section{
			{"Situation", "Where the idea stands.", "purpose"},
			{"Strategic choices", "The key decisions and their rationale.", "decisions"},
			{"Trade-offs & rejected paths", "What we deliberately chose not to do.", "rejected"},
			{"Bets & assumptions", "What we are betting on.", "assumptions"},
			{"Evidence", "What supports the strategy.", "evidence"},
			{"Risks", "What could go wrong.", "risks"},
			{"Next moves", "Immediate actions.", "actions"},
		}},
	domain.ArtifactTechnicalSpec: {Name: "Technical Specification", Description: "Requirements, design, interfaces and constraints.",
		Audience: "engineers implementing the system",
		Sections: []Section{
			{"Overview", "What the system does.", "purpose"},
			{"Requirements", "Functional and non-functional requirements.", "requirements"},
			{"Design decisions", "Technical decisions with rationale.", "decisions"},
			{"Architecture", "Components and data flow.", "architecture"},
			{"Stack", "Technologies.", "stack"},
			{"Constraints", "Limits and guarantees.", "constraints"},
			{"Testing", "Verification approach.", "testing"},
			{"Open questions", "Unresolved technical questions.", "questions"},
		}},
	domain.ArtifactArchitecture: {Name: "Architecture Document", Description: "System structure, components, data and decisions.",
		Audience: "engineers and reviewers",
		Sections: []Section{
			{"Context", "System purpose and boundaries.", "purpose"},
			{"Architecture decisions", "ADR-style decisions: decision, rationale, alternatives.", "decisions"},
			{"Components", "Major components and responsibilities.", "architecture"},
			{"Technology", "Chosen stack.", "stack"},
			{"Quality attributes & constraints", "Performance, security, cost.", "constraints"},
			{"Risks & open questions", "Known risks and unknowns.", "questions"},
		}},
	domain.ArtifactDecisionMemo: {Name: "Decision Memo", Description: "Decisions, rationale, alternatives and how they evolved.",
		Audience: "anyone asking 'why did we decide this?'",
		Sections: []Section{
			{"Summary", "The decisions in brief.", "summary"},
			{"Decisions", "Each decision: rationale, alternatives considered, evidence.", "decisions"},
			{"How the decisions evolved", "Superseded and reversed decisions, and why they changed.", "history"},
			{"Evidence considered", "Supporting and challenging evidence.", "evidence"},
			{"Open questions", "What could still change the decision.", "questions"},
		}},
	domain.ArtifactProposal: {Name: "Proposal", Description: "A persuasive, grounded proposal.",
		Audience: "a decision-maker or partner",
		Sections: []Section{
			{"Proposal", "What is proposed.", "purpose"},
			{"Why", "Problem and evidence.", "evidence"},
			{"Approach", "Key decisions and plan.", "decisions"},
			{"What we will not do", "Explicit exclusions.", "rejected"},
			{"Risks & mitigations", "Assumptions and how they're handled.", "assumptions"},
			{"Ask & next steps", "What is needed and next actions.", "actions"},
		}},
	domain.ArtifactChecklist: {Name: "Checklist", Description: "A checkbox list of everything to do or verify.",
		Audience: "the person executing",
		Sections: []Section{
			{"Before starting", "Assumptions to validate and questions to answer.", "questions"},
			{"Tasks", "Checkbox list of actions.", "actions"},
			{"Verify", "Checks that confirm decisions were followed.", "decisions"},
		}},
	domain.ArtifactMeetingBrief: {Name: "Meeting Brief", Description: "Context, decisions needed and questions for a meeting.",
		Audience: "meeting participants",
		Sections: []Section{
			{"Context", "Background in 3-4 sentences.", "purpose"},
			{"Where we are", "Current decisions.", "decisions"},
			{"Decisions needed", "Open questions to resolve in the meeting.", "questions"},
			{"Supporting evidence", "Relevant evidence.", "evidence"},
		}},
	domain.ArtifactExecutiveSummary: {Name: "Executive Summary", Description: "A one-page summary of the idea and its state.",
		Audience: "a busy reader",
		Sections: []Section{
			{"In one paragraph", "What it is and where it stands.", "summary"},
			{"Key decisions", "The 3-5 most important decisions.", "decisions"},
			{"What we learned", "Top insights.", "insights"},
			{"Risks & open questions", "What's unresolved.", "questions"},
			{"Recommendation", "Recommended next step.", "next"},
		}},
	domain.ArtifactExperimentPlan: {Name: "Experiment Plan", Description: "Experiments to validate the riskiest assumptions.",
		Audience: "the idea owner",
		Sections: []Section{
			{"Hypotheses", "Unvalidated assumptions, riskiest first, as testable hypotheses.", "assumptions"},
			{"Experiments", "For each hypothesis: method, metric, success threshold, cost.", "experiments"},
			{"Existing evidence", "What we already know.", "evidence"},
			{"Decisions that depend on results", "Which decisions change based on outcomes.", "decisions"},
		}},
	domain.ArtifactRequirements: {Name: "Requirements Document", Description: "Functional and non-functional requirements.",
		Audience: "builders and testers",
		Sections: []Section{
			{"Purpose", "What the product does.", "purpose"},
			{"Functional requirements", "Numbered, testable requirements.", "requirements"},
			{"Non-functional requirements & constraints", "Performance, security, cost, platform.", "constraints"},
			{"Out of scope", "Explicitly excluded.", "rejected"},
			{"Open questions", "Unresolved requirements questions.", "questions"},
		}},
	domain.ArtifactReference: {Name: "Reference Document", Description: "Everything known about the idea, organized for later retrieval.",
		Audience: "future you",
		Sections: []Section{
			{"Summary", "What the idea is.", "summary"},
			{"Origin", "How it began.", "origin"},
			{"Decisions", "All decisions.", "decisions"},
			{"Insights", "What was learned.", "insights"},
			{"Evidence", "Evidence gathered.", "evidence"},
			{"Assumptions", "Assumptions and their status.", "assumptions"},
			{"Open questions", "Unresolved.", "questions"},
			{"History", "How thinking changed.", "history"},
		}},
	domain.ArtifactCustom: {Name: "Document", Description: "A custom Markdown document shaped by your instructions.",
		Audience: "as described in the instructions",
		Sections: []Section{
			{"Overview", "Overview.", "summary"},
			{"Decisions", "Relevant decisions.", "decisions"},
			{"Details", "Details per instructions.", "insights"},
			{"Open questions", "Unresolved.", "questions"},
		}},
}

func init() {
	for t, tpl := range Templates {
		tpl.Type = t
		Templates[t] = tpl
	}
}

// Get returns the template for a type.
func Get(t domain.ArtifactType) Template {
	if tpl, ok := Templates[t]; ok {
		return tpl
	}
	return Templates[domain.ArtifactCustom]
}
