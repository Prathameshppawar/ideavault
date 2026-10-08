package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
)

const knowledgeSelect = `SELECT k.id, k.user_id, k.idea_id, k.branch_id, k.kind, k.ref_number, k.lineage_id, k.statement, k.details,
	k.status, k.origin, k.review_state, k.confidence, k.source_excerpt, k.source_message_id, k.source_conversation_id,
	k.source_id, k.created_by, k.agent_run_id, k.inherited_from_id, k.superseded_by_id, k.created_at, k.updated_at,
	d.rationale, d.alternatives, d.decided_at, d.reverses_id,
	a.risk, a.validation_method, a.validated_at,
	e.stance, e.url, e.strength, e.target_item_id,
	i.importance,
	q.answer, q.answered_at, q.answered_by_item_id,
	t.priority, t.due_at, t.completed_at
FROM knowledge_items k
LEFT JOIN decisions d ON d.item_id = k.id
LEFT JOIN assumptions a ON a.item_id = k.id
LEFT JOIN evidence e ON e.item_id = k.id
LEFT JOIN insights i ON i.item_id = k.id
LEFT JOIN open_questions q ON q.item_id = k.id
LEFT JOIN action_items t ON t.item_id = k.id`

func scanKnowledge(row pgx.Row) (*domain.KnowledgeItem, error) {
	var k domain.KnowledgeItem
	var (
		dRationale               *string
		dAlts                    []byte
		dDecidedAt               *time.Time
		dReverses                *uuid.UUID
		aRisk, aMethod           *string
		aValidatedAt             *time.Time
		eStance, eURL, eStrength *string
		eTarget                  *uuid.UUID
		iImportance              *string
		qAnswer                  *string
		qAnsweredAt              *time.Time
		qAnsweredBy              *uuid.UUID
		tPriority                *string
		tDue, tCompleted         *time.Time
	)
	err := row.Scan(&k.ID, &k.UserID, &k.IdeaID, &k.BranchID, &k.Kind, &k.RefNumber, &k.LineageID, &k.Statement, &k.Details,
		&k.Status, &k.Origin, &k.ReviewState, &k.Confidence, &k.SourceExcerpt, &k.SourceMessageID, &k.SourceConversationID,
		&k.SourceID, &k.CreatedBy, &k.AgentRunID, &k.InheritedFromID, &k.SupersededByID, &k.CreatedAt, &k.UpdatedAt,
		&dRationale, &dAlts, &dDecidedAt, &dReverses,
		&aRisk, &aMethod, &aValidatedAt,
		&eStance, &eURL, &eStrength, &eTarget,
		&iImportance,
		&qAnswer, &qAnsweredAt, &qAnsweredBy,
		&tPriority, &tDue, &tCompleted)
	if err != nil {
		return nil, err
	}
	k.Label = k.Kind.Label(k.RefNumber)
	switch k.Kind {
	case domain.KindDecision:
		k.Decision = &domain.DecisionAttrs{Alternatives: []domain.Alternative{}, ReversesID: dReverses}
		if dRationale != nil {
			k.Decision.Rationale = *dRationale
		}
		if len(dAlts) > 0 {
			_ = json.Unmarshal(dAlts, &k.Decision.Alternatives)
		}
		if dDecidedAt != nil {
			k.Decision.DecidedAt = *dDecidedAt
		}
	case domain.KindAssumption:
		k.Assumption = &domain.AssumptionAttrs{Risk: deref(aRisk, "MEDIUM"), ValidationMethod: deref(aMethod, ""), ValidatedAt: aValidatedAt}
	case domain.KindEvidence:
		k.Evidence = &domain.EvidenceAttrs{Stance: deref(eStance, "SUPPORTS"), URL: deref(eURL, ""), Strength: deref(eStrength, "MODERATE"), TargetItemID: eTarget}
	case domain.KindInsight:
		k.Insight = &domain.InsightAttrs{Importance: deref(iImportance, "MEDIUM")}
	case domain.KindQuestion:
		k.Question = &domain.QuestionAttrs{Answer: deref(qAnswer, ""), AnsweredAt: qAnsweredAt, AnsweredByItemID: qAnsweredBy}
	case domain.KindAction:
		k.Action = &domain.ActionAttrs{Priority: deref(tPriority, "MEDIUM"), DueAt: tDue, CompletedAt: tCompleted}
	}
	k.EnsureAttrs()
	return &k, nil
}

