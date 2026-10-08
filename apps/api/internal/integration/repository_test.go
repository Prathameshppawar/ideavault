package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/db"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/models"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/repository/postgres"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/service"
)

func TestRepositoryIdeaCRUDAndSlugs(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	st := e.Store()
	a := &domain.Idea{UserID: u.ID, Title: "Biker Community Platform", OriginText: "riders plan trips", Tags: []string{"mobility"}}
	if err := st.CreateIdea(ctx(), a); err != nil {
		t.Fatal(err)
	}
	b := &domain.Idea{UserID: u.ID, Title: "Biker community   platform!"}
	if err := st.CreateIdea(ctx(), b); err != nil {
		t.Fatal(err)
	}
	if a.Slug != "biker-community-platform" || b.Slug != "biker-community-platform-2" {
		t.Errorf("slugs must stay unique per user: %q %q", a.Slug, b.Slug)
	}
	if a.Status != domain.StatusExploring || a.Version != 1 || a.CreatedAt.IsZero() || a.Tags[0] != "mobility" {
		t.Errorf("defaults not applied: %+v", a)
	}
	// Another user may reuse the slug.
	u2 := e.NewUser(t)
	c := &domain.Idea{UserID: u2.ID, Title: "Biker Community Platform"}
	if err := st.CreateIdea(ctx(), c); err != nil || c.Slug != "biker-community-platform" {
		t.Errorf("slugs are per-user: %q %v", c.Slug, err)
	}
	got := try(st.GetIdeaBySlug(ctx(), u.ID, "biker-community-platform")).must(t)
	if got.ID != a.ID {
		t.Error("GetIdeaBySlug returned the wrong idea")
	}
	a.Summary = "Interpreted summary"
	a.Status = domain.StatusActive
	a.Version = 2
	if err := st.UpdateIdea(ctx(), a); err != nil {
		t.Fatal(err)
	}
	got = try(st.GetIdea(ctx(), u.ID, a.ID)).must(t)
	if got.Summary != "Interpreted summary" || got.Status != domain.StatusActive || got.Version != 2 {
		t.Errorf("update not persisted: %+v", got)
	}
	ideas, total, err := st.ListIdeas(ctx(), postgres.IdeaFilter{UserID: u.ID, Query: "biker", Sort: "title"})
	if err != nil || total != 2 || len(ideas) != 2 {
		t.Errorf("ListIdeas = %d/%d %v", len(ideas), total, err)
	}
	ideas, total, _ = st.ListIdeas(ctx(), postgres.IdeaFilter{UserID: u.ID, Statuses: []domain.IdeaStatus{domain.StatusActive}})
	if total != 1 || ideas[0].ID != a.ID {
		t.Errorf("status filter = %d", total)
	}
	// Check constraints are enforced by the database.
	bad := &domain.Idea{UserID: u.ID, Title: strings.Repeat("x", 301)}
	if err := st.CreateIdea(ctx(), bad); err == nil {
		t.Error("titles over 300 characters must be rejected by the schema")
	}
	_, err = e.App.Pool.Exec(ctx(), `UPDATE ideas SET status = 'MERGED' WHERE id = $1`, a.ID)
	if err == nil || !db.IsCheckViolation(err) {
		t.Errorf("MERGED without a merge target must violate ideas_merge_target, got %v", err)
	}
}

