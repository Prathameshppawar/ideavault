package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/models"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/repository/postgres"
)

// CreateCheckpointInput snapshots a branch.
type CreateCheckpointInput struct {
	IdeaID   uuid.UUID             `json:"idea_id"`
	BranchID *uuid.UUID            `json:"branch_id,omitempty"`
	Title    string                `json:"title,omitempty"`
	Summary  string                `json:"summary,omitempty"`
	Context  string                `json:"context,omitempty"`
	Kind     domain.CheckpointKind `json:"kind,omitempty"`
}

// CreateCheckpoint captures an immutable snapshot of a branch's knowledge state.
func (s *Service) CreateCheckpoint(ctx context.Context, a Actor, in CreateCheckpointInput) (*domain.Checkpoint, error) {
	idea, err := s.store.GetIdea(ctx, a.UserID, in.IdeaID)
	if err != nil {
		return nil, err
	}
	br, err := s.resolveBranch(ctx, a.UserID, idea, in.BranchID)
	if err != nil {
		return nil, err
	}
	kind := in.Kind
	if kind == "" {
		kind = domain.CheckpointManual
	}
	snap, err := s.buildSnapshot(ctx, s.store, a.UserID, idea, br, in.Context)
	if err != nil {
		return nil, err
	}
	summary := strings.TrimSpace(in.Summary)
	if summary == "" {
		summary = s.summarizeSnapshot(ctx, a.UserID, snap)
	}
	var cp *domain.Checkpoint
	err = s.store.WithTx(ctx, func(tx *postgres.Store) error {
		if err := tx.LockIdeaKnowledge(ctx, idea.ID); err != nil {
			return err
		}
		n, err := tx.NextCheckpointNumber(ctx, idea.ID)
		if err != nil {
			return err
		}
		// Re-read inside the lock so the snapshot reflects committed state at numbering time.
		snap, err = s.buildSnapshot(ctx, tx, a.UserID, idea, br, in.Context)
		if err != nil {
			return err
		}
		title := strings.TrimSpace(in.Title)
		if title == "" {
			title = defaultCheckpointTitle(kind, snap)
		}
		cp = &domain.Checkpoint{UserID: a.UserID, IdeaID: idea.ID, BranchID: br.ID, Number: n, Title: trimTo(title, 200), Summary: summary, Kind: kind,
			ParentCheckpointID: br.HeadCheckpointID, Snapshot: snap, ContentHash: snapshotHash(snap), CreatedBy: a.Kind, AgentRunID: a.AgentRunID, BranchName: br.Name}
		if a.Kind == domain.ActorImport {
			cp.CreatedBy = domain.ActorSystem
		}
		if err := tx.InsertCheckpoint(ctx, cp); err != nil {
			return err
		}
		cp.Counts = snap.CountByKind()
		s.activity(ctx, tx, a, &idea.ID, &br.ID, "checkpoint.created", domain.EntityCheckpoint, &cp.ID,
			fmt.Sprintf("%s on %s: %s", cp.Label, br.Name, cp.Title), map[string]any{"number": n, "kind": string(kind)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return cp, nil
}

func defaultCheckpointTitle(kind domain.CheckpointKind, snap *domain.Snapshot) string {
	switch kind {
	case domain.CheckpointConclusion:
		return "Conclusion"
	case domain.CheckpointForkBase:
		return "Fork base"
	case domain.CheckpointMerge:
		return "Merged external thinking"
	}
	live := 0
	var latest *domain.KnowledgeItem
	for i := range snap.Items {
		it := &snap.Items[i]
		if it.IsLive() {
			live++
			if it.Kind == domain.KindDecision && (latest == nil || it.CreatedAt.After(latest.CreatedAt)) {
				latest = it
			}
		}
	}
	if latest != nil {
		return "After " + latest.Label + ": " + trimTo(latest.Statement, 80)
	}
	return fmt.Sprintf("Snapshot of %d knowledge items", live)
}

// buildSnapshot assembles the full self-contained state of a branch.
func (s *Service) buildSnapshot(ctx context.Context, st *postgres.Store, userID uuid.UUID, idea *domain.Idea, br *domain.Branch, note string) (*domain.Snapshot, error) {
	items, err := st.ListKnowledge(ctx, postgres.KnowledgeFilter{UserID: userID, BranchID: &br.ID, ReviewStates: []domain.ReviewState{domain.ReviewAccepted}, Limit: 5000, OldestFirst: true})
	if err != nil {
		return nil, err
	}
	rels, err := st.ListRelationships(ctx, postgres.RelationshipFilter{UserID: userID, IdeaID: &idea.ID, BranchID: &br.ID, Limit: 5000})
	if err != nil {
		return nil, err
	}
	inSnap := map[uuid.UUID]bool{}
	for _, it := range items {
		inSnap[it.ID] = true
	}
	snap := &domain.Snapshot{SchemaVersion: 1, CapturedAt: time.Now().UTC(), Items: items, Context: strings.TrimSpace(note),
		Idea:   domain.SnapshotIdea{ID: idea.ID, Title: idea.Title, Summary: idea.Summary, OriginText: idea.OriginText, Status: idea.Status, Version: idea.Version, Tags: idea.Tags},
		Branch: domain.SnapshotBranch{ID: br.ID, Name: br.Name, ParentBranchID: br.ParentBranchID, ForkedFromCheckpointID: br.ForkedFromCheckpointID}}
	if snap.Items == nil {
		snap.Items = []domain.KnowledgeItem{}
	}
	snap.Relationships = []domain.Relationship{}
	for _, r := range rels {
		touches := (r.FromType.IsKnowledge() && inSnap[r.FromID]) || (r.ToType.IsKnowledge() && inSnap[r.ToID]) || r.FromType == domain.EntityIdea || r.ToType == domain.EntityIdea
		if !touches {
			continue
		}
		snap.Relationships = append(snap.Relationships, r)
		if r.RelType == domain.RelContradicts {
			snap.Contradictions = append(snap.Contradictions, domain.SnapshotContradiction{FromID: r.FromID, ToID: r.ToID, FromLabel: r.FromLabel, ToLabel: r.ToLabel, Rationale: r.Rationale})
		}
	}
	convs, err := st.ListConversations(ctx, postgres.ConversationFilter{UserID: userID, BranchID: &br.ID, IncludeLinked: true, Limit: 500})
	if err != nil {
		return nil, err
	}
	snap.Conversations = []domain.SnapshotRef{}
	srcSeen := map[uuid.UUID]bool{}
	snap.Sources = []domain.SnapshotRef{}
	for _, c := range convs {
		snap.Conversations = append(snap.Conversations, domain.SnapshotRef{ID: c.ID, Title: c.Title, Kind: string(c.Origin), At: c.StartedAt})
		if c.SourceID != nil && !srcSeen[*c.SourceID] {
			srcSeen[*c.SourceID] = true
			if src, err := st.GetSource(ctx, userID, *c.SourceID); err == nil {
				snap.Sources = append(snap.Sources, domain.SnapshotRef{ID: src.ID, Title: src.Title, Kind: src.Provider, At: src.CreatedAt})
			}
		}
	}
	arts, err := st.ListArtifacts(ctx, postgres.ArtifactFilter{UserID: userID, IdeaID: &idea.ID, BranchID: &br.ID})
	if err != nil {
		return nil, err
	}
	snap.Artifacts = []domain.SnapshotRef{}
	for _, a := range arts {
		snap.Artifacts = append(snap.Artifacts, domain.SnapshotRef{ID: a.ID, Title: a.Title, Kind: string(a.Type), At: a.UpdatedAt})
	}
	return snap, nil
}

func snapshotHash(s *domain.Snapshot) string {
	type entry struct {
		ID     uuid.UUID `json:"id"`
		Status string    `json:"s"`
	}
	var es []entry
	for _, it := range s.Items {
		es = append(es, entry{it.ID, it.Status})
	}
	sort.Slice(es, func(i, j int) bool { return es[i].ID.String() < es[j].ID.String() })
	b, _ := json.Marshal(struct {
		Idea  string  `json:"idea"`
		Items []entry `json:"items"`
		Rels  int     `json:"rels"`
		Convs int     `json:"convs"`
	}{s.Idea.Title + "|" + s.Idea.Summary + "|" + string(s.Idea.Status), es, len(s.Relationships), len(s.Conversations)})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// summarizeSnapshot writes a short INTERPRETATION summary of the state (LLM if available, else deterministic).
func (s *Service) summarizeSnapshot(ctx context.Context, userID uuid.UUID, snap *domain.Snapshot) string {
	det := deterministicSummary(snap)
	if s.gw == nil || !s.gw.HasRealModel(models.TaskSummary) || len(snap.Items) == 0 {
		return det
	}
	var b strings.Builder
	for _, it := range snap.Items {
		if it.IsLive() {
			fmt.Fprintf(&b, "[%s] (%s) %s\n", it.Label, it.Status, trimTo(it.Statement, 300))
		}
	}
	cctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	res, err := s.gw.Do(cctx, models.Call{Task: models.TaskSummary, UserID: &userID, IdeaID: &snap.Idea.ID, Request: models.Request{
		System:    "You summarise the current state of an idea's thinking for a checkpoint. 2-3 plain sentences. Mention the most important decisions by label (e.g. D2) and the biggest open question. Only use the items provided; never invent facts.",
		Messages:  []models.Message{{Role: models.RoleUser, Content: fmt.Sprintf("Idea: %s\nSummary: %s\n\nItems:\n%s", snap.Idea.Title, trimTo(snap.Idea.Summary, 600), trimTo(b.String(), 12000))}},
		MaxTokens: 800,
	}})
	if err != nil || strings.TrimSpace(res.Content) == "" {
		return det
	}
	return trimTo(strings.TrimSpace(res.Content), 1200)
}

func deterministicSummary(snap *domain.Snapshot) string {
	counts := map[domain.KnowledgeKind]int{}
	var decisions, questions []string
	for _, it := range snap.Items {
		if !it.IsLive() {
			continue
		}
		counts[it.Kind]++
		switch it.Kind {
		case domain.KindDecision:
			decisions = append(decisions, it.Label+" "+trimTo(it.Statement, 70))
		case domain.KindQuestion:
			questions = append(questions, it.Label+" "+trimTo(it.Statement, 70))
		}
	}
	var parts []string
	for _, k := range domain.AllKnowledgeKinds {
		if counts[k] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[k], plural(string(k), counts[k])))
		}
	}
	if len(parts) == 0 {
		return "No durable knowledge captured yet on " + snap.Branch.Name + "."
	}
	out := "State: " + strings.Join(parts, ", ") + "."
	if len(decisions) > 0 {
		out += " Latest decisions: " + strings.Join(lastN(decisions, 2), "; ") + "."
	}
	if len(questions) > 0 {
		out += " Open: " + strings.Join(lastN(questions, 1), "; ") + "."
	}
	return out
}

func plural(word string, n int) string {
	if n == 1 {
		return word
	}
	switch word {
	case "evidence":
		return "evidence items"
	}
	return word + "s"
}

func lastN(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// ResolveCheckpoint finds a checkpoint by id or by "CP4"/"4" within an idea.
func (s *Service) ResolveCheckpoint(ctx context.Context, userID, ideaID uuid.UUID, ref string) (*domain.Checkpoint, error) {
	ref = strings.TrimSpace(ref)
	if id, err := uuid.Parse(ref); err == nil {
		cp, err := s.store.GetCheckpoint(ctx, userID, id)
		if err != nil {
			return nil, err
		}
		if cp.IdeaID != ideaID {
			return nil, domain.Invalid("checkpoint", "checkpoint belongs to another idea")
		}
		return cp, nil
	}
	num := strings.TrimPrefix(strings.ToUpper(strings.ReplaceAll(ref, " ", "")), "CHECKPOINT")
	num = strings.TrimPrefix(num, "CP")
	num = strings.TrimPrefix(num, "#")
	var n int
	if _, err := fmt.Sscanf(num, "%d", &n); err != nil || n <= 0 {
		return nil, domain.Invalid("checkpoint", "checkpoint reference must look like CP4 or a UUID")
	}
	return s.store.GetCheckpointByNumber(ctx, userID, ideaID, n)
}

// CompareCheckpoints diffs two checkpoints by knowledge lineage.
func (s *Service) CompareCheckpoints(ctx context.Context, userID, fromID, toID uuid.UUID) (*domain.CheckpointDiff, error) {
	a, err := s.store.GetCheckpoint(ctx, userID, fromID)
	if err != nil {
		return nil, err
	}
	b, err := s.store.GetCheckpoint(ctx, userID, toID)
	if err != nil {
		return nil, err
	}
	if a.IdeaID != b.IdeaID {
		return nil, domain.Invalid("to", "checkpoints belong to different ideas")
	}
	return diffSnapshots(a, b), nil
}

func diffSnapshots(a, b *domain.Checkpoint) *domain.CheckpointDiff {
	d := &domain.CheckpointDiff{
		From:  domain.EntityRef{Type: domain.EntityCheckpoint, ID: a.ID, Label: a.Label, Title: a.Title},
		To:    domain.EntityRef{Type: domain.EntityCheckpoint, ID: b.ID, Label: b.Label, Title: b.Title},
		Added: []domain.KnowledgeItem{}, Removed: []domain.KnowledgeItem{}, Changed: []domain.ItemChange{}, Superseded: []domain.Supersession{},
	}
	byLineageA := map[uuid.UUID]domain.KnowledgeItem{}
	for _, it := range a.Snapshot.Items {
		byLineageA[it.LineageID] = it
	}
	byLineageB := map[uuid.UUID]domain.KnowledgeItem{}
	byIDB := map[uuid.UUID]domain.KnowledgeItem{}
	for _, it := range b.Snapshot.Items {
		byLineageB[it.LineageID] = it
		byIDB[it.ID] = it
	}
	for lin, itB := range byLineageB {
		itA, ok := byLineageA[lin]
		if !ok {
			d.Added = append(d.Added, itB)
			continue
		}
		var fields []string
		if itA.Status != itB.Status {
			fields = append(fields, "status")
		}
		if itA.Statement != itB.Statement {
			fields = append(fields, "statement")
		}
		if len(fields) > 0 {
			d.Changed = append(d.Changed, domain.ItemChange{Before: itA, After: itB, Fields: fields})
		} else {
			d.Unchanged++
		}
		if itB.SupersededByID != nil && itA.SupersededByID == nil {
			if nb, ok := byIDB[*itB.SupersededByID]; ok {
				d.Superseded = append(d.Superseded, domain.Supersession{Old: itB, New: nb})
			}
		}
	}
	for lin, itA := range byLineageA {
		if _, ok := byLineageB[lin]; !ok {
			d.Removed = append(d.Removed, itA)
		}
	}
	sortItems := func(xs []domain.KnowledgeItem) {
		sort.Slice(xs, func(i, j int) bool {
			if xs[i].Kind != xs[j].Kind {
				return xs[i].Kind < xs[j].Kind
			}
			return xs[i].RefNumber < xs[j].RefNumber
		})
	}
	sortItems(d.Added)
	sortItems(d.Removed)
	return d
}