func deref(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}

// NextRefNumber allocates the next human ref number for a kind within an idea.
// Callers must hold the idea knowledge lock (LockIdeaKnowledge) inside a transaction.
func (s *Store) NextRefNumber(ctx context.Context, ideaID uuid.UUID, kind domain.KnowledgeKind) (int, error) {
	var n int
	err := s.q.QueryRow(ctx, `SELECT coalesce(max(ref_number), 0) + 1 FROM knowledge_items WHERE idea_id = $1 AND kind = $2`, ideaID, kind).Scan(&n)
	return n, err
}

// LockIdeaKnowledge serializes ref allocation and checkpointing for an idea (transaction-scoped).
func (s *Store) LockIdeaKnowledge(ctx context.Context, ideaID uuid.UUID) error {
	return s.lockKey(ctx, "idea-knowledge", ideaID.String())
}

// InsertKnowledge inserts a knowledge item and its type-specific row.
// If RefNumber is 0 a new ref is allocated; if LineageID is zero the item starts a new lineage.
func (s *Store) InsertKnowledge(ctx context.Context, k *domain.KnowledgeItem) error {
	return s.WithTx(ctx, func(tx *Store) error {
		if err := tx.LockIdeaKnowledge(ctx, k.IdeaID); err != nil {
			return err
		}
		if k.RefNumber == 0 {
			n, err := tx.NextRefNumber(ctx, k.IdeaID, k.Kind)
			if err != nil {
				return err
			}
			k.RefNumber = n
		}
		if k.ID == uuid.Nil {
			k.ID = uuid.New()
		}
		if k.LineageID == uuid.Nil {
			k.LineageID = k.ID
		}
		if k.Status == "" {
			k.Status = k.Kind.DefaultStatus()
		}
		if k.Origin == "" {
			k.Origin = domain.OriginSource
		}
		if k.ReviewState == "" {
			k.ReviewState = domain.ReviewAccepted
		}
		if k.CreatedBy == "" {
			k.CreatedBy = domain.ActorUser
		}
		k.EnsureAttrs()
		err := tx.q.QueryRow(ctx, `
			INSERT INTO knowledge_items (id, user_id, idea_id, branch_id, kind, ref_number, lineage_id, statement, details, status, origin,
				review_state, confidence, source_excerpt, source_message_id, source_conversation_id, source_id, created_by, agent_run_id,
				inherited_from_id, superseded_by_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
			RETURNING created_at, updated_at`,
			k.ID, k.UserID, k.IdeaID, k.BranchID, k.Kind, k.RefNumber, k.LineageID, sanitizeText(k.Statement), sanitizeText(k.Details), k.Status, k.Origin,
			k.ReviewState, k.Confidence, sanitizeText(k.SourceExcerpt), k.SourceMessageID, k.SourceConversationID, k.SourceID, k.CreatedBy, k.AgentRunID,
			k.InheritedFromID, k.SupersededByID).Scan(&k.CreatedAt, &k.UpdatedAt)
		if err != nil {
			return mapErr(err, string(k.Kind))
		}
		k.Label = k.Kind.Label(k.RefNumber)
		return tx.insertKnowledgeExt(ctx, k)
	})
}