func TestRepositoryUserScoping(t *testing.T) {
	e := requireEnv(t)
	owner, other := e.NewUser(t), e.NewUser(t)
	created := e.MustIdea(t, owner, "Private Clinic Idea", "clinic scheduling")
	it := e.MustRecord(t, owner, decision(created.Idea.ID, "Use SMS reminders"))
	cp := e.MustCheckpoint(t, owner, created.Idea.ID, nil, "")
	st := e.Store()
	if _, err := st.GetIdea(ctx(), other.ID, created.Idea.ID); !domain.IsNotFound(err) {
		t.Errorf("another user read the idea: %v", err)
	}
	if _, err := st.GetBranch(ctx(), other.ID, created.Branch.ID); !domain.IsNotFound(err) {
		t.Errorf("another user read the branch: %v", err)
	}
	if _, err := st.GetKnowledge(ctx(), other.ID, it.ID); !domain.IsNotFound(err) {
		t.Errorf("another user read the decision: %v", err)
	}
	if _, err := st.GetCheckpoint(ctx(), other.ID, cp.ID); !domain.IsNotFound(err) {
		t.Errorf("another user read the checkpoint: %v", err)
	}
	ideas, total, _ := st.ListIdeas(ctx(), postgres.IdeaFilter{UserID: other.ID})
	if total != 0 || len(ideas) != 0 {
		t.Errorf("another user lists %d ideas", total)
	}
	hits, _ := st.SearchFTS(ctx(), postgres.FTSQuery{UserID: other.ID, Text: "clinic"})
	if len(hits) != 0 {
		t.Errorf("search leaked %d hits across users", len(hits))
	}
	// Writes through the service are scoped too.
	_, err := e.Svc().RecordKnowledge(ctx(), other.Actor(), decision(created.Idea.ID, "Hijack"))
	wantKind(t, err, domain.KindNotFound)
	_, err = e.Svc().UpdateIdea(ctx(), other.Actor(), created.Idea.ID, service.UpdateIdeaInput{Title: ptr("Stolen")})
	wantKind(t, err, domain.KindNotFound)
	wantKind(t, e.Svc().DeleteIdea(ctx(), other.Actor(), created.Idea.ID), domain.KindNotFound)
	_, err = e.Svc().CreateRelationship(ctx(), other.Actor(), service.CreateRelationshipInput{FromType: domain.EntityDecision, FromID: it.ID,
		ToType: domain.EntityIdea, ToID: created.Idea.ID, RelType: domain.RelRelatedTo})
	wantKind(t, err, domain.KindNotFound)
	if ok, _ := st.EntityOwned(ctx(), other.ID, domain.EntityCheckpoint, cp.ID); ok {
		t.Error("EntityOwned must be user-scoped")
	}
	if ok, _ := st.EntityOwned(ctx(), owner.ID, domain.EntityDecision, it.ID); !ok {
		t.Error("owner owns the decision")
	}
	if ok, _ := st.EntityOwned(ctx(), owner.ID, domain.EntityAssumption, it.ID); ok {
		t.Error("EntityOwned must check the knowledge kind")
	}
}

func ptr[T any](v T) *T { return &v }

