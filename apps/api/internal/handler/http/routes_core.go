package httpapi

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/repository/postgres"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/service"
)

// ---------- request/response DTOs ----------

type authReq struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name,omitempty"`
}

type authResp struct {
	User      *domain.User `json:"user"`
	ExpiresAt string       `json:"expires_at"`
}

type authStatus struct {
	SetupRequired bool         `json:"setup_required"`
	Authenticated bool         `json:"authenticated"`
	User          *domain.User `json:"user,omitempty"`
	Version       string       `json:"version"`
}

type passwordReq struct {
	Current string `json:"current"`
	Next    string `json:"next"`
}

type tokenReq struct {
	Label string `json:"label"`
}

type tokenResp struct {
	Token   string          `json:"token"`
	Session *domain.Session `json:"session"`
	Note    string          `json:"note"`
}

type ideaList struct {
	Ideas []domain.Idea `json:"ideas"`
	Total int           `json:"total"`
}

type duplicateReq struct {
	Text string `json:"text"`
}

type duplicateResp struct {
	Candidates []service.IdeaCandidate `json:"candidates"`
}

type linkIdeaReq struct {
	ToIdeaID  uuid.UUID      `json:"to_idea_id"`
	RelType   domain.RelType `json:"rel_type"`
	Rationale string         `json:"rationale,omitempty"`
}

type reopenReq struct {
	Reason string `json:"reason,omitempty"`
}

type forkReq struct {
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Selections  []service.Selection `json:"selections,omitempty"`
}

type mergeContextReq struct {
	Selections []service.Selection `json:"selections"`
	Note       string              `json:"note,omitempty"`
}

type reviewReq struct {
	IDs    []uuid.UUID `json:"ids"`
	Accept bool        `json:"accept"`
}

type itemsResp struct {
	Items []domain.KnowledgeItem `json:"items"`
}

type convPatch struct {
	Title    *string    `json:"title,omitempty"`
	IdeaID   *uuid.UUID `json:"idea_id,omitempty"`
	BranchID *uuid.UUID `json:"branch_id,omitempty"`
}

type convDetail struct {
	Conversation *domain.Conversation `json:"conversation"`
	Messages     []domain.Message     `json:"messages"`
}

type extractReq struct {
	IdeaID  *uuid.UUID `json:"idea_id,omitempty"`
	Persist bool       `json:"persist"`
	// Replace rejects this conversation's pending proposals before saving new ones.
	Replace bool `json:"replace,omitempty"`
}

type extractResp struct {
	Result    *service.ExtractResult `json:"result"`
	Proposals []domain.KnowledgeItem `json:"proposals"`
}

type okResp struct {
	OK bool `json:"ok"`
}

