package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/repository/postgres"
)

// EmbedJob is the payload for embedding jobs.
type EmbedJob struct {
	UserID     uuid.UUID         `json:"user_id"`
	EntityType domain.EntityType `json:"entity_type"`
	EntityID   uuid.UUID         `json:"entity_id"`
}

const chunkChars = 1800

// chunkText splits text on paragraph boundaries into ~chunkChars pieces.
func chunkText(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if len(s) <= chunkChars {
		return []string{s}
	}
	var out []string
	var cur strings.Builder
	for _, para := range strings.Split(s, "\n\n") {
		for len(para) > chunkChars {
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
			cut := strings.LastIndex(para[:chunkChars], " ")
			if cut < chunkChars/2 {
				cut = chunkChars
			}
			out = append(out, para[:cut])
			para = para[cut:]
		}
		if cur.Len()+len(para) > chunkChars && cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteString("\n\n")
		}
		cur.WriteString(para)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}

func hashText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

// EmbedEntity (re)computes embeddings for one entity, skipping unchanged chunks.
func (s *Service) EmbedEntity(ctx context.Context, userID uuid.UUID, t domain.EntityType, id uuid.UUID) error {
	emb := s.gw.Embedder()
	if emb == nil {
		return nil
	}
	var text string
	var ideaID *uuid.UUID
	switch {
	case t == domain.EntityIdea:
		idea, err := s.store.GetIdea(ctx, userID, id)
		if err != nil {
			return ignoreNotFound(err)
		}
		text = idea.Title + "\n" + idea.Summary + "\n" + trimTo(idea.OriginText, 3000) + "\n" + strings.Join(idea.Tags, ", ")
		ideaID = &idea.ID
	case t.IsKnowledge():
		k, err := s.store.GetKnowledge(ctx, userID, id)
		if err != nil {
			return ignoreNotFound(err)
		}
		text = fmt.Sprintf("%s: %s\n%s", k.Kind, k.Statement, k.Details)
		if k.Decision != nil {
			text += "\nRationale: " + k.Decision.Rationale
		}
		ideaID = &k.IdeaID
	case t == domain.EntityMessage:
		m, err := s.store.GetMessage(ctx, userID, id)
		if err != nil {
			return ignoreNotFound(err)
		}
		text = m.Content
		if conv, err := s.store.GetConversation(ctx, userID, m.ConversationID); err == nil {
			ideaID = conv.IdeaID
		}
	case t == domain.EntityArtifact:
		a, err := s.store.GetArtifact(ctx, userID, id)
		if err != nil {
			return ignoreNotFound(err)
		}
		text = a.Title + "\n\n" + a.ContentMarkdown
		ideaID = &a.IdeaID
	default:
		return nil
	}
	chunks := chunkText(text)
	model := emb.Model()
	existing, err := s.store.EmbeddingHashes(ctx, t, id, model)
	if err != nil {
		return err
	}
	var todo []int
	for i, c := range chunks {
		if existing[i] != hashText(c) {
			todo = append(todo, i)
		}
	}
	if len(todo) > 0 {
		texts := make([]string, len(todo))
		for j, i := range todo {
			texts[j] = chunks[i]
		}
		vecs, err := s.gw.Embed(ctx, &userID, texts)
		if err != nil {
			return err
		}
		for j, i := range todo {
			if err := s.store.UpsertEmbedding(ctx, userID, postgres.EmbeddingRow{EntityType: t, EntityID: id, IdeaID: ideaID, ChunkIndex: i, Content: chunks[i],
				ContentHash: hashText(chunks[i]), Provider: emb.ProviderID(), Model: model, Vector: vecs[j]}); err != nil {
				return err
			}
		}
	}
	return s.store.DeleteEmbeddingsFrom(ctx, t, id, model, len(chunks))
}

func ignoreNotFound(err error) error {
	if domain.IsNotFound(err) {
		return nil
	}
	return err
}

// EmbedConversation embeds every substantive message of a conversation.
func (s *Service) EmbedConversation(ctx context.Context, userID, convID uuid.UUID) error {
	msgs, err := s.store.ListMessages(ctx, userID, convID, 0, 5000)
	if err != nil {
		return ignoreNotFound(err)
	}
	for _, m := range msgs {
		if (m.Role != domain.RoleUser && m.Role != domain.RoleAssistant) || len(m.Content) < 20 {
			continue
		}
		if err := s.EmbedEntity(ctx, userID, domain.EntityMessage, m.ID); err != nil {
			return err
		}
	}
	return nil
}

// BackfillEmbeddings embeds entities missing vectors for the current model.
func (s *Service) BackfillEmbeddings(ctx context.Context, userID uuid.UUID, limit int) (int, error) {
	emb := s.gw.Embedder()
	if emb == nil {
		return 0, nil
	}
	todo, err := s.store.UnembeddedEntities(ctx, userID, emb.Model(), limit)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, u := range todo {
		if err := s.EmbedEntity(ctx, userID, u.EntityType, u.EntityID); err != nil {
			s.log.WarnContext(ctx, "backfill embedding failed", "entity", u.EntityID, "error", err)
			continue
		}
		n++
	}
	return n, nil
}

// JobHandler processes one job payload.
type JobHandler func(ctx context.Context, payload json.RawMessage) error

// JobHandlers maps job kinds to handlers for the background worker.
func (s *Service) JobHandlers() map[string]JobHandler {
	return map[string]JobHandler{
		JobImportParse: func(ctx context.Context, p json.RawMessage) error {
			var j ImportJob
			if err := json.Unmarshal(p, &j); err != nil {
				return err
			}
			return s.ProcessImportParse(ctx, j.UserID, j.ImportID)
		},
		JobImportCommit: func(ctx context.Context, p json.RawMessage) error {
			var j ImportJob
			if err := json.Unmarshal(p, &j); err != nil {
				return err
			}
			return s.ProcessImportCommit(ctx, j.UserID, j.ImportID)
		},
		JobEmbedEntity: func(ctx context.Context, p json.RawMessage) error {
			var j EmbedJob
			if err := json.Unmarshal(p, &j); err != nil {
				return err
			}
			return s.EmbedEntity(ctx, j.UserID, j.EntityType, j.EntityID)
		},
		JobEmbedConversation: func(ctx context.Context, p json.RawMessage) error {
			var j EmbedJob
			if err := json.Unmarshal(p, &j); err != nil {
				return err
			}
			return s.EmbedConversation(ctx, j.UserID, j.EntityID)
		},
		JobEmbedBackfill: func(ctx context.Context, p json.RawMessage) error {
			var j EmbedJob
			if err := json.Unmarshal(p, &j); err != nil {
				return err
			}
			cctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			defer cancel()
			_, err := s.BackfillEmbeddings(cctx, j.UserID, 500)
			return err
		},
	}
}