func TestRepositoryKnowledgeConstraints(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	idea := e.MustIdea(t, u, "Constraint Idea", "")
	st := e.Store()
	d1 := e.MustRecord(t, u, decision(idea.Idea.ID, "First decision"))
	d2 := e.MustRecord(t, u, decision(idea.Idea.ID, "Second decision"))
	a1 := e.MustRecord(t, u, knowledge(idea.Idea.ID, domain.KindAssumption, "First assumption"))
	if d1.Label != "D1" || d2.Label != "D2" || a1.Label != "A1" {
		t.Errorf("refs are allocated per kind: %s %s %s", d1.Label, d2.Label, a1.Label)
	}
	if d1.LineageID != d1.ID || d1.BranchID != idea.Branch.ID || d1.ReviewState != domain.ReviewAccepted {
		t.Errorf("new item defaults wrong: %+v", d1)
	}
	// Unique ref per branch/kind is enforced by the schema.
	dup := &domain.KnowledgeItem{UserID: u.ID, IdeaID: idea.Idea.ID, BranchID: idea.Branch.ID, Kind: domain.KindDecision, RefNumber: 1, Statement: "dup"}
	err := st.InsertKnowledge(ctx(), dup)
	wantKind(t, err, domain.KindConflict)
	// The same ref is allowed on another branch (forks keep their lineage refs).
	br := &domain.Branch{UserID: u.ID, IdeaID: idea.Idea.ID, Name: "Other"}
	if err := st.CreateBranch(ctx(), br); err != nil {
		t.Fatal(err)
	}
	copyD1 := &domain.KnowledgeItem{UserID: u.ID, IdeaID: idea.Idea.ID, BranchID: br.ID, Kind: domain.KindDecision, RefNumber: 1, LineageID: d1.LineageID, Statement: "First decision"}
	if err := st.InsertKnowledge(ctx(), copyD1); err != nil {
		t.Fatalf("same ref on another branch must be allowed: %v", err)
	}
	// Status values are validated per kind by the schema.
	_, err = e.App.Pool.Exec(ctx(), `UPDATE knowledge_items SET status = 'OPEN' WHERE id = $1`, d1.ID)
	if !db.IsCheckViolation(err) {
		t.Errorf("an invalid status for a decision must violate knowledge_status_valid, got %v", err)
	}
	_, err = e.App.Pool.Exec(ctx(), `UPDATE knowledge_items SET superseded_by_id = id WHERE id = $1`, d1.ID)
	if !db.IsCheckViolation(err) {
		t.Errorf("self-supersession must be rejected, got %v", err)
	}
	// Branch slugs are unique per idea and de-duplicated.
	br2 := &domain.Branch{UserID: u.ID, IdeaID: idea.Idea.ID, Name: "Other"}
	if err := st.CreateBranch(ctx(), br2); err != nil || br2.Slug != "other-2" {
		t.Errorf("branch slug de-dup: %q %v", br2.Slug, err)
	}
	// Only one default branch per idea.
	_, err = e.App.Pool.Exec(ctx(), `UPDATE branches SET is_default = true WHERE id = $1`, br.ID)
	if !db.IsUniqueViolation(err) {
		t.Errorf("a second default branch must violate branches_one_default_idx, got %v", err)
	}
	// Relationships: duplicates are idempotent, self-edges rejected.
	r1 := &domain.Relationship{FromType: domain.EntityDecision, FromID: d2.ID, ToType: domain.EntityDecision, ToID: d1.ID, RelType: domain.RelRelatedTo, Origin: domain.OriginSource, CreatedBy: domain.ActorUser}
	r2 := *r1
	if err := st.CreateRelationship(ctx(), u.ID, r1); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateRelationship(ctx(), u.ID, &r2); err != nil || r2.ID != r1.ID {
		t.Errorf("duplicate edge must return the existing one: %v %v %v", err, r1.ID, r2.ID)
	}
	self := &domain.Relationship{FromType: domain.EntityDecision, FromID: d1.ID, ToType: domain.EntityDecision, ToID: d1.ID, RelType: domain.RelRelatedTo, Origin: domain.OriginSource, CreatedBy: domain.ActorUser}
	if err := st.CreateRelationship(ctx(), u.ID, self); err == nil {
		t.Error("self relationships must be rejected")
	}
	rels := try(st.ListRelationships(ctx(), postgres.RelationshipFilter{UserID: u.ID, EntityID: &d1.ID})).must(t)
	if len(rels) != 1 || !strings.HasPrefix(rels[0].FromLabel, "D2 ") || !strings.HasPrefix(rels[0].ToLabel, "D1 ") {
		t.Errorf("relationship labels: %+v", rels)
	}
}