func (a *API) registerCore() {
	// ---------- Auth ----------
	a.add(Route{Method: "GET", Path: "/v1/auth/status", Tag: "auth", Summary: "Setup/authentication status", Resp: authStatus{}, Handler: a.authStatus})
	a.add(Route{Method: "POST", Path: "/v1/auth/setup", Tag: "auth", Summary: "Create the vault owner (first run only)", Req: authReq{}, Resp: authResp{}, Limit: "auth", Handler: a.setup})
	a.add(Route{Method: "POST", Path: "/v1/auth/login", Tag: "auth", Summary: "Sign in", Req: authReq{}, Resp: authResp{}, Limit: "auth", Handler: a.login})
	a.add(Route{Method: "POST", Path: "/v1/auth/logout", Tag: "auth", Summary: "Sign out", Resp: okResp{}, Handler: a.logout})
	a.add(Route{Method: "GET", Path: "/v1/auth/me", Tag: "auth", Summary: "Current user", Auth: true, Resp: domain.User{}, Handler: func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, userFrom(r)) }})
	a.add(Route{Method: "POST", Path: "/v1/auth/password", Tag: "auth", Summary: "Change password", Auth: true, Req: passwordReq{}, Resp: okResp{}, Limit: "auth", Handler: a.changePassword})
	a.add(Route{Method: "GET", Path: "/v1/auth/sessions", Tag: "auth", Summary: "List sessions and API tokens", Auth: true, Resp: []domain.Session{}, Handler: a.listSessions})
	a.add(Route{Method: "DELETE", Path: "/v1/auth/sessions/{id}", Tag: "auth", Summary: "Revoke a session/token", Auth: true, Resp: okResp{}, Handler: a.revokeSession})
	a.add(Route{Method: "POST", Path: "/v1/auth/tokens", Tag: "auth", Summary: "Create a personal API token (shown once)", Auth: true, Req: tokenReq{}, Resp: tokenResp{}, Handler: a.createToken})

	// ---------- Ideas ----------
	a.add(Route{Method: "GET", Path: "/v1/ideas", Tag: "ideas", Summary: "List ideas", Auth: true, Resp: ideaList{},
		Query:   []Param{{"status", "string", "comma-separated statuses"}, {"q", "string", "text filter"}, {"tag", "string", ""}, {"sort", "string", "recent|created|title|momentum"}, {"limit", "integer", ""}, {"offset", "integer", ""}},
		Handler: a.listIdeas})
	a.add(Route{Method: "POST", Path: "/v1/ideas", Tag: "ideas", Summary: "Create an Idea Space", Auth: true, Req: service.CreateIdeaInput{}, Resp: service.IdeaCreated{}, Handler: a.createIdea})
	a.add(Route{Method: "POST", Path: "/v1/ideas/check-duplicates", Tag: "ideas", Summary: "Find existing ideas matching text", Auth: true, Req: duplicateReq{}, Resp: duplicateResp{}, Handler: a.checkDuplicates})
	a.add(Route{Method: "GET", Path: "/v1/ideas/{id}", Tag: "ideas", Summary: "Idea overview (branch-aware)", Auth: true, Resp: service.IdeaOverview{}, Query: []Param{{"branch_id", "uuid", ""}}, Handler: a.getIdea})
	a.add(Route{Method: "PATCH", Path: "/v1/ideas/{id}", Tag: "ideas", Summary: "Update idea identity/status (versioned)", Auth: true, Req: service.UpdateIdeaInput{}, Resp: domain.Idea{}, Handler: a.updateIdea})
	a.add(Route{Method: "DELETE", Path: "/v1/ideas/{id}", Tag: "ideas", Summary: "Permanently delete an idea (requires X-Confirm: delete)", Auth: true, Resp: okResp{}, Handler: a.deleteIdea})
	a.add(Route{Method: "GET", Path: "/v1/ideas/{id}/versions", Tag: "ideas", Summary: "Idea version history", Auth: true, Resp: []domain.IdeaVersion{}, Handler: a.ideaVersions})
	a.add(Route{Method: "GET", Path: "/v1/ideas/{id}/journey", Tag: "ideas", Summary: "Idea journey (origin → current)", Auth: true, Resp: []service.JourneyNode{}, Handler: a.journey})
	a.add(Route{Method: "GET", Path: "/v1/ideas/{id}/tree", Tag: "branches", Summary: "Branch tree", Auth: true, Resp: service.BranchTree{}, Handler: a.branchTree})
	a.add(Route{Method: "POST", Path: "/v1/ideas/{id}/conclude", Tag: "ideas", Summary: "Conclude an idea", Auth: true, Req: service.ConcludeInput{}, Resp: service.ConclusionResult{}, Handler: a.conclude})
	a.add(Route{Method: "POST", Path: "/v1/ideas/{id}/reopen", Tag: "ideas", Summary: "Reopen an idea", Auth: true, Req: reopenReq{}, Resp: domain.Idea{}, Handler: a.reopen})
	a.add(Route{Method: "POST", Path: "/v1/ideas/{id}/links", Tag: "ideas", Summary: "Relate two ideas", Auth: true, Req: linkIdeaReq{}, Resp: domain.Relationship{}, Handler: a.linkIdeas})
	a.add(Route{Method: "GET", Path: "/v1/ideas/{id}/similar", Tag: "ideas", Summary: "Similar ideas", Auth: true, Resp: duplicateResp{}, Handler: a.similarIdeas})
	a.add(Route{Method: "GET", Path: "/v1/ideas/{id}/evolution", Tag: "analysis", Summary: "How the idea evolved", Auth: true, Resp: service.IdeaEvolution{}, Handler: a.evolution})
	a.add(Route{Method: "POST", Path: "/v1/ideas/{id}/contradictions", Tag: "analysis", Summary: "Detect contradictions", Auth: true, Resp: contradictionsResp{}, Query: []Param{{"branch_id", "uuid", ""}}, Handler: a.contradictions})

	// ---------- Branches ----------
	a.add(Route{Method: "GET", Path: "/v1/ideas/{id}/branches", Tag: "branches", Summary: "List branches", Auth: true, Resp: []domain.Branch{}, Handler: a.listBranches})
	a.add(Route{Method: "POST", Path: "/v1/ideas/{id}/branches", Tag: "branches", Summary: "Create a branch (from a branch head or checkpoint)", Auth: true, Req: service.CreateBranchInput{}, Resp: service.ForkResult{}, Handler: a.createBranch})
	a.add(Route{Method: "GET", Path: "/v1/branches/compare", Tag: "branches", Summary: "Compare two branches", Auth: true, Resp: service.BranchComparison{},
		Query: []Param{{"a", "uuid", "branch A"}, {"b", "uuid", "branch B"}, {"explain", "boolean", "LLM narrative"}}, Handler: a.compareBranches})
	a.add(Route{Method: "GET", Path: "/v1/branches/{id}", Tag: "branches", Summary: "Get a branch", Auth: true, Resp: domain.Branch{}, Handler: a.getBranch})
	a.add(Route{Method: "PATCH", Path: "/v1/branches/{id}", Tag: "branches", Summary: "Rename / change status / make default", Auth: true, Req: service.UpdateBranchInput{}, Resp: domain.Branch{}, Handler: a.updateBranch})
	a.add(Route{Method: "GET", Path: "/v1/branches/{id}/inheritance", Tag: "branches", Summary: "Exactly what the branch inherited", Auth: true, Resp: []domain.InheritanceRecord{}, Handler: a.inheritance})
	a.add(Route{Method: "POST", Path: "/v1/branches/{id}/merge-context", Tag: "branches", Summary: "Bring selected knowledge into the branch", Auth: true, Req: mergeContextReq{}, Resp: service.ForkResult{}, Handler: a.mergeContext})

	// ---------- Checkpoints ----------
	a.add(Route{Method: "GET", Path: "/v1/ideas/{id}/checkpoints", Tag: "checkpoints", Summary: "List checkpoints", Auth: true, Resp: []domain.Checkpoint{}, Query: []Param{{"branch_id", "uuid", ""}}, Handler: a.listCheckpoints})
	a.add(Route{Method: "POST", Path: "/v1/ideas/{id}/checkpoints", Tag: "checkpoints", Summary: "Create an immutable checkpoint", Auth: true, Req: service.CreateCheckpointInput{}, Resp: domain.Checkpoint{}, Handler: a.createCheckpoint})
	a.add(Route{Method: "GET", Path: "/v1/checkpoints/compare", Tag: "checkpoints", Summary: "Diff two checkpoints", Auth: true, Resp: domain.CheckpointDiff{}, Query: []Param{{"from", "uuid", ""}, {"to", "uuid", ""}}, Handler: a.compareCheckpoints})
	a.add(Route{Method: "GET", Path: "/v1/checkpoints/{id}", Tag: "checkpoints", Summary: "Get a checkpoint with its snapshot", Auth: true, Resp: domain.Checkpoint{}, Handler: a.getCheckpoint})
	a.add(Route{Method: "POST", Path: "/v1/checkpoints/{id}/fork", Tag: "checkpoints", Summary: "Fork a new branch from a checkpoint (optionally with selected later knowledge)", Auth: true, Req: forkReq{}, Resp: service.ForkResult{}, Handler: a.forkCheckpoint})

	// ---------- Knowledge ----------
	a.add(Route{Method: "GET", Path: "/v1/knowledge", Tag: "knowledge", Summary: "List knowledge items", Auth: true, Resp: itemsResp{},
		Query:   []Param{{"idea_id", "uuid", ""}, {"branch_id", "uuid", ""}, {"kind", "string", "comma-separated"}, {"status", "string", "comma-separated"}, {"review", "string", "PROPOSED|ACCEPTED|REJECTED"}, {"q", "string", ""}, {"include_history", "boolean", ""}, {"limit", "integer", ""}},
		Handler: a.listKnowledge})
	a.add(Route{Method: "POST", Path: "/v1/knowledge", Tag: "knowledge", Summary: "Record a decision/assumption/evidence/insight/question/action", Auth: true, Req: service.RecordKnowledgeInput{}, Resp: domain.KnowledgeItem{}, Handler: a.recordKnowledge})
	a.add(Route{Method: "POST", Path: "/v1/knowledge/review", Tag: "knowledge", Summary: "Accept/reject proposed items", Auth: true, Req: reviewReq{}, Resp: itemsResp{}, Handler: a.reviewKnowledge})
	a.add(Route{Method: "GET", Path: "/v1/knowledge/{id}", Tag: "knowledge", Summary: "Explain an item (history, evidence, source, checkpoints)", Auth: true, Resp: service.KnowledgeExplanation{}, Handler: a.explainKnowledge})
	a.add(Route{Method: "PATCH", Path: "/v1/knowledge/{id}/status", Tag: "knowledge", Summary: "Change an item's status", Auth: true, Req: service.UpdateKnowledgeStatusInput{}, Resp: domain.KnowledgeItem{}, Handler: a.updateKnowledgeStatus})

	// ---------- Relationships ----------
	a.add(Route{Method: "GET", Path: "/v1/relationships", Tag: "relationships", Summary: "List relationships", Auth: true, Resp: []domain.Relationship{}, Query: []Param{{"idea_id", "uuid", ""}, {"entity_id", "uuid", ""}}, Handler: a.listRelationships})
	a.add(Route{Method: "POST", Path: "/v1/relationships", Tag: "relationships", Summary: "Create a relationship", Auth: true, Req: service.CreateRelationshipInput{}, Resp: domain.Relationship{}, Handler: a.createRelationship})
	a.add(Route{Method: "DELETE", Path: "/v1/relationships/{id}", Tag: "relationships", Summary: "Delete a relationship", Auth: true, Resp: okResp{}, Handler: a.deleteRelationship})

	// ---------- Conversations ----------
	a.add(Route{Method: "GET", Path: "/v1/conversations", Tag: "conversations", Summary: "List conversations", Auth: true, Resp: []domain.Conversation{},
		Query: []Param{{"idea_id", "uuid", ""}, {"branch_id", "uuid", ""}, {"origin", "string", "native|imported|external"}, {"q", "string", ""}, {"limit", "integer", ""}}, Handler: a.listConversations})
	a.add(Route{Method: "GET", Path: "/v1/conversations/{id}", Tag: "conversations", Summary: "Conversation with messages", Auth: true, Resp: convDetail{}, Query: []Param{{"after", "integer", "position"}, {"limit", "integer", ""}}, Handler: a.getConversation})
	a.add(Route{Method: "GET", Path: "/v1/messages/{id}", Tag: "conversations", Summary: "One message (resolves its conversation for deep links)", Auth: true, Resp: domain.Message{}, Handler: a.getMessage})
	a.add(Route{Method: "PATCH", Path: "/v1/conversations/{id}", Tag: "conversations", Summary: "Rename or attach to an idea", Auth: true, Req: convPatch{}, Resp: domain.Conversation{}, Handler: a.patchConversation})
	a.add(Route{Method: "POST", Path: "/v1/conversations/{id}/extract", Tag: "conversations", Summary: "Extract candidate knowledge (optionally persist as proposals)", Auth: true, Req: extractReq{}, Resp: extractResp{}, Handler: a.extractConversation})
}

