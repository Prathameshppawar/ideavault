package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/models"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/repository/postgres"
)

// Selection picks later/other knowledge to bring into a fork or merge.
//   - ItemIDs: specific knowledge items (their current state, or their state at CheckpointID if given).
//   - CheckpointID + Kinds: every item of those kinds in that checkpoint (e.g. "research from CP7" = evidence+insight).
//   - ConversationIDs / ArtifactIDs / SourceIDs: linked, not copied.
type Selection struct {
	ItemIDs         []uuid.UUID            `json:"item_ids,omitempty"`
	CheckpointID    *uuid.UUID             `json:"checkpoint_id,omitempty"`
	Kinds           []domain.KnowledgeKind `json:"kinds,omitempty"`
	ConversationIDs []uuid.UUID            `json:"conversation_ids,omitempty"`
	ArtifactIDs     []uuid.UUID            `json:"artifact_ids,omitempty"`
	SourceIDs       []uuid.UUID            `json:"source_ids,omitempty"`
}

// ForkInput creates a branch from a checkpoint, optionally with selected later knowledge.
type ForkInput struct {
	CheckpointID uuid.UUID   `json:"checkpoint_id"`
	Name         string      `json:"name"`
	Description  string      `json:"description,omitempty"`
	Selections   []Selection `json:"selections,omitempty"`
}

// ForkResult reports exactly what a fork inherited.
type ForkResult struct {
	Branch          *domain.Branch             `json:"branch"`
	FromCheckpoint  *domain.Checkpoint         `json:"from_checkpoint"`
	BaseCheckpoint  *domain.Checkpoint         `json:"base_checkpoint"`
	Inherited       []domain.InheritanceRecord `json:"inherited"`
	FromCheckpointN int                        `json:"from_checkpoint_items"`
	SelectedN       int                        `json:"selected_items"`
	LinkedN         int                        `json:"linked_entities"`
}