func TestImmutabilityTriggers(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	idea := e.MustIdea(t, u, "Immutable Idea", "history must never be rewritten")
	d1 := e.MustRecord(t, u, decision(idea.Idea.ID, "Keep history"))
	cp := e.MustCheckpoint(t, u, idea.Idea.ID, nil, "Snapshot")
	art := try(e.Svc().CreateArtifact(ctx(), u.Actor(), service.CreateArtifactInput{IdeaID: idea.Idea.ID, Type: domain.ArtifactCustom, Title: "Doc", ContentMarkdown: "# Doc\nCites [D1]."})).must(t)
	pool := e.App.Pool
	tests := []struct {
		name string
		sql  string
		arg  any
	}{
		{"update checkpoint", `UPDATE checkpoints SET title = 'rewritten' WHERE id = $1`, cp.ID},
		{"delete checkpoint", `DELETE FROM checkpoints WHERE id = $1`, cp.ID},
		{"update checkpoint item", `UPDATE checkpoint_items SET status = 'SUPERSEDED' WHERE checkpoint_id = $1`, cp.ID},
		{"delete checkpoint item", `DELETE FROM checkpoint_items WHERE checkpoint_id = $1`, cp.ID},
		{"update idea version", `UPDATE idea_versions SET title = 'rewritten' WHERE idea_id = $1`, idea.Idea.ID},
		{"delete idea version", `DELETE FROM idea_versions WHERE idea_id = $1`, idea.Idea.ID},
		{"update artifact version", `UPDATE artifact_versions SET content_markdown = 'rewritten' WHERE artifact_id = $1`, art.Artifact.ID},
		{"delete artifact version", `DELETE FROM artifact_versions WHERE artifact_id = $1`, art.Artifact.ID},
		{"update provenance", `UPDATE artifact_provenance SET label = 'X9' WHERE artifact_version_id = (SELECT id FROM artifact_versions WHERE artifact_id = $1)`, art.Artifact.ID},
		{"rewrite knowledge statement", `UPDATE knowledge_items SET statement = 'rewritten' WHERE id = $1`, d1.ID},
		{"rewrite knowledge details", `UPDATE knowledge_items SET details = 'rewritten' WHERE id = $1`, d1.ID},
		{"move knowledge to another kind", `UPDATE knowledge_items SET kind = 'insight', status = 'ACTIVE' WHERE id = $1`, d1.ID},
		{"rewrite knowledge origin", `UPDATE knowledge_items SET origin = 'INTERPRETATION' WHERE id = $1`, d1.ID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tag, err := pool.Exec(ctx(), tt.sql, tt.arg)
			if err == nil {
				t.Fatalf("%s succeeded (%d rows); history must be immutable", tt.name, tag.RowsAffected())
			}
			if !db.IsCheckViolation(err) || !strings.Contains(err.Error(), "immutable") {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
	// Status (not content) may change.
	if _, err := pool.Exec(ctx(), `UPDATE knowledge_items SET status = 'REVERSED' WHERE id = $1`, d1.ID); err != nil {
		t.Errorf("status changes are allowed: %v", err)
	}
	// The repository maps immutability errors to domain.Immutable.
	err := e.Store().UpdateKnowledgeState(ctx(), u.ID, d1.ID, nil, nil, nil)
	if err != nil {
		t.Errorf("no-op state update should succeed: %v", err)
	}
	// Explicit, confirmed idea deletion is the only escape hatch.
	if err := e.Svc().DeleteIdea(ctx(), u.Actor(), idea.Idea.ID); err != nil {
		t.Fatalf("DeleteIdea with history: %v", err)
	}
	for _, q := range []string{
		`SELECT count(*) FROM checkpoints WHERE idea_id = $1`, `SELECT count(*) FROM idea_versions WHERE idea_id = $1`,
		`SELECT count(*) FROM knowledge_items WHERE idea_id = $1`, `SELECT count(*) FROM artifacts WHERE idea_id = $1`,
		`SELECT count(*) FROM relationships WHERE idea_id = $1`,
	} {
		if n := countRows(t, q, idea.Idea.ID); n != 0 {
			t.Errorf("%s = %d after deleting the idea", q, n)
		}
	}
	if countRows(t, `SELECT count(*) FROM artifact_versions WHERE artifact_id = $1`, art.Artifact.ID) != 0 {
		t.Error("artifact versions must be deleted with the idea")
	}
	// The escape hatch is transaction-local: history is protected again afterwards.
	other := e.MustIdea(t, u, "Still Protected", "")
	cp2 := e.MustCheckpoint(t, u, other.Idea.ID, nil, "")
	if _, err := pool.Exec(ctx(), `DELETE FROM checkpoints WHERE id = $1`, cp2.ID); err == nil {
		t.Error("allow_history_delete leaked outside its transaction")
	}
	if err := e.Store().AllowHistoryDelete(ctx()); err == nil {
		t.Error("AllowHistoryDelete outside a transaction must fail")
	}
}

func TestSemanticSearchReturnsNearest(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	st := e.Store()
	model := "test-embed-v1"
	unit := func(i int) []float32 {
		v := make([]float32, models.EmbeddingDims)
		v[i] = 1
		return v
	}
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	for i, id := range ids {
		if err := st.UpsertEmbedding(ctx(), u.ID, postgres.EmbeddingRow{EntityType: domain.EntityIdea, EntityID: id, Content: "c", ContentHash: "h", Provider: "test", Model: model, Vector: unit(i)}); err != nil {
			t.Fatal(err)
		}
	}
	// A different user's identical vector must never be returned.
	stranger := e.NewUser(t)
	_ = st.UpsertEmbedding(ctx(), stranger.ID, postgres.EmbeddingRow{EntityType: domain.EntityIdea, EntityID: uuid.New(), Content: "c", ContentHash: "h", Provider: "test", Model: model, Vector: unit(1)})
	q := make([]float32, models.EmbeddingDims)
	q[1], q[2] = 0.9, 0.1
	hits := try(st.SemanticSearch(ctx(), u.ID, model, q, []domain.EntityType{domain.EntityIdea}, nil, 10)).must(t)
	if len(hits) != 3 || hits[0].EntityID != ids[1] || hits[1].EntityID != ids[2] || hits[2].EntityID != ids[0] {
		t.Fatalf("nearest-neighbour order wrong: %+v", hits)
	}
	if hits[0].Score < 0.99 || hits[2].Score > 0.01 {
		t.Errorf("cosine scores: %v %v", hits[0].Score, hits[2].Score)
	}
	if hs := try(st.SemanticSearch(ctx(), u.ID, "other-model", q, nil, nil, 10)).must(t); len(hs) != 0 {
		t.Error("vectors from different models must never be compared")
	}
	// Upsert replaces a chunk; hashes allow skipping unchanged content.
	if err := st.UpsertEmbedding(ctx(), u.ID, postgres.EmbeddingRow{EntityType: domain.EntityIdea, EntityID: ids[0], Content: "c2", ContentHash: "h2", Provider: "test", Model: model, Vector: unit(1)}); err != nil {
		t.Fatal(err)
	}
	hashes := try(st.EmbeddingHashes(ctx(), domain.EntityIdea, ids[0], model)).must(t)
	if hashes[0] != "h2" || len(hashes) != 1 {
		t.Errorf("hashes = %v", hashes)
	}
}

func TestEmbeddingPipelineAndHybridSearch(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	bike := e.MustIdea(t, u, "Biker Community Platform", "Riders create group trips, plan routes and invite friends to ride together.")
	clinic := e.MustIdea(t, u, "Clinic Scheduling Assistant", "Doctors manage appointments and patients get SMS reminders.")
	e.MustRecord(t, u, decision(bike.Idea.ID, "Riders plan group trips with shared routes"))
	e.DrainJobs(t) // embeddings are queued by the service and processed by the worker
	emb := e.App.Gateway.Embedder()
	if emb.Model() != "hash-768-v1" {
		t.Fatalf("tests must use the offline hash embedder, got %s", emb.Model())
	}
	cov := try(e.Store().EmbeddingCoverage(ctx(), u.ID, emb.Model())).must(t)
	if cov["idea"] != 2 || cov["decision"] != 1 {
		t.Errorf("embedding coverage = %v", cov)
	}
	vec := try(e.App.Gateway.Embed(ctx(), &u.ID, []string{"group motorcycle trips and routes"})).must(t)
	hits := try(e.Store().SemanticSearch(ctx(), u.ID, emb.Model(), vec[0], []domain.EntityType{domain.EntityIdea}, nil, 5)).must(t)
	if len(hits) < 2 || hits[0].EntityID != bike.Idea.ID {
		t.Errorf("semantic search should rank the biker idea first: %+v", hits)
	}
	res := try(e.Svc().Search(ctx(), u.ID, service.SearchRequest{Query: "appointments reminders"})).must(t)
	if !res.Semantic || len(res.Results) == 0 || res.Results[0].EntityID != clinic.Idea.ID {
		t.Errorf("hybrid search top result should be the clinic idea: semantic=%v %+v", res.Semantic, res.Results)
	}
	// Re-embedding unchanged content is a no-op (hash check), and backfill finds nothing new.
	if n := try(e.Svc().BackfillEmbeddings(ctx(), u.ID, 100)).must(t); n != 0 {
		t.Errorf("backfill re-embedded %d already-embedded entities", n)
	}
}

func TestFullTextSearch(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	st := e.Store()
	idea := e.MustIdea(t, u, "Zoho to SharePoint Extension", "Stream mail attachments into SharePoint.")
	d := e.MustRecord(t, u, decision(idea.Idea.ID, "Use a Chrome extension with Manifest V3"))
	a := e.MustRecord(t, u, service.RecordKnowledgeInput{IdeaID: idea.Idea.ID, Kind: domain.KindAssumption, Statement: "SharePoint accepts uploads from the extension"})
	conv := &domain.Conversation{UserID: u.ID, IdeaID: &idea.Idea.ID, Title: "Extension architecture chat", Origin: domain.OriginImported, Provider: "chatgpt"}
	if err := st.CreateConversation(ctx(), conv); err != nil {
		t.Fatal(err)
	}
	early := &domain.Message{UserID: u.ID, ConversationID: conv.ID, Role: domain.RoleUser, Content: "Could a browser extension stream attachments?", Untrusted: true,
		CreatedAt: time.Now().Add(-48 * time.Hour)}
	if err := st.AppendMessage(ctx(), early); err != nil {
		t.Fatal(err)
	}
	all := try(st.SearchFTS(ctx(), postgres.FTSQuery{UserID: u.ID, Text: "extension"})).must(t)
	types := map[domain.EntityType]bool{}
	for _, h := range all {
		types[h.EntityType] = true
		if h.EntityID == early.ID && (!h.Untrusted || h.Role != "user") {
			t.Errorf("imported messages are flagged untrusted in search: %+v", h)
		}
	}
	for _, want := range []domain.EntityType{domain.EntityIdea, domain.EntityDecision, domain.EntityAssumption, domain.EntityConversation, domain.EntityMessage} {
		if !types[want] {
			t.Errorf("FTS did not return a %s hit: %v", want, types)
		}
	}
	onlyDecisions := try(st.SearchFTS(ctx(), postgres.FTSQuery{UserID: u.ID, Text: "extension", Types: []domain.EntityType{domain.EntityDecision}})).must(t)
	if len(onlyDecisions) != 1 || onlyDecisions[0].EntityID != d.ID || onlyDecisions[0].Label != "D1" {
		t.Errorf("type filter: %+v", onlyDecisions)
	}
	unvalidated := try(st.SearchFTS(ctx(), postgres.FTSQuery{UserID: u.ID, Types: []domain.EntityType{domain.EntityAssumption}, Statuses: []string{"UNVALIDATED"}})).must(t)
	if len(unvalidated) != 1 || unvalidated[0].EntityID != a.ID {
		t.Errorf("status filter without text: %+v", unvalidated)
	}
	oldest := try(st.SearchFTS(ctx(), postgres.FTSQuery{UserID: u.ID, Text: "extension", Order: "oldest"})).must(t)
	if len(oldest) == 0 || oldest[0].EntityID != early.ID {
		t.Errorf("oldest-first must start with the earliest message, got %+v", oldest[0])
	}
	for i := 1; i < len(oldest); i++ {
		if oldest[i].CreatedAt.Before(oldest[i-1].CreatedAt) {
			t.Error("oldest order not chronological")
		}
	}
	if hits := try(st.SearchFTS(ctx(), postgres.FTSQuery{UserID: u.ID, Text: "kubernetes"})).must(t); len(hits) != 0 {
		t.Errorf("unrelated query returned %d hits", len(hits))
	}
	// websearch syntax and odd input never error.
	for _, q := range []string{`"manifest v3"`, "-sharepoint extension", "a OR b", "'; DROP TABLE ideas; --", "%_\\"} {
		if _, err := st.SearchFTS(ctx(), postgres.FTSQuery{UserID: u.ID, Text: q}); err != nil {
			t.Errorf("SearchFTS(%q): %v", q, err)
		}
	}
}

// Short OLTP queries must not pay for JIT compilation: on fresh vaults (no table
// statistics) the supersession-chain query was estimated at ~1M rows and spent
// about a second in JIT on every "why did we decide…" explanation.
func TestConnectionsDisableJITForFastExplanations(t *testing.T) {
	e := requireEnv(t)
	var jit string
	if err := e.App.Pool.QueryRow(ctx(), `SELECT current_setting('jit')`).Scan(&jit); err != nil || jit != "off" {
		t.Fatalf("jit = %q (%v); the pool must disable JIT", jit, err)
	}
	u := e.NewUser(t)
	idea := e.MustIdea(t, u, "Latency Idea", "")
	d1 := e.MustRecord(t, u, decision(idea.Idea.ID, "First"))
	d2 := e.MustRecord(t, u, service.RecordKnowledgeInput{IdeaID: idea.Idea.ID, Kind: domain.KindDecision, Statement: "Second", Supersedes: &d1.ID})
	start := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := e.Svc().ExplainKnowledge(ctx(), u.ID, d2.ID); err != nil {
			t.Fatal(err)
		}
	}
	if el := time.Since(start); el > 1500*time.Millisecond {
		t.Errorf("3 explanations took %v; expected well under a second", el)
	}
}