// ---------- auth handlers ----------

func (a *API) authStatus(w http.ResponseWriter, r *http.Request) {
	setup, err := a.svc.SetupRequired(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	st := authStatus{SetupRequired: setup, Version: Version}
	tok := bearer(r)
	if c, err := r.Cookie(sessionCookie); err == nil && tok == "" {
		tok = c.Value
	}
	if tok != "" {
		if u, _, err := a.svc.Authenticate(r.Context(), tok); err == nil {
			st.Authenticated, st.User = true, u
		}
	}
	writeJSON(w, 200, st)
}

func (a *API) setup(w http.ResponseWriter, r *http.Request) {
	var in authReq
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	res, err := a.svc.Setup(r.Context(), in.Email, in.Password, in.DisplayName, r.UserAgent(), clientIP(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	a.setSessionCookie(w, res.Token, res.ExpiresAt)
	writeJSON(w, 201, authResp{User: res.User, ExpiresAt: res.ExpiresAt.Format("2006-01-02T15:04:05Z07:00")})
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var in authReq
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	res, err := a.svc.Login(r.Context(), in.Email, in.Password, r.UserAgent(), clientIP(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	a.setSessionCookie(w, res.Token, res.ExpiresAt)
	writeJSON(w, 200, authResp{User: res.User, ExpiresAt: res.ExpiresAt.Format("2006-01-02T15:04:05Z07:00")})
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = a.svc.Logout(r.Context(), c.Value)
	}
	a.clearSessionCookie(w)
	writeJSON(w, 200, okResp{OK: true})
}

func (a *API) changePassword(w http.ResponseWriter, r *http.Request) {
	var in passwordReq
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	if err := a.svc.ChangePassword(r.Context(), uid(r), in.Current, in.Next); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, okResp{OK: true})
}

func (a *API) listSessions(w http.ResponseWriter, r *http.Request) {
	ss, err := a.svc.Store().ListSessions(r.Context(), uid(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, nonNilSlice(ss))
}

func (a *API) revokeSession(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := a.svc.Store().RevokeSessionByID(r.Context(), uid(r), id); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, okResp{OK: true})
}

func (a *API) createToken(w http.ResponseWriter, r *http.Request) {
	var in tokenReq
	_ = decodeJSON(w, r, &in)
	res, err := a.svc.CreateAPIToken(r.Context(), uid(r), in.Label)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 201, tokenResp{Token: res.Token, Session: res.Session, Note: "Store this token now; it will not be shown again. Use it as 'Authorization: Bearer <token>'."})
}

// ---------- ideas ----------

func (a *API) listIdeas(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := postgres.IdeaFilter{UserID: uid(r), Query: q.Get("q"), Tag: q.Get("tag"), Sort: q.Get("sort"), Limit: queryInt(r, "limit", 50), Offset: queryInt(r, "offset", 0)}
	for _, s := range strings.Split(q.Get("status"), ",") {
		if s = strings.ToUpper(strings.TrimSpace(s)); s != "" {
			f.Statuses = append(f.Statuses, domain.IdeaStatus(s))
		}
	}
	ideas, total, err := a.svc.ListIdeas(r.Context(), f)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, ideaList{Ideas: nonNilSlice(ideas), Total: total})
}

func (a *API) createIdea(w http.ResponseWriter, r *http.Request) {
	var in service.CreateIdeaInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	res, err := a.svc.CreateIdea(r.Context(), actor(r), in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 201, res)
}

func (a *API) checkDuplicates(w http.ResponseWriter, r *http.Request) {
	var in duplicateReq
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	c, err := a.svc.FindIdeaCandidates(r.Context(), uid(r), in.Text, 5)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, duplicateResp{Candidates: nonNilSlice(c)})
}

