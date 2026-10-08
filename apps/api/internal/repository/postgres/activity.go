package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
)

// RecordActivity appends an activity event and bumps the idea's last activity.
func (s *Store) RecordActivity(ctx context.Context, userID uuid.UUID, e *domain.ActivityEvent) error {
	if e.Actor == "" {
		e.Actor = domain.ActorUser
	}
	err := s.q.QueryRow(ctx, `
		INSERT INTO activity_events (user_id, idea_id, branch_id, event_type, entity_type, entity_id, summary, actor, metadata)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id, created_at`,
		userID, e.IdeaID, e.BranchID, e.EventType, string(e.EntityType), e.EntityID, truncate(e.Summary, 500), e.Actor, toJSON(e.Metadata)).
		Scan(&e.ID, &e.CreatedAt)
	if err != nil {
		return err
	}
	if e.IdeaID != nil {
		return s.TouchIdea(ctx, *e.IdeaID)
	}
	return nil
}

// ActivityFilter selects activity events.
type ActivityFilter struct {
	UserID      uuid.UUID
	IdeaID      *uuid.UUID
	BranchID    *uuid.UUID
	EventTypes  []string
	Since       *time.Time
	Limit       int
	OldestFirst bool
}

// ListActivity lists activity events with idea titles.
func (s *Store) ListActivity(ctx context.Context, f ActivityFilter) ([]domain.ActivityEvent, error) {
	b := &qb{}
	b.add("e.user_id = ?", f.UserID)
	if f.IdeaID != nil {
		b.add("e.idea_id = ?", *f.IdeaID)
	}
	if f.BranchID != nil {
		b.add("e.branch_id = ?", *f.BranchID)
	}
	if len(f.EventTypes) > 0 {
		b.add("e.event_type = ANY(?)", f.EventTypes)
	}
	if f.Since != nil {
		b.add("e.created_at >= ?", *f.Since)
	}
	order := "DESC"
	if f.OldestFirst {
		order = "ASC"
	}
	rows, err := s.q.Query(ctx, `SELECT e.id, e.idea_id, e.branch_id, e.event_type, e.entity_type, e.entity_id, e.summary, e.actor, e.metadata, e.created_at, coalesce(i.title, '')
		FROM activity_events e LEFT JOIN ideas i ON i.id = e.idea_id`+b.clause()+
		fmt.Sprintf(` ORDER BY e.created_at %s LIMIT %d`, order, clampLimit(f.Limit, 100, 5000)), b.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ActivityEvent
	for rows.Next() {
		var e domain.ActivityEvent
		var et string
		var meta []byte
		if err := rows.Scan(&e.ID, &e.IdeaID, &e.BranchID, &e.EventType, &et, &e.EntityID, &e.Summary, &e.Actor, &meta, &e.CreatedAt, &e.IdeaTitle); err != nil {
			return nil, err
		}
		e.EntityType = domain.EntityType(et)
		e.Metadata = jsonMap(meta)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ActivityBucket is a time bucket of activity counts.
type ActivityBucket struct {
	Day    time.Time      `json:"day"`
	Total  int            `json:"total"`
	ByType map[string]int `json:"by_type"`
	Ideas  int            `json:"ideas"`
}

// ActivityTimeline aggregates activity per day.
func (s *Store) ActivityTimeline(ctx context.Context, userID uuid.UUID, ideaID *uuid.UUID, since time.Time) ([]ActivityBucket, error) {
	b := &qb{}
	b.add("user_id = ?", userID)
	b.add("created_at >= ?", since)
	if ideaID != nil {
		b.add("idea_id = ?", *ideaID)
	}
	rows, err := s.q.Query(ctx, `SELECT date_trunc('day', created_at) d, event_type, count(*), count(DISTINCT idea_id)
		FROM activity_events`+b.clause()+` GROUP BY d, event_type ORDER BY d`, b.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	idx := map[time.Time]*ActivityBucket{}
	var order []time.Time
	for rows.Next() {
		var d time.Time
		var et string
		var n, ideas int
		if err := rows.Scan(&d, &et, &n, &ideas); err != nil {
			return nil, err
		}
		bk := idx[d]
		if bk == nil {
			bk = &ActivityBucket{Day: d, ByType: map[string]int{}}
			idx[d] = bk
			order = append(order, d)
		}
		bk.Total += n
		bk.ByType[et] += n
		if ideas > bk.Ideas {
			bk.Ideas = ideas
		}
	}
	out := make([]ActivityBucket, 0, len(order))
	for _, d := range order {
		out = append(out, *idx[d])
	}
	return out, rows.Err()
}

// IdeaMomentum is an idea's recent change velocity.
type IdeaMomentum struct {
	IdeaID    uuid.UUID         `json:"idea_id"`
	Title     string            `json:"title"`
	Status    domain.IdeaStatus `json:"status"`
	Recent    int               `json:"recent"`   // events in window
	Previous  int               `json:"previous"` // events in the preceding window
	LastAt    time.Time         `json:"last_at"`
	Decisions int               `json:"decisions"`
	Reversals int               `json:"reversals"`
	Branches  int               `json:"branches"`
}

// Momentum ranks ideas by activity in the last `window` compared to the preceding window.
func (s *Store) Momentum(ctx context.Context, userID uuid.UUID, window time.Duration, limit int) ([]IdeaMomentum, error) {
	now := time.Now()
	rows, err := s.q.Query(ctx, `
		SELECT i.id, i.title, i.status,
			count(e.id) FILTER (WHERE e.created_at >= $2),
			count(e.id) FILTER (WHERE e.created_at < $2 AND e.created_at >= $3),
			i.last_activity_at,
			count(e.id) FILTER (WHERE e.created_at >= $2 AND e.event_type = 'decision.recorded'),
			count(e.id) FILTER (WHERE e.created_at >= $2 AND e.event_type IN ('decision.superseded','decision.reversed')),
			count(e.id) FILTER (WHERE e.created_at >= $2 AND e.event_type = 'branch.created')
		FROM ideas i LEFT JOIN activity_events e ON e.idea_id = i.id AND e.created_at >= $3
		WHERE i.user_id = $1
		GROUP BY i.id
		HAVING count(e.id) FILTER (WHERE e.created_at >= $2) > 0
		ORDER BY count(e.id) FILTER (WHERE e.created_at >= $2) DESC, i.last_activity_at DESC
		LIMIT $4`, userID, now.Add(-window), now.Add(-2*window), clampLimit(limit, 20, 200))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IdeaMomentum
	for rows.Next() {
		var m IdeaMomentum
		if err := rows.Scan(&m.IdeaID, &m.Title, &m.Status, &m.Recent, &m.Previous, &m.LastAt, &m.Decisions, &m.Reversals, &m.Branches); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
