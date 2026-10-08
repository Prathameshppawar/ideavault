package domain

import (
	"strings"

	"github.com/google/uuid"
)

// EntityType names every node type in the thinking graph.
type EntityType string

const (
	EntityIdea         EntityType = "idea"
	EntityBranch       EntityType = "branch"
	EntityCheckpoint   EntityType = "checkpoint"
	EntityConversation EntityType = "conversation"
	EntityMessage      EntityType = "message"
	EntitySource       EntityType = "source"
	EntityDecision     EntityType = "decision"
	EntityAssumption   EntityType = "assumption"
	EntityEvidence     EntityType = "evidence"
	EntityInsight      EntityType = "insight"
	EntityQuestion     EntityType = "question"
	EntityAction       EntityType = "action"
	EntityArtifact     EntityType = "artifact"
	EntityContextPack  EntityType = "context_pack"
)

// AllEntityTypes lists every valid entity type.
var AllEntityTypes = []EntityType{
	EntityIdea, EntityBranch, EntityCheckpoint, EntityConversation, EntityMessage, EntitySource,
	EntityDecision, EntityAssumption, EntityEvidence, EntityInsight, EntityQuestion, EntityAction,
	EntityArtifact, EntityContextPack,
}

// Valid reports whether t is a known entity type.
func (t EntityType) Valid() bool {
	for _, x := range AllEntityTypes {
		if x == t {
			return true
		}
	}
	return false
}

// IsKnowledge reports whether t is one of the six knowledge kinds.
func (t EntityType) IsKnowledge() bool {
	_, ok := ParseKnowledgeKind(string(t))
	return ok
}

// EntityRef points at any graph entity. Label is a human reference such as "D3" or "CP4".
type EntityRef struct {
	Type  EntityType `json:"type"`
	ID    uuid.UUID  `json:"id"`
	Label string     `json:"label,omitempty"`
	Title string     `json:"title,omitempty"`
}

// Link renders the in-app link scheme used in chat responses: iv://<type>/<id>.
func (r EntityRef) Link() string { return "iv://" + string(r.Type) + "/" + r.ID.String() }

// Actor identifies who performed an operation.
type Actor string

const (
	ActorUser   Actor = "user"
	ActorAgent  Actor = "agent"
	ActorImport Actor = "import"
	ActorSystem Actor = "system"
)

// Origin distinguishes what the source material said from what IdeaVault inferred.
type Origin string

const (
	// OriginSource marks content stated in the source (user or imported conversation).
	OriginSource Origin = "SOURCE"
	// OriginInterpretation marks content inferred by IdeaVault's AI.
	OriginInterpretation Origin = "INTERPRETATION"
)

// Valid reports whether o is a known origin.
func (o Origin) Valid() bool { return o == OriginSource || o == OriginInterpretation }

// Slugify converts a title into a URL-safe slug.
func Slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
		if b.Len() >= 60 {
			break
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if out == "" {
		out = "untitled"
	}
	return out
}