func (s *Store) insertKnowledgeExt(ctx context.Context, k *domain.KnowledgeItem) error {
	var err error
	switch k.Kind {
	case domain.KindDecision:
		d := k.Decision
		decided := d.DecidedAt
		if decided.IsZero() {
			decided = time.Now()
			d.DecidedAt = decided
		}
		alts, _ := json.Marshal(d.Alternatives)
		_, err = s.q.Exec(ctx, `INSERT INTO decisions (item_id, rationale, alternatives, decided_at, reverses_id) VALUES ($1, $2, $3, $4, $5)`,
			k.ID, sanitizeText(d.Rationale), alts, decided, d.ReversesID)
	case domain.KindAssumption:
		a := k.Assumption
		_, err = s.q.Exec(ctx, `INSERT INTO assumptions (item_id, risk, validation_method, validated_at) VALUES ($1, $2, $3, $4)`,
			k.ID, a.Risk, a.ValidationMethod, a.ValidatedAt)
	case domain.KindEvidence:
		e := k.Evidence
		_, err = s.q.Exec(ctx, `INSERT INTO evidence (item_id, stance, url, strength, target_item_id) VALUES ($1, $2, $3, $4, $5)`,
			k.ID, e.Stance, e.URL, e.Strength, e.TargetItemID)
	case domain.KindInsight:
		_, err = s.q.Exec(ctx, `INSERT INTO insights (item_id, importance) VALUES ($1, $2)`, k.ID, k.Insight.Importance)
	case domain.KindQuestion:
		q := k.Question
		_, err = s.q.Exec(ctx, `INSERT INTO open_questions (item_id, answer, answered_at, answered_by_item_id) VALUES ($1, $2, $3, $4)`,
			k.ID, q.Answer, q.AnsweredAt, q.AnsweredByItemID)
	case domain.KindAction:
		t := k.Action
		_, err = s.q.Exec(ctx, `INSERT INTO action_items (item_id, priority, due_at, completed_at) VALUES ($1, $2, $3, $4)`,
			k.ID, t.Priority, t.DueAt, t.CompletedAt)
	default:
		return domain.Invalid("kind", "unknown knowledge kind")
	}
	return mapErr(err, string(k.Kind))
}

// GetKnowledge fetches one item owned by userID.
func (s *Store) GetKnowledge(ctx context.Context, userID, id uuid.UUID) (*domain.KnowledgeItem, error) {
	k, err := scanKnowledge(s.q.QueryRow(ctx, knowledgeSelect+` WHERE k.id = $1 AND k.user_id = $2`, id, userID))
	return k, mapErr(err, "knowledge item")
}

// FindKnowledgeByRef resolves "D3"-style refs within a branch (falls back to the idea).
func (s *Store) FindKnowledgeByRef(ctx context.Context, userID, ideaID uuid.UUID, branchID *uuid.UUID, kind domain.KnowledgeKind, ref int) (*domain.KnowledgeItem, error) {
	k, err := scanKnowledge(s.q.QueryRow(ctx, knowledgeSelect+`
		WHERE k.user_id = $1 AND k.idea_id = $2 AND k.kind = $3 AND k.ref_number = $4
		ORDER BY (k.branch_id = $5) DESC, k.created_at LIMIT 1`, userID, ideaID, kind, ref, branchID))
	return k, mapErr(err, "knowledge item")
}

// KnowledgeFilter selects knowledge items.
type KnowledgeFilter struct {
	UserID       uuid.UUID
	IdeaID       *uuid.UUID
	BranchID     *uuid.UUID
	Kinds        []domain.KnowledgeKind
	Statuses     []string
	ReviewStates []domain.ReviewState
	LiveOnly     bool // accepted and not superseded/rejected/retracted...
	IDs          []uuid.UUID
	LineageIDs   []uuid.UUID
	Origin       domain.Origin
	// SourceConversationID filters items extracted from a conversation.
	SourceConversationID *uuid.UUID
	Query                string
	Since                *time.Time
	Before               *time.Time
	Limit                int
	Offset               int
	OldestFirst          bool
}

