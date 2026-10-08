// Package service implements IdeaVault's business logic. Both the HTTP API and the
// agent's tools call these methods, so the chatbot and the UI share one set of rules.
package service

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/config"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/connectors"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/contextengine"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/models"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/repository/postgres"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/security"
)

// Actor identifies who is performing a write and on whose behalf.
type Actor struct {
	UserID     uuid.UUID
	Kind       domain.Actor
	AgentRunID *uuid.UUID
}

// UserActor returns an actor for a direct user action.
func UserActor(userID uuid.UUID) Actor { return Actor{UserID: userID, Kind: domain.ActorUser} }

// AgentActor returns an actor for an agent-run action.
func AgentActor(userID, runID uuid.UUID) Actor {
	return Actor{UserID: userID, Kind: domain.ActorAgent, AgentRunID: &runID}
}

// Cache is an optional key/value cache (Redis or in-memory).
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, bool)
	Set(ctx context.Context, key string, val []byte, ttl time.Duration)
	Delete(ctx context.Context, keys ...string)
}

// Service is the business-logic facade.
type Service struct {
	store   *postgres.Store
	gw      *models.Gateway
	log     *slog.Logger
	cfg     *config.Config
	sealer  *security.Sealer
	fetcher imports.Fetcher
	cache   Cache
	parser  ImportParser
	engine  *contextengine.Engine
	conns   *connectors.Registry
	factory models.ProviderFactory
}

// ImportParser parses import inputs (implemented by imports/adapters.Registry).
type ImportParser interface {
	Parse(ctx context.Context, in imports.Input, adapterName string) (*imports.ParseResult, error)
	ParseURL(ctx context.Context, fetcher imports.Fetcher, rawURL string) (*imports.ParseResult, error)
}

// Deps bundles Service dependencies.
type Deps struct {
	Store   *postgres.Store
	Gateway *models.Gateway
	Log     *slog.Logger
	Config  *config.Config
	Sealer  *security.Sealer
	Fetcher imports.Fetcher
	Cache   Cache
	Parser  ImportParser
	// Connectors is the connector registry (optional; defaults are built if nil).
	Connectors *connectors.Registry
	// ProviderFactory builds providers when keys are set from the UI.
	ProviderFactory models.ProviderFactory
}

// New constructs the Service.
func New(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Service{store: d.Store, gw: d.Gateway, log: d.Log, cfg: d.Config, sealer: d.Sealer, fetcher: d.Fetcher, cache: d.Cache, parser: d.Parser,
		engine: contextengine.New(d.Store, d.Gateway), conns: d.Connectors, factory: d.ProviderFactory}
}

// Connectors exposes the connector registry.
func (s *Service) Connectors() *connectors.Registry { return s.conns }

// Context exposes the context engine.
func (s *Service) Context() *contextengine.Engine { return s.engine }

// Store exposes the repository (read paths in handlers/agent).
func (s *Service) Store() *postgres.Store { return s.store }

// Gateway exposes the model gateway.
func (s *Service) Gateway() *models.Gateway { return s.gw }

// Logger exposes the logger.
func (s *Service) Logger() *slog.Logger { return s.log }

// activity records an activity event; failures are logged, never fatal.
func (s *Service) activity(ctx context.Context, st *postgres.Store, a Actor, ideaID, branchID *uuid.UUID, eventType string, et domain.EntityType, eid *uuid.UUID, summary string, meta map[string]any) {
	ev := &domain.ActivityEvent{IdeaID: ideaID, BranchID: branchID, EventType: eventType, EntityType: et, EntityID: eid, Summary: summary, Actor: a.Kind, Metadata: meta}
	if a.AgentRunID != nil {
		if ev.Metadata == nil {
			ev.Metadata = map[string]any{}
		}
		ev.Metadata["agent_run_id"] = a.AgentRunID.String()
	}
	if err := st.RecordActivity(ctx, a.UserID, ev); err != nil {
		s.log.WarnContext(ctx, "record activity failed", "event", eventType, "error", err)
	}
}

// enqueueEmbedding schedules (re)embedding of an entity; best-effort.
func (s *Service) enqueueEmbedding(ctx context.Context, userID uuid.UUID, t domain.EntityType, id uuid.UUID) {
	if s.gw == nil || s.gw.Embedder() == nil {
		return
	}
	uid := userID
	_, err := s.store.EnqueueJob(ctx, &uid, JobEmbedEntity, EmbedJob{UserID: userID, EntityType: t, EntityID: id}, "embed:"+id.String(), time.Now().Add(500*time.Millisecond))
	if err != nil {
		s.log.WarnContext(ctx, "enqueue embedding failed", "error", err)
	}
}

func ptr[T any](v T) *T { return &v }

func trimTo(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\n"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(strings.TrimLeft(s, "#*-> "))
}

// resolveBranch returns the branch to operate on: explicit (validated to belong to the idea) or the idea's default.
func (s *Service) resolveBranch(ctx context.Context, userID uuid.UUID, idea *domain.Idea, branchID *uuid.UUID) (*domain.Branch, error) {
	if branchID != nil && *branchID != uuid.Nil {
		b, err := s.store.GetBranch(ctx, userID, *branchID)
		if err != nil {
			return nil, err
		}
		if b.IdeaID != idea.ID {
			return nil, domain.Invalid("branch_id", "branch does not belong to this idea")
		}
		return b, nil
	}
	if idea.DefaultBranchID == nil {
		return nil, domain.Conflict("idea has no default branch")
	}
	return s.store.GetBranch(ctx, userID, *idea.DefaultBranchID)
}