func (a *API) getIdea(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	bid, err := queryUUID(r, "branch_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	ov, err := a.svc.IdeaOverview(r.Context(), uid(r), id, bid)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, ov)
}

func (a *API) updateIdea(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in service.UpdateIdeaInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	idea, err := a.svc.UpdateIdea(r.Context(), actor(r), id, in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, idea)
}

func (a *API) deleteIdea(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	if r.Header.Get("X-Confirm") != "delete" {
		writeError(w, r, domain.Invalid("confirm", "destructive: send header X-Confirm: delete"))
		return
	}
	if err := a.svc.DeleteIdea(r.Context(), actor(r), id); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, okResp{OK: true})
}

func (a *API) ideaVersions(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	v, err := a.svc.Store().ListIdeaVersions(r.Context(), uid(r), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, nonNilSlice(v))
}

func (a *API) journey(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	j, err := a.svc.Journey(r.Context(), uid(r), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, j)
}

func (a *API) branchTree(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	t, err := a.svc.GetBranchTree(r.Context(), uid(r), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, t)
}

func (a *API) conclude(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in service.ConcludeInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	in.IdeaID = id
	res, err := a.svc.ConcludeIdea(r.Context(), actor(r), in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, res)
}

func (a *API) reopen(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in reopenReq
	_ = decodeJSON(w, r, &in)
	idea, err := a.svc.ReopenIdea(r.Context(), actor(r), id, in.Reason)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, idea)
}