// ForkFromCheckpoint creates a new branch whose starting state is the checkpoint's snapshot
// (as it was at that moment), plus any explicitly selected knowledge. The source branch is untouched.
func (s *Service) ForkFromCheckpoint(ctx context.Context, a Actor, in ForkInput) (*ForkResult, error) {
	cp, err := s.store.GetCheckpoint(ctx, a.UserID, in.CheckpointID)
	if err != nil {
		return nil, err
	}
	idea, err := s.store.GetIdea(ctx, a.UserID, cp.IdeaID)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "Fork of " + cp.Label
	}
	mode := domain.ForkCheckpoint
	if len(in.Selections) > 0 {
		mode = domain.ForkSelective
	}
	res := &ForkResult{FromCheckpoint: cp}
	err = s.store.WithTx(ctx, func(tx *postgres.Store) error {
		br := &domain.Branch{UserID: a.UserID, IdeaID: idea.ID, Name: trimTo(name, 200), Description: strings.TrimSpace(in.Description),
			ParentBranchID: &cp.BranchID, ForkedFromCheckpointID: &cp.ID, ForkMode: &mode}
		if err := tx.CreateBranch(ctx, br); err != nil {
			return err
		}
		copier := newItemCopier(tx, a, idea.ID, br.ID)
		// 1) Everything in the checkpoint, exactly as it was then.
		for _, it := range cp.Snapshot.Items {
			if err := copier.copy(ctx, it, "checkpoint", &cp.ID, &cp.BranchID); err != nil {
				return err
			}
		}
		res.FromCheckpointN = len(cp.Snapshot.Items)
		// Conversations/sources/artifacts captured by the checkpoint are linked (not copied).
		for _, c := range cp.Snapshot.Conversations {
			if err := s.linkInherited(ctx, tx, br.ID, domain.EntityConversation, c.ID, "checkpoint", &cp.ID, &cp.BranchID); err != nil {
				return err
			}
			res.LinkedN++
		}
		for _, src := range cp.Snapshot.Sources {
			if err := s.linkInherited(ctx, tx, br.ID, domain.EntitySource, src.ID, "checkpoint", &cp.ID, &cp.BranchID); err != nil {
				return err
			}
			res.LinkedN++
		}
		for _, art := range cp.Snapshot.Artifacts {
			if err := s.linkInherited(ctx, tx, br.ID, domain.EntityArtifact, art.ID, "checkpoint", &cp.ID, &cp.BranchID); err != nil {
				return err
			}
			res.LinkedN++
		}
		// 2) Selected future/other knowledge — only what was asked for.
		n, linked, err := s.applySelections(ctx, tx, a, idea.ID, br.ID, copier, in.Selections, "selection")
		if err != nil {
			return err
		}
		res.SelectedN, res.LinkedN = n, res.LinkedN+linked
		// 3) Rewire relationships among copied items, and supersession links.
		if err := copier.finish(ctx, cp.Snapshot.Relationships); err != nil {
			return err
		}
		res.Branch = br
		summary := fmt.Sprintf("Forked %q from %s (%s)", br.Name, cp.Label, cp.BranchName)
		if res.SelectedN > 0 {
			summary += fmt.Sprintf(" + %d selected items", res.SelectedN)
		}
		s.activity(ctx, tx, a, &idea.ID, &br.ID, "branch.created", domain.EntityBranch, &br.ID, summary,
			map[string]any{"from_checkpoint": cp.Label, "mode": string(mode), "selected": res.SelectedN})
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Base checkpoint on the new branch records its starting state immutably.
	note := fmt.Sprintf("Forked from %s on %s.", cp.Label, cp.BranchName)
	if res.SelectedN > 0 {
		note += fmt.Sprintf(" Brought %d selected item(s) from later thinking.", res.SelectedN)
	}
	base, err := s.CreateCheckpoint(ctx, a, CreateCheckpointInput{IdeaID: idea.ID, BranchID: &res.Branch.ID, Kind: domain.CheckpointForkBase,
		Title: "Fork base (from " + cp.Label + ")", Context: note, Summary: note})
	if err != nil {
		return nil, err
	}
	res.BaseCheckpoint = base
	if res.Inherited, err = s.store.ListInheritance(ctx, a.UserID, res.Branch.ID); err != nil {
		return nil, err
	}
	res.Branch, err = s.store.GetBranch(ctx, a.UserID, res.Branch.ID)
	return res, err
}

func (s *Service) linkInherited(ctx context.Context, tx *postgres.Store, branchID uuid.UUID, t domain.EntityType, id uuid.UUID, via string, cpID, srcBranch *uuid.UUID) error {
	if err := tx.LinkToBranch(ctx, branchID, t, id, "inherited", cpID); err != nil {
		return err
	}
	return tx.InsertInheritance(ctx, &domain.InheritanceRecord{BranchID: branchID, EntityType: t, SourceEntityID: id, Via: via, SourceCheckpointID: cpID, SourceBranchID: srcBranch})
}

// applySelections copies/links selected entities into a branch, skipping lineages already present.
func (s *Service) applySelections(ctx context.Context, tx *postgres.Store, a Actor, ideaID, branchID uuid.UUID, copier *itemCopier, sels []Selection, via string) (copied, linked int, err error) {
	for _, sel := range sels {
		var srcCP *domain.Checkpoint
		if sel.CheckpointID != nil {
			srcCP, err = tx.GetCheckpoint(ctx, a.UserID, *sel.CheckpointID)
			if err != nil {
				return 0, 0, err
			}
			if srcCP.IdeaID != ideaID {
				return 0, 0, domain.Invalid("selections", "selected checkpoint belongs to another idea")
			}
		}
		// Items by id: from the checkpoint snapshot if given, otherwise current state.
		for _, id := range sel.ItemIDs {
			var it *domain.KnowledgeItem
			if srcCP != nil {
				for i := range srcCP.Snapshot.Items {
					if srcCP.Snapshot.Items[i].ID == id {
						it = &srcCP.Snapshot.Items[i]
					}
				}
			}
			if it == nil {
				it, err = tx.GetKnowledge(ctx, a.UserID, id)
				if err != nil {
					return 0, 0, err
				}
			}
			if it.IdeaID != ideaID {
				return 0, 0, domain.Invalid("selections", "selected item belongs to another idea")
			}
			var cpID *uuid.UUID
			if srcCP != nil {
				cpID = &srcCP.ID
			}
			ok, err := copier.copyIfAbsent(ctx, *it, via, cpID, &it.BranchID)
			if err != nil {
				return 0, 0, err
			}
			if ok {
				copied++
			}
		}
		// Kinds from a checkpoint (e.g. all evidence + insights at CP7).
		if srcCP != nil && len(sel.Kinds) > 0 && len(sel.ItemIDs) == 0 {
			want := map[domain.KnowledgeKind]bool{}
			for _, k := range sel.Kinds {
				if kk, ok := domain.ParseKnowledgeKind(string(k)); ok {
					want[kk] = true
				}
			}
			for _, it := range srcCP.Snapshot.Items {
				if !want[it.Kind] || !it.IsLive() {
					continue
				}
				ok, err := copier.copyIfAbsent(ctx, it, via, &srcCP.ID, &srcCP.BranchID)
				if err != nil {
					return 0, 0, err
				}
				if ok {
					copied++
				}
			}
		}
		for _, id := range sel.ConversationIDs {
			if ok, _ := tx.EntityOwned(ctx, a.UserID, domain.EntityConversation, id); !ok {
				return 0, 0, domain.NotFound("conversation")
			}
			if err := s.linkInherited(ctx, tx, branchID, domain.EntityConversation, id, via, sel.CheckpointID, nil); err != nil {
				return 0, 0, err
			}
			linked++
		}
		for _, id := range sel.ArtifactIDs {
			if ok, _ := tx.EntityOwned(ctx, a.UserID, domain.EntityArtifact, id); !ok {
				return 0, 0, domain.NotFound("artifact")
			}
			if err := s.linkInherited(ctx, tx, branchID, domain.EntityArtifact, id, via, sel.CheckpointID, nil); err != nil {
				return 0, 0, err
			}
			linked++
		}
		for _, id := range sel.SourceIDs {
			if ok, _ := tx.EntityOwned(ctx, a.UserID, domain.EntitySource, id); !ok {
				return 0, 0, domain.NotFound("source")
			}
			if err := s.linkInherited(ctx, tx, branchID, domain.EntitySource, id, via, sel.CheckpointID, nil); err != nil {
				return 0, 0, err
			}
			linked++
		}
	}
	return copied, linked, nil
}

// itemCopier copies knowledge items into a branch while preserving lineage and provenance.
type itemCopier struct {
	tx       *postgres.Store
	a        Actor
	ideaID   uuid.UUID
	branchID uuid.UUID
	mapping  map[uuid.UUID]uuid.UUID // original id → copy id
	lineages map[uuid.UUID]bool
	pending  []domain.KnowledgeItem // originals (for supersession rewiring)
	loaded   bool
}

func newItemCopier(tx *postgres.Store, a Actor, ideaID, branchID uuid.UUID) *itemCopier {
	return &itemCopier{tx: tx, a: a, ideaID: ideaID, branchID: branchID, mapping: map[uuid.UUID]uuid.UUID{}, lineages: map[uuid.UUID]bool{}}
}

func (c *itemCopier) loadExisting(ctx context.Context) error {
	if c.loaded {
		return nil
	}
	c.loaded = true
	items, err := c.tx.ListKnowledge(ctx, postgres.KnowledgeFilter{UserID: c.a.UserID, BranchID: &c.branchID, Limit: 5000})
	if err != nil {
		return err
	}
	for _, it := range items {
		if it.IsLive() {
			c.lineages[it.LineageID] = true
		}
	}
	return nil
}

func (c *itemCopier) copyIfAbsent(ctx context.Context, it domain.KnowledgeItem, via string, cpID, srcBranch *uuid.UUID) (bool, error) {
	if err := c.loadExisting(ctx); err != nil {
		return false, err
	}
	if c.lineages[it.LineageID] {
		return false, nil
	}
	return true, c.copy(ctx, it, via, cpID, srcBranch)
}

func (c *itemCopier) copy(ctx context.Context, it domain.KnowledgeItem, via string, cpID, srcBranch *uuid.UUID) error {
	if _, done := c.mapping[it.ID]; done {
		return nil
	}
	orig := it.ID
	cp := it
	cp.ID = uuid.Nil
	cp.UserID = c.a.UserID
	cp.BranchID = c.branchID
	cp.InheritedFromID = &orig
	cp.SupersededByID = nil
	cp.CreatedBy = c.a.Kind
	cp.AgentRunID = c.a.AgentRunID
	cp.ReviewState = domain.ReviewAccepted
	cp.EnsureAttrs()
	// Intra-branch references are remapped in finish(); clear them for now.
	if cp.Decision != nil {
		d := *cp.Decision
		d.ReversesID = nil
		cp.Decision = &d
	}
	if cp.Evidence != nil {
		e := *cp.Evidence
		e.TargetItemID = nil
		cp.Evidence = &e
	}
	if cp.Question != nil {
		q := *cp.Question
		q.AnsweredByItemID = nil
		cp.Question = &q
	}
	if err := c.tx.InsertKnowledge(ctx, &cp); err != nil {
		return err
	}
	c.mapping[orig] = cp.ID
	c.lineages[cp.LineageID] = true
	c.pending = append(c.pending, it)
	if err := c.tx.InsertInheritance(ctx, &domain.InheritanceRecord{BranchID: c.branchID, EntityType: it.Kind.EntityType(), SourceEntityID: orig,
		NewEntityID: &cp.ID, Via: via, SourceCheckpointID: cpID, SourceBranchID: srcBranch}); err != nil {
		return err
	}
	return c.tx.CreateRelationship(ctx, c.a.UserID, &domain.Relationship{FromType: it.Kind.EntityType(), FromID: cp.ID, ToType: it.Kind.EntityType(), ToID: orig,
		RelType: domain.RelInheritedFrom, IdeaID: &c.ideaID, BranchID: &c.branchID, Origin: domain.OriginSource, CreatedBy: domain.ActorSystem, AgentRunID: c.a.AgentRunID,
		Rationale: "inherited via " + via})
}

// finish rewires supersession and recreates relationships among copied items.
func (c *itemCopier) finish(ctx context.Context, rels []domain.Relationship) error {
	for _, orig := range c.pending {
		if orig.SupersededByID == nil {
			continue
		}
		if newer, ok := c.mapping[*orig.SupersededByID]; ok {
			if err := c.tx.UpdateKnowledgeState(ctx, c.a.UserID, c.mapping[orig.ID], nil, nil, &newer); err != nil {
				return err
			}
		}
	}
	for _, r := range rels {
		if r.RelType == domain.RelInheritedFrom {
			continue
		}
		from, okF := c.mapping[r.FromID]
		to, okT := c.mapping[r.ToID]
		if !okF && !(r.FromType == domain.EntityIdea) {
			continue
		}
		if !okT && !(r.ToType == domain.EntityIdea) {
			continue
		}
		if !okF {
			from = r.FromID
		}
		if !okT {
			to = r.ToID
		}
		nr := r
		nr.ID = uuid.Nil
		nr.FromID, nr.ToID = from, to
		nr.BranchID = &c.branchID
		nr.IdeaID = &c.ideaID
		if err := c.tx.CreateRelationship(ctx, c.a.UserID, &nr); err != nil {
			return err
		}
	}
	return nil
}

// CreateBranchInput creates a branch from the current head of another branch.
type CreateBranchInput struct {
	IdeaID       uuid.UUID  `json:"idea_id"`
	Name         string     `json:"name"`
	Description  string     `json:"description,omitempty"`
	FromBranchID *uuid.UUID `json:"from_branch_id,omitempty"`
	// FromCheckpointID forks from a specific checkpoint instead of the branch head.
	FromCheckpointID *uuid.UUID  `json:"from_checkpoint_id,omitempty"`
	Selections       []Selection `json:"selections,omitempty"`
}

// CreateBranch branches from a checkpoint, or snapshots the source branch first so provenance is always a checkpoint.
func (s *Service) CreateBranch(ctx context.Context, a Actor, in CreateBranchInput) (*ForkResult, error) {
	if in.FromCheckpointID != nil {
		return s.ForkFromCheckpoint(ctx, a, ForkInput{CheckpointID: *in.FromCheckpointID, Name: in.Name, Description: in.Description, Selections: in.Selections})
	}
	idea, err := s.store.GetIdea(ctx, a.UserID, in.IdeaID)
	if err != nil {
		return nil, err
	}
	src, err := s.resolveBranch(ctx, a.UserID, idea, in.FromBranchID)
	if err != nil {
		return nil, err
	}
	cp, err := s.CreateCheckpoint(ctx, a, CreateCheckpointInput{IdeaID: idea.ID, BranchID: &src.ID, Kind: domain.CheckpointAuto,
		Title: "Before branching: " + trimTo(in.Name, 80), Context: "Automatic checkpoint taken so the new branch has an exact starting point."})
	if err != nil {
		return nil, err
	}
	return s.ForkFromCheckpoint(ctx, a, ForkInput{CheckpointID: cp.ID, Name: in.Name, Description: in.Description, Selections: in.Selections})
}

// MergeSelectedContext brings selected knowledge from other branches/checkpoints into a branch.
func (s *Service) MergeSelectedContext(ctx context.Context, a Actor, targetBranchID uuid.UUID, sels []Selection, note string) (*ForkResult, error) {
	br, err := s.store.GetBranch(ctx, a.UserID, targetBranchID)
	if err != nil {
		return nil, err
	}
	if len(sels) == 0 {
		return nil, domain.Invalid("selections", "select at least one item, checkpoint kind set, conversation or artifact")
	}
	res := &ForkResult{Branch: br}
	err = s.store.WithTx(ctx, func(tx *postgres.Store) error {
		copier := newItemCopier(tx, a, br.IdeaID, br.ID)
		n, linked, err := s.applySelections(ctx, tx, a, br.IdeaID, br.ID, copier, sels, "merge")
		if err != nil {
			return err
		}
		if err := copier.finish(ctx, nil); err != nil {
			return err
		}
		res.SelectedN, res.LinkedN = n, linked
		s.activity(ctx, tx, a, &br.IdeaID, &br.ID, "branch.merged_context", domain.EntityBranch, &br.ID,
			fmt.Sprintf("Merged %d item(s) and %d linked entities into %s", n, linked, br.Name), nil)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if res.SelectedN+res.LinkedN > 0 {
		title := "Merged selected context"
		if note != "" {
			title = trimTo(note, 120)
		}
		res.BaseCheckpoint, err = s.CreateCheckpoint(ctx, a, CreateCheckpointInput{IdeaID: br.IdeaID, BranchID: &br.ID, Kind: domain.CheckpointMerge, Title: title})
		if err != nil {
			return nil, err
		}
	}
	res.Inherited, err = s.store.ListInheritance(ctx, a.UserID, br.ID)
	return res, err
}

// UpdateBranchInput renames or changes status of a branch.
type UpdateBranchInput struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Status      *string `json:"status,omitempty"`
	MakeDefault bool    `json:"make_default,omitempty"`
}

// UpdateBranch applies branch metadata/status changes.
func (s *Service) UpdateBranch(ctx context.Context, a Actor, id uuid.UUID, in UpdateBranchInput) (*domain.Branch, error) {
	err := s.store.WithTx(ctx, func(tx *postgres.Store) error {
		br, err := tx.GetBranch(ctx, a.UserID, id)
		if err != nil {
			return err
		}
		prev := br.Status
		if in.Name != nil && strings.TrimSpace(*in.Name) != "" {
			br.Name = trimTo(*in.Name, 200)
		}
		if in.Description != nil {
			br.Description = strings.TrimSpace(*in.Description)
		}
		if in.Status != nil {
			st := domain.BranchStatus(strings.ToUpper(*in.Status))
			if !st.Valid() {
				return domain.Invalid("status", "branch status must be ACTIVE, CONCLUDED, ARCHIVED or MERGED")
			}
			br.Status = st
		}
		if err := tx.UpdateBranch(ctx, br); err != nil {
			return err
		}
		if in.MakeDefault {
			if err := tx.SetDefaultBranch(ctx, a.UserID, br.IdeaID, br.ID); err != nil {
				return err
			}
		}
		summary := "Branch updated: " + br.Name
		if prev != br.Status {
			summary = fmt.Sprintf("Branch %s: %s → %s", br.Name, prev, br.Status)
		}
		s.activity(ctx, tx, a, &br.IdeaID, &br.ID, "branch.updated", domain.EntityBranch, &br.ID, summary, nil)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.store.GetBranch(ctx, a.UserID, id)
}

// BranchComparison compares the live knowledge of two branches by lineage.
type BranchComparison struct {
	A              *domain.Branch         `json:"a"`
	B              *domain.Branch         `json:"b"`
	OnlyInA        []domain.KnowledgeItem `json:"only_in_a"`
	OnlyInB        []domain.KnowledgeItem `json:"only_in_b"`
	Diverged       []domain.ItemChange    `json:"diverged"`
	Shared         int                    `json:"shared"`
	CommonAncestor *domain.EntityRef      `json:"common_ancestor,omitempty"`
	Narrative      string                 `json:"narrative"`
	Analyzer       string                 `json:"analyzer"`
}

// CompareBranches diffs two branches and (when a model is available) explains the difference.
func (s *Service) CompareBranches(ctx context.Context, userID, aID, bID uuid.UUID, explain bool) (*BranchComparison, error) {
	ba, err := s.store.GetBranch(ctx, userID, aID)
	if err != nil {
		return nil, err
	}
	bb, err := s.store.GetBranch(ctx, userID, bID)
	if err != nil {
		return nil, err
	}
	if ba.IdeaID != bb.IdeaID {
		return nil, domain.Invalid("b", "branches belong to different ideas")
	}
	ia, err := s.store.ListKnowledge(ctx, postgres.KnowledgeFilter{UserID: userID, BranchID: &aID, LiveOnly: true, Limit: 5000})
	if err != nil {
		return nil, err
	}
	ib, err := s.store.ListKnowledge(ctx, postgres.KnowledgeFilter{UserID: userID, BranchID: &bID, LiveOnly: true, Limit: 5000})
	if err != nil {
		return nil, err
	}
	cmp := &BranchComparison{A: ba, B: bb, OnlyInA: []domain.KnowledgeItem{}, OnlyInB: []domain.KnowledgeItem{}, Diverged: []domain.ItemChange{}, Analyzer: "deterministic"}
	la := map[uuid.UUID]domain.KnowledgeItem{}
	for _, it := range ia {
		la[it.LineageID] = it
	}
	lb := map[uuid.UUID]domain.KnowledgeItem{}
	for _, it := range ib {
		lb[it.LineageID] = it
	}
	for lin, it := range la {
		other, ok := lb[lin]
		switch {
		case !ok:
			cmp.OnlyInA = append(cmp.OnlyInA, it)
		case other.Status != it.Status:
			cmp.Diverged = append(cmp.Diverged, domain.ItemChange{Before: it, After: other, Fields: []string{"status"}})
		default:
			cmp.Shared++
		}
	}
	for lin, it := range lb {
		if _, ok := la[lin]; !ok {
			cmp.OnlyInB = append(cmp.OnlyInB, it)
		}
	}
	byLabel := func(xs []domain.KnowledgeItem) {
		sort.Slice(xs, func(i, j int) bool { return xs[i].Label < xs[j].Label })
	}
	byLabel(cmp.OnlyInA)
	byLabel(cmp.OnlyInB)
	if cpID := divergencePoint(ba, bb); cpID != nil {
		if cp, err := s.store.GetCheckpoint(ctx, userID, *cpID); err == nil {
			cmp.CommonAncestor = &domain.EntityRef{Type: domain.EntityCheckpoint, ID: cp.ID, Label: cp.Label, Title: cp.Title}
		}
	}
	cmp.Narrative = deterministicBranchNarrative(cmp)
	if explain && s.gw != nil && s.gw.HasRealModel(models.TaskBranchComparison) {
		var b strings.Builder
		fmt.Fprintf(&b, "Branch A: %s\nBranch B: %s\n\nOnly in A:\n", ba.Name, bb.Name)
		for _, it := range cmp.OnlyInA {
			fmt.Fprintf(&b, "- [%s] %s\n", it.Label, trimTo(it.Statement, 240))
		}
		b.WriteString("\nOnly in B:\n")
		for _, it := range cmp.OnlyInB {
			fmt.Fprintf(&b, "- [%s] %s\n", it.Label, trimTo(it.Statement, 240))
		}
		b.WriteString("\nSame item, different status:\n")
		for _, d := range cmp.Diverged {
			fmt.Fprintf(&b, "- [%s] %s: A=%s B=%s\n", d.Before.Label, trimTo(d.Before.Statement, 200), d.Before.Status, d.After.Status)
		}
		cctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		res, err := s.gw.Do(cctx, models.Call{Task: models.TaskBranchComparison, UserID: &userID, IdeaID: &ba.IdeaID, Request: models.Request{
			System:    "Explain how two directions of thinking differ, grounded ONLY in the listed items. Cite item labels like [D3]. 1 short paragraph + up to 4 bullets on the key trade-offs. Do not invent items.",
			Messages:  []models.Message{{Role: models.RoleUser, Content: trimTo(b.String(), 20000)}},
			MaxTokens: 1500,
		}})
		if err == nil && strings.TrimSpace(res.Content) != "" {
			cmp.Narrative = strings.TrimSpace(res.Content)
			cmp.Analyzer = "llm:" + res.ModelKey
		}
	}
	return cmp, nil
}

// divergencePoint returns the checkpoint where two branches of an idea split: the
// fork checkpoint of whichever branch was forked from the other (in either argument
// order) or, for sibling forks of the same parent, the earlier fork checkpoint.
func divergencePoint(a, b *domain.Branch) *uuid.UUID {
	switch {
	case b.ForkedFromCheckpointID != nil && b.ParentBranchID != nil && *b.ParentBranchID == a.ID:
		return b.ForkedFromCheckpointID
	case a.ForkedFromCheckpointID != nil && a.ParentBranchID != nil && *a.ParentBranchID == b.ID:
		return a.ForkedFromCheckpointID
	case a.ForkedFromCheckpointID != nil && b.ForkedFromCheckpointID != nil && a.ParentBranchID != nil && b.ParentBranchID != nil && *a.ParentBranchID == *b.ParentBranchID:
		if a.ForkedFromCheckpointNumber != nil && b.ForkedFromCheckpointNumber != nil && *b.ForkedFromCheckpointNumber < *a.ForkedFromCheckpointNumber {
			return b.ForkedFromCheckpointID
		}
		return a.ForkedFromCheckpointID
	}
	return nil
}

func deterministicBranchNarrative(c *BranchComparison) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("%s and %s share %d item(s).", c.A.Name, c.B.Name, c.Shared))
	if len(c.OnlyInA) > 0 {
		parts = append(parts, fmt.Sprintf("%d item(s) exist only in %s.", len(c.OnlyInA), c.A.Name))
	}
	if len(c.OnlyInB) > 0 {
		parts = append(parts, fmt.Sprintf("%d item(s) exist only in %s.", len(c.OnlyInB), c.B.Name))
	}
	if len(c.Diverged) > 0 {
		parts = append(parts, fmt.Sprintf("%d shared item(s) have a different status.", len(c.Diverged)))
	}
	if c.CommonAncestor != nil {
		parts = append(parts, "They diverged at "+c.CommonAncestor.Label+".")
	}
	return strings.Join(parts, " ")
}

// BranchTree is the data for the visual branch tree.
type BranchTree struct {
	Branches    []domain.Branch     `json:"branches"`
	Checkpoints []domain.Checkpoint `json:"checkpoints"`
}

// GetBranchTree returns branches and checkpoints for an idea.
func (s *Service) GetBranchTree(ctx context.Context, userID, ideaID uuid.UUID) (*BranchTree, error) {
	if _, err := s.store.GetIdea(ctx, userID, ideaID); err != nil {
		return nil, err
	}
	brs, err := s.store.ListBranches(ctx, userID, ideaID)
	if err != nil {
		return nil, err
	}
	cps, err := s.store.ListCheckpoints(ctx, userID, ideaID, nil)
	if err != nil {
		return nil, err
	}
	return &BranchTree{Branches: brs, Checkpoints: cps}, nil
}