// ListKnowledge lists knowledge items matching the filter.
func (s *Store) ListKnowledge(ctx context.Context, f KnowledgeFilter) ([]domain.KnowledgeItem, error) {
	b := &qb{}
	b.add("k.user_id = ?", f.UserID)
	if f.IdeaID != nil {
		b.add("k.idea_id = ?", *f.IdeaID)
	}
	if f.BranchID != nil {
		b.add("k.branch_id = ?", *f.BranchID)
	}
	if len(f.Kinds) > 0 {
		ks := make([]string, len(f.Kinds))
		for i, k := range f.Kinds {
			ks[i] = string(k)
		}
		b.add("k.kind = ANY(?)", ks)
	}
	if len(f.Statuses) > 0 {
		b.add("k.status = ANY(?)", f.Statuses)
	}
	if len(f.ReviewStates) > 0 {
		rs := make([]string, len(f.ReviewStates))
		for i, r := range f.ReviewStates {
			rs[i] = string(r)
		}
		b.add("k.review_state = ANY(?)", rs)
	}
	if f.LiveOnly {
		b.add("k.review_state = 'ACCEPTED'")
		b.add("k.status NOT IN ('SUPERSEDED','REVERSED','REJECTED','RETRACTED','INVALIDATED','DROPPED')")
	}
	if len(f.IDs) > 0 {
		b.add("k.id = ANY(?)", f.IDs)
	}
	if len(f.LineageIDs) > 0 {
		b.add("k.lineage_id = ANY(?)", f.LineageIDs)
	}
	if f.Origin != "" {
		b.add("k.origin = ?", f.Origin)
	}
	if f.SourceConversationID != nil {
		b.add("k.source_conversation_id = ?", *f.SourceConversationID)
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		b.add("(k.search_tsv @@ websearch_to_tsquery('english', ?) OR k.statement ILIKE ?)", q, "%"+q+"%")
	}
	if f.Since != nil {
		b.add("k.created_at >= ?", *f.Since)
	}
	if f.Before != nil {
		b.add("k.created_at <= ?", *f.Before)
	}
	order := "k.created_at DESC"
	if f.OldestFirst {
		order = "k.created_at ASC"
	}
	sql := knowledgeSelect + b.clause() + ` ORDER BY ` + order + `, k.kind, k.ref_number` +
		fmt.Sprintf(` LIMIT %d OFFSET %d`, clampLimit(f.Limit, 200, 5000), max(f.Offset, 0))
	rows, err := s.q.Query(ctx, sql, b.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.KnowledgeItem
	for rows.Next() {
		k, err := scanKnowledge(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *k)
	}
	return out, rows.Err()
}

// UpdateKnowledgeState changes status/review state/supersession (never content).
func (s *Store) UpdateKnowledgeState(ctx context.Context, userID, id uuid.UUID, status *string, review *domain.ReviewState, supersededBy *uuid.UUID) error {
	var rs *string
	if review != nil {
		r := string(*review)
		rs = &r
	}
	tag, err := s.q.Exec(ctx, `UPDATE knowledge_items SET status = coalesce($3, status), review_state = coalesce($4, review_state),
		superseded_by_id = coalesce($5, superseded_by_id) WHERE id = $1 AND user_id = $2`, id, userID, status, rs, supersededBy)
	if err != nil {
		return mapErr(err, "knowledge item")
	}
	if tag.RowsAffected() == 0 {
		return domain.NotFound("knowledge item")
	}
	return nil
}

// UpdateQuestionAnswer records the answer of an open question.
func (s *Store) UpdateQuestionAnswer(ctx context.Context, itemID uuid.UUID, answer string, answeredBy *uuid.UUID) error {
	_, err := s.q.Exec(ctx, `UPDATE open_questions SET answer = $2, answered_at = now(), answered_by_item_id = $3 WHERE item_id = $1`, itemID, answer, answeredBy)
	return err
}

// UpdateAssumptionValidation stamps validation time.
func (s *Store) UpdateAssumptionValidation(ctx context.Context, itemID uuid.UUID, validated bool) error {
	_, err := s.q.Exec(ctx, `UPDATE assumptions SET validated_at = CASE WHEN $2 THEN now() ELSE NULL END WHERE item_id = $1`, itemID, validated)
	return err
}

// UpdateActionCompletion stamps completion time.
func (s *Store) UpdateActionCompletion(ctx context.Context, itemID uuid.UUID, done bool) error {
	_, err := s.q.Exec(ctx, `UPDATE action_items SET completed_at = CASE WHEN $2 THEN now() ELSE NULL END WHERE item_id = $1`, itemID, done)
	return err
}

// SupersessionChain walks superseded_by links from an item (both directions) and returns the chain oldest-first.
func (s *Store) SupersessionChain(ctx context.Context, userID, id uuid.UUID) ([]domain.KnowledgeItem, error) {
	rows, err := s.q.Query(ctx, `
		WITH RECURSIVE back AS (
			SELECT k.id, 0 AS depth FROM knowledge_items k WHERE k.id = $1 AND k.user_id = $2
			UNION
			SELECT p.id, back.depth - 1 FROM knowledge_items p JOIN back ON p.superseded_by_id = back.id WHERE back.depth > -50
		), fwd AS (
			SELECT k.id, 0 AS depth FROM knowledge_items k WHERE k.id = $1 AND k.user_id = $2
			UNION
			SELECT n.superseded_by_id, fwd.depth + 1 FROM knowledge_items n JOIN fwd ON n.id = fwd.id WHERE n.superseded_by_id IS NOT NULL AND fwd.depth < 50
		), chain AS (SELECT id, depth FROM back UNION SELECT id, depth FROM fwd)
		`+knowledgeSelect+` JOIN chain ON chain.id = k.id ORDER BY chain.depth`, id, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.KnowledgeItem
	seen := map[uuid.UUID]bool{}
	for rows.Next() {
		k, err := scanKnowledge(rows)
		if err != nil {
			return nil, err
		}
		if !seen[k.ID] {
			seen[k.ID] = true
			out = append(out, *k)
		}
	}
	return out, rows.Err()
}

// CheckpointsContaining returns checkpoints (labels) whose snapshot included the item's lineage.
func (s *Store) CheckpointsContaining(ctx context.Context, userID, itemID uuid.UUID) ([]domain.Checkpoint, error) {
	rows, err := s.q.Query(ctx, `
		SELECT c.id, c.idea_id, c.branch_id, c.number, c.title, c.summary, c.kind, c.created_at, b.name, ci.status
		FROM checkpoint_items ci JOIN checkpoints c ON c.id = ci.checkpoint_id JOIN branches b ON b.id = c.branch_id
		WHERE ci.item_id = $1 AND c.user_id = $2 ORDER BY c.number`, itemID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Checkpoint
	for rows.Next() {
		var c domain.Checkpoint
		var status string
		if err := rows.Scan(&c.ID, &c.IdeaID, &c.BranchID, &c.Number, &c.Title, &c.Summary, &c.Kind, &c.CreatedAt, &c.BranchName, &status); err != nil {
			return nil, err
		}
		c.Label = fmt.Sprintf("CP%d", c.Number)
		c.Counts = map[string]int{}
		out = append(out, c)
	}
	return out, rows.Err()
}

// KnowledgeCounts returns counts per kind/status for dashboard views across the user's ideas.
func (s *Store) KnowledgeCounts(ctx context.Context, userID uuid.UUID) (map[string]map[string]int, error) {
	rows, err := s.q.Query(ctx, `SELECT kind, status, count(*) FROM knowledge_items WHERE user_id = $1 AND review_state = 'ACCEPTED' GROUP BY kind, status`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]int{}
	for rows.Next() {
		var kind, status string
		var n int
		if err := rows.Scan(&kind, &status, &n); err != nil {
			return nil, err
		}
		if out[kind] == nil {
			out[kind] = map[string]int{}
		}
		out[kind][status] = n
	}
	return out, rows.Err()
}