func (a *API) linkIdeas(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in linkIdeaReq
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	rel, err := a.svc.LinkIdeas(r.Context(), actor(r), id, in.ToIdeaID, in.RelType, in.Rationale, domain.OriginSource)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 201, rel)
}

func (a *API) similarIdeas(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	idea, err := a.svc.GetIdea(r.Context(), uid(r), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	c, err := a.svc.FindIdeaCandidates(r.Context(), uid(r), idea.Title+". "+idea.Summary+" "+idea.OriginText, 8)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var out []service.IdeaCandidate
	for _, x := range c {
		if x.Idea.ID != id {
			out = append(out, x)
		}
	}
	writeJSON(w, 200, duplicateResp{Candidates: nonNilSlice(out)})
}

// ---------- branches ----------

func (a *API) listBranches(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	b, err := a.svc.Store().ListBranches(r.Context(), uid(r), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, nonNilSlice(b))
}

func (a *API) createBranch(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in service.CreateBranchInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	in.IdeaID = id
	res, err := a.svc.CreateBranch(r.Context(), actor(r), in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 201, res)
}

func (a *API) getBranch(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	b, err := a.svc.Store().GetBranch(r.Context(), uid(r), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, b)
}

func (a *API) updateBranch(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in service.UpdateBranchInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	b, err := a.svc.UpdateBranch(r.Context(), actor(r), id, in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, b)
}

func (a *API) inheritance(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	inh, err := a.svc.Store().ListInheritance(r.Context(), uid(r), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, nonNilSlice(inh))
}

func (a *API) mergeContext(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in mergeContextReq
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	res, err := a.svc.MergeSelectedContext(r.Context(), actor(r), id, in.Selections, in.Note)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, res)
}

func (a *API) compareBranches(w http.ResponseWriter, r *http.Request) {
	ba, err := queryUUID(r, "a")
	if err != nil || ba == nil {
		writeError(w, r, domain.Invalid("a", "branch a is required"))
		return
	}
	bb, err := queryUUID(r, "b")
	if err != nil || bb == nil {
		writeError(w, r, domain.Invalid("b", "branch b is required"))
		return
	}
	c, err := a.svc.CompareBranches(r.Context(), uid(r), *ba, *bb, queryBool(r, "explain"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, c)
}

// ---------- checkpoints ----------

func (a *API) listCheckpoints(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	bid, err := queryUUID(r, "branch_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	cps, err := a.svc.Store().ListCheckpoints(r.Context(), uid(r), id, bid)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, nonNilSlice(cps))
}

func (a *API) createCheckpoint(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in service.CreateCheckpointInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	in.IdeaID = id
	if in.Kind != "" && in.Kind != domain.CheckpointManual {
		in.Kind = domain.CheckpointManual // only manual checkpoints via API; other kinds are system-generated
	}
	cp, err := a.svc.CreateCheckpoint(r.Context(), actor(r), in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 201, cp)
}

func (a *API) getCheckpoint(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	cp, err := a.svc.Store().GetCheckpoint(r.Context(), uid(r), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, cp)
}

func (a *API) forkCheckpoint(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in forkReq
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	res, err := a.svc.ForkFromCheckpoint(r.Context(), actor(r), service.ForkInput{CheckpointID: id, Name: in.Name, Description: in.Description, Selections: in.Selections})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 201, res)
}

func (a *API) compareCheckpoints(w http.ResponseWriter, r *http.Request) {
	from, err := queryUUID(r, "from")
	if err != nil || from == nil {
		writeError(w, r, domain.Invalid("from", "required"))
		return
	}
	to, err := queryUUID(r, "to")
	if err != nil || to == nil {
		writeError(w, r, domain.Invalid("to", "required"))
		return
	}
	d, err := a.svc.CompareCheckpoints(r.Context(), uid(r), *from, *to)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, d)
}

// ---------- knowledge ----------

func (a *API) listKnowledge(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := postgres.KnowledgeFilter{UserID: uid(r), Query: q.Get("q"), Limit: queryInt(r, "limit", 500)}
	var err error
	if f.IdeaID, err = queryUUID(r, "idea_id"); err != nil {
		writeError(w, r, err)
		return
	}
	if f.BranchID, err = queryUUID(r, "branch_id"); err != nil {
		writeError(w, r, err)
		return
	}
	for _, k := range strings.Split(q.Get("kind"), ",") {
		if kk, ok := domain.ParseKnowledgeKind(k); ok {
			f.Kinds = append(f.Kinds, kk)
		}
	}
	for _, s := range strings.Split(q.Get("status"), ",") {
		if s = strings.ToUpper(strings.TrimSpace(s)); s != "" {
			f.Statuses = append(f.Statuses, s)
		}
	}
	switch strings.ToUpper(q.Get("review")) {
	case "PROPOSED":
		f.ReviewStates = []domain.ReviewState{domain.ReviewProposed}
	case "REJECTED":
		f.ReviewStates = []domain.ReviewState{domain.ReviewRejected}
	case "ALL":
	default:
		f.ReviewStates = []domain.ReviewState{domain.ReviewAccepted}
		if !queryBool(r, "include_history") && len(f.Statuses) == 0 {
			f.LiveOnly = true
		}
	}
	items, err := a.svc.Store().ListKnowledge(r.Context(), f)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, itemsResp{Items: nonNilSlice(items)})
}

func (a *API) recordKnowledge(w http.ResponseWriter, r *http.Request) {
	var in service.RecordKnowledgeInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	it, err := a.svc.RecordKnowledge(r.Context(), actor(r), in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 201, it)
}

func (a *API) reviewKnowledge(w http.ResponseWriter, r *http.Request) {
	var in reviewReq
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	items, err := a.svc.ReviewKnowledge(r.Context(), actor(r), in.IDs, in.Accept)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, itemsResp{Items: nonNilSlice(items)})
}

func (a *API) explainKnowledge(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	ex, err := a.svc.ExplainKnowledge(r.Context(), uid(r), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, ex)
}

func (a *API) updateKnowledgeStatus(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in service.UpdateKnowledgeStatusInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	it, err := a.svc.UpdateKnowledgeStatus(r.Context(), actor(r), id, in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, it)
}

// ---------- relationships ----------

func (a *API) listRelationships(w http.ResponseWriter, r *http.Request) {
	f := postgres.RelationshipFilter{UserID: uid(r)}
	var err error
	if f.IdeaID, err = queryUUID(r, "idea_id"); err != nil {
		writeError(w, r, err)
		return
	}
	if f.EntityID, err = queryUUID(r, "entity_id"); err != nil {
		writeError(w, r, err)
		return
	}
	rels, err := a.svc.Store().ListRelationships(r.Context(), f)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, nonNilSlice(rels))
}

func (a *API) createRelationship(w http.ResponseWriter, r *http.Request) {
	var in service.CreateRelationshipInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	rel, err := a.svc.CreateRelationship(r.Context(), actor(r), in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 201, rel)
}

func (a *API) deleteRelationship(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := a.svc.Store().DeleteRelationship(r.Context(), uid(r), id); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, okResp{OK: true})
}

// ---------- conversations ----------

func (a *API) listConversations(w http.ResponseWriter, r *http.Request) {
	f := postgres.ConversationFilter{UserID: uid(r), Origin: r.URL.Query().Get("origin"), Query: r.URL.Query().Get("q"), Limit: queryInt(r, "limit", 50), IncludeLinked: true}
	var err error
	if f.IdeaID, err = queryUUID(r, "idea_id"); err != nil {
		writeError(w, r, err)
		return
	}
	if f.BranchID, err = queryUUID(r, "branch_id"); err != nil {
		writeError(w, r, err)
		return
	}
	cs, err := a.svc.Store().ListConversations(r.Context(), f)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, nonNilSlice(cs))
}

func (a *API) getConversation(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	c, err := a.svc.Store().GetConversation(r.Context(), uid(r), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	msgs, err := a.svc.Store().ListMessages(r.Context(), uid(r), id, queryInt(r, "after", 0), queryInt(r, "limit", 500))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, convDetail{Conversation: c, Messages: nonNilSlice(msgs)})
}

func (a *API) getMessage(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	m, err := a.svc.Store().GetMessage(r.Context(), uid(r), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, m)
}

func (a *API) patchConversation(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in convPatch
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	st := a.svc.Store()
	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		if t == "" {
			writeError(w, r, domain.Invalid("title", "title cannot be empty"))
			return
		}
		if err := st.UpdateConversationMeta(r.Context(), uid(r), id, &t, nil); err != nil {
			writeError(w, r, err)
			return
		}
	}
	if in.IdeaID != nil {
		idea, err := st.GetIdea(r.Context(), uid(r), *in.IdeaID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		bid := idea.DefaultBranchID
		if in.BranchID != nil {
			bid = in.BranchID
		}
		if err := st.AttachConversation(r.Context(), uid(r), id, &idea.ID, bid); err != nil {
			writeError(w, r, err)
			return
		}
	}
	c, err := st.GetConversation(r.Context(), uid(r), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, c)
}

func (a *API) extractConversation(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in extractReq
	_ = decodeJSON(w, r, &in)
	st := a.svc.Store()
	conv, err := st.GetConversation(r.Context(), uid(r), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	msgs, err := st.ListMessages(r.Context(), uid(r), id, 0, 3000)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var em []service.ExtractMessage
	for i, m := range msgs {
		mid := m.ID
		em = append(em, service.ExtractMessage{Index: i, Role: string(m.Role), Content: m.Content, MessageID: &mid})
	}
	ideaID := in.IdeaID
	if ideaID == nil {
		ideaID = conv.IdeaID
	}
	title := ""
	if ideaID != nil {
		if idea, err := st.GetIdea(r.Context(), uid(r), *ideaID); err == nil {
			title = idea.Title
		}
	}
	res, err := a.svc.ExtractKnowledge(r.Context(), uid(r), ideaID, title, em)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := extractResp{Result: res, Proposals: []domain.KnowledgeItem{}}
	if in.Persist {
		if ideaID == nil {
			writeError(w, r, domain.Invalid("idea_id", "attach the conversation to an idea to save proposals"))
			return
		}
		if in.Replace {
			old, err := st.ListKnowledge(r.Context(), postgres.KnowledgeFilter{UserID: uid(r), SourceConversationID: &conv.ID, ReviewStates: []domain.ReviewState{domain.ReviewProposed}, Limit: 1000})
			if err != nil {
				writeError(w, r, err)
				return
			}
			var ids []uuid.UUID
			for _, k := range old {
				ids = append(ids, k.ID)
			}
			if len(ids) > 0 {
				if _, err := a.svc.ReviewKnowledge(r.Context(), actor(r), ids, false); err != nil {
					writeError(w, r, err)
					return
				}
			}
		}
		props, err := a.svc.PersistProposals(r.Context(), actor(r), *ideaID, nil, &conv.ID, conv.SourceID, em, res.Items)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out.Proposals = nonNilSlice(props)
	}
	writeJSON(w, 200, out)
}

func nonNilSlice[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
