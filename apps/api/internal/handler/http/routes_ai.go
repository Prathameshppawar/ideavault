package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/agent"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/artifacts"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/repository/postgres"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/service"
)

type confirmReq struct {
	Approve bool `json:"approve"`
}

type toolInfo struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Category    domain.ToolCategory `json:"category"`
	Parameters  json.RawMessage     `json:"parameters"`
	Permission  string              `json:"permission"`
}

type explainReq struct {
	Question string `json:"question"`
	Version  int    `json:"version,omitempty"`
}

type mergeDeltaReq struct {
	ItemIDs []uuid.UUID `json:"item_ids"`
}

type contradictionsResp struct {
	Findings []service.ContradictionFinding `json:"findings"`
	Analyzer string                         `json:"analyzer"`
}

type promptReq struct {
	Text      string     `json:"text,omitempty"`
	MessageID *uuid.UUID `json:"message_id,omitempty"`
}

type thinkingReq struct {
	IdeaID *uuid.UUID `json:"idea_id,omitempty"`
}

type importCreateReq struct {
	SourceKind   string     `json:"source_kind"`
	Text         string     `json:"text,omitempty"`
	URL          string     `json:"url,omitempty"`
	Filename     string     `json:"filename,omitempty"`
	Adapter      string     `json:"adapter,omitempty"`
	TargetIdeaID *uuid.UUID `json:"target_idea_id,omitempty"`
	// Sync imports immediately (small inputs) instead of queueing a preview.
	Sync    bool `json:"sync,omitempty"`
	NewIdea bool `json:"new_idea,omitempty"`
	Extract bool `json:"extract,omitempty"`
}

type adapterInfo struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
}

func (a *API) registerAI() {
	// ---------- Agent ----------
	a.add(Route{Method: "POST", Path: "/v1/agent/chat", Tag: "agent", Summary: "Chat with the Agent Supervisor (Server-Sent Events: run_started, token, trace, confirmation_required, message, run_completed, error, done)",
		Auth: true, Req: agent.ChatRequest{}, Stream: true, Limit: "agent", Handler: a.chat})
	a.add(Route{Method: "POST", Path: "/v1/agent/tool-calls/{id}/confirm", Tag: "agent", Summary: "Approve or decline a paused DESTRUCTIVE/EXTERNAL action (SSE continues the run)",
		Auth: true, Req: confirmReq{}, Stream: true, Limit: "agent", Handler: a.confirmTool})
	a.add(Route{Method: "GET", Path: "/v1/agent/pending", Tag: "agent", Summary: "Actions awaiting confirmation", Auth: true, Resp: []domain.ToolCallRecord{},
		Query: []Param{{"conversation_id", "uuid", "only this conversation"}}, Handler: a.pending})
	a.add(Route{Method: "GET", Path: "/v1/agent/runs/{id}", Tag: "agent", Summary: "Agent run with execution trace and tool calls", Auth: true, Resp: domain.AgentRun{}, Handler: a.getRun})
	a.add(Route{Method: "GET", Path: "/v1/agent/tools", Tag: "agent", Summary: "Tool catalog with permission categories", Auth: true, Resp: []toolInfo{}, Handler: a.toolCatalog})
	a.add(Route{Method: "GET", Path: "/v1/agent/policy", Tag: "agent", Summary: "Agent permission policy", Auth: true, Resp: agent.Policy{}, Handler: a.getPolicy})
	a.add(Route{Method: "PUT", Path: "/v1/agent/policy", Tag: "agent", Summary: "Update agent permission policy", Auth: true, Req: agent.Policy{}, Resp: agent.Policy{}, Handler: a.putPolicy})

	// ---------- Artifacts ----------
	a.add(Route{Method: "GET", Path: "/v1/artifacts/templates", Tag: "artifacts", Summary: "Artifact types and their sections", Auth: true, Resp: []artifacts.Template{}, Handler: a.templates})
	a.add(Route{Method: "GET", Path: "/v1/artifacts", Tag: "artifacts", Summary: "List artifacts", Auth: true, Resp: []domain.Artifact{},
		Query: []Param{{"idea_id", "uuid", ""}, {"type", "string", ""}, {"status", "string", ""}, {"q", "string", ""}}, Handler: a.listArtifacts})
	a.add(Route{Method: "POST", Path: "/v1/artifacts", Tag: "artifacts", Summary: "Create a hand-written Markdown artifact", Auth: true, Req: service.CreateArtifactInput{}, Resp: service.ArtifactResult{}, Handler: a.createArtifact})
	a.add(Route{Method: "POST", Path: "/v1/artifacts/generate", Tag: "artifacts", Summary: "Generate an artifact from recorded thinking (with provenance)", Auth: true, Req: service.GenerateArtifactInput{}, Resp: service.ArtifactResult{}, Limit: "agent", Handler: a.generateArtifact})
	a.add(Route{Method: "GET", Path: "/v1/artifacts/{id}", Tag: "artifacts", Summary: "Get an artifact", Auth: true, Resp: domain.Artifact{}, Handler: a.getArtifact})
	a.add(Route{Method: "PATCH", Path: "/v1/artifacts/{id}", Tag: "artifacts", Summary: "Edit (new version) or change status", Auth: true, Req: service.UpdateArtifactInput{}, Resp: service.ArtifactResult{}, Handler: a.updateArtifact})
	a.add(Route{Method: "DELETE", Path: "/v1/artifacts/{id}", Tag: "artifacts", Summary: "Permanently delete (requires X-Confirm: delete)", Auth: true, Resp: okResp{}, Handler: a.deleteArtifact})
	a.add(Route{Method: "POST", Path: "/v1/artifacts/{id}/regenerate", Tag: "artifacts", Summary: "Regenerate as a new version", Auth: true, Req: service.RegenerateArtifactInput{}, Resp: service.ArtifactResult{}, Limit: "agent", Handler: a.regenerateArtifact})
	a.add(Route{Method: "GET", Path: "/v1/artifacts/{id}/versions", Tag: "artifacts", Summary: "Version history", Auth: true, Resp: []domain.ArtifactVersion{}, Handler: a.artifactVersions})
	a.add(Route{Method: "GET", Path: "/v1/artifacts/{id}/provenance", Tag: "artifacts", Summary: "Thinking that produced a version", Auth: true, Resp: []domain.ProvenanceEntry{}, Query: []Param{{"version", "integer", "0 = current"}}, Handler: a.artifactProvenance})
	a.add(Route{Method: "POST", Path: "/v1/artifacts/{id}/explain", Tag: "artifacts", Summary: "Why does this artifact say X?", Auth: true, Req: explainReq{}, Resp: service.ArtifactExplanation{}, Handler: a.explainArtifact})
	a.add(Route{Method: "GET", Path: "/v1/artifacts/{id}/download", Tag: "artifacts", Summary: "Download Markdown (.md)", Auth: true, Raw: "text/markdown", Query: []Param{{"version", "integer", ""}}, Handler: a.downloadArtifact})

	// ---------- Context packs ----------
	a.add(Route{Method: "GET", Path: "/v1/context-packs", Tag: "context-packs", Summary: "List context packs", Auth: true, Resp: []domain.ContextPack{}, Query: []Param{{"idea_id", "uuid", ""}}, Handler: a.listPacks})
	a.add(Route{Method: "POST", Path: "/v1/context-packs", Tag: "context-packs", Summary: "Build a context pack", Auth: true, Req: service.BuildContextPackInput{}, Resp: domain.ContextPack{}, Handler: a.buildPack})
	a.add(Route{Method: "GET", Path: "/v1/context-packs/{id}", Tag: "context-packs", Summary: "Get a context pack", Auth: true, Resp: domain.ContextPack{}, Handler: a.getPack})
	a.add(Route{Method: "GET", Path: "/v1/context-packs/{id}/export", Tag: "context-packs", Summary: "Export as Markdown or JSON", Auth: true, Raw: "text/markdown", Query: []Param{{"format", "string", "md|json"}}, Handler: a.exportPack})

	// ---------- External AI delta ----------
	a.add(Route{Method: "POST", Path: "/v1/deltas", Tag: "deltas", Summary: "Analyze an external AI conversation against a branch", Auth: true, Req: service.AnalyzeDeltaInput{}, Resp: domain.Delta{}, Limit: "agent", Handler: a.analyzeDelta})
	a.add(Route{Method: "GET", Path: "/v1/deltas", Tag: "deltas", Summary: "List deltas", Auth: true, Resp: []domain.Delta{}, Query: []Param{{"idea_id", "uuid", ""}}, Handler: a.listDeltas})
	a.add(Route{Method: "GET", Path: "/v1/deltas/{id}", Tag: "deltas", Summary: "Get a delta with its items", Auth: true, Resp: domain.Delta{}, Handler: a.getDelta})
	a.add(Route{Method: "POST", Path: "/v1/deltas/{id}/merge", Tag: "deltas", Summary: "Merge selected changes (never overwrites history)", Auth: true, Req: mergeDeltaReq{}, Resp: service.MergeDeltaResult{}, Handler: a.mergeDelta})
	a.add(Route{Method: "POST", Path: "/v1/deltas/{id}/discard", Tag: "deltas", Summary: "Discard a delta", Auth: true, Resp: domain.Delta{}, Handler: a.discardDelta})

	// ---------- Imports ----------
	a.add(Route{Method: "GET", Path: "/v1/imports/adapters", Tag: "imports", Summary: "Versioned import adapters", Auth: true, Resp: []adapterInfo{}, Handler: a.adapters})
	a.add(Route{Method: "GET", Path: "/v1/imports", Tag: "imports", Summary: "List imports", Auth: true, Resp: []domain.Import{}, Handler: a.listImports})
	a.add(Route{Method: "POST", Path: "/v1/imports", Tag: "imports", Summary: "Start an import (multipart file upload, or JSON paste/url)", Auth: true, Req: importCreateReq{}, Resp: domain.Import{}, Limit: "import", Handler: a.createImport})
	a.add(Route{Method: "GET", Path: "/v1/imports/{id}", Tag: "imports", Summary: "Import status, progress and preview items", Auth: true, Resp: service.ImportDetail{}, Handler: a.getImport})
	a.add(Route{Method: "POST", Path: "/v1/imports/{id}/commit", Tag: "imports", Summary: "Commit selected conversations", Auth: true, Req: service.CommitImportInput{}, Resp: domain.Import{}, Limit: "import", Handler: a.commitImport})

	// ---------- Search ----------
	a.add(Route{Method: "GET", Path: "/v1/search", Tag: "search", Summary: "Natural-language hybrid search (full-text + semantic)", Auth: true, Resp: service.SearchResponse{},
		Query:   []Param{{"q", "string", "query"}, {"types", "string", "comma-separated entity types"}, {"idea_id", "uuid", ""}, {"mode", "string", "hybrid|fts|semantic"}, {"order", "string", "relevance|oldest|newest"}, {"limit", "integer", ""}},
		Handler: a.search})

	// ---------- Analysis ----------
	a.add(Route{Method: "POST", Path: "/v1/analysis/prompt", Tag: "analysis", Summary: "Analyze prompt quality", Auth: true, Req: promptReq{}, Resp: service.PromptAnalysis{}, Limit: "agent", Handler: a.analyzePrompt})
	a.add(Route{Method: "POST", Path: "/v1/analysis/thinking", Tag: "analysis", Summary: "Analyze observable thinking patterns", Auth: true, Req: thinkingReq{}, Resp: service.ThinkingAnalysis{}, Limit: "agent", Handler: a.analyzeThinking})
	a.add(Route{Method: "GET", Path: "/v1/analysis", Tag: "analysis", Summary: "Past analyses", Auth: true, Resp: []domain.Analysis{}, Query: []Param{{"kind", "string", ""}, {"idea_id", "uuid", ""}}, Handler: a.listAnalyses})
}

// ---------- agent ----------

func (a *API) chat(w http.ResponseWriter, r *http.Request) {
	var in agent.ChatRequest
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	user := uid(r)
	a.stream(w, r, func(ctx context.Context, emit agent.Emitter) error {
		_, err := a.sup.Chat(ctx, user, in, emit)
		return err
	})
}

func (a *API) confirmTool(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in confirmReq
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	user := uid(r)
	a.stream(w, r, func(ctx context.Context, emit agent.Emitter) error {
		_, err := a.sup.Resume(ctx, user, id, in.Approve, emit)
		return err
	})
}

func (a *API) pending(w http.ResponseWriter, r *http.Request) {
	convID, err := queryUUID(r, "conversation_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var p []domain.ToolCallRecord
	if convID != nil {
		p, err = a.svc.Store().PendingConfirmationsInConversation(r.Context(), uid(r), *convID)
	} else {
		p, err = a.svc.Store().PendingConfirmations(r.Context(), uid(r))
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, nonNilSlice(p))
}

func (a *API) getRun(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	run, err := a.svc.Store().GetAgentRun(r.Context(), uid(r), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, run)
}

func (a *API) toolCatalog(w http.ResponseWriter, r *http.Request) {
	pol := agent.Policy{}
	var out []toolInfo
	for _, t := range a.sup.Tools() {
		out = append(out, toolInfo{Name: t.Name, Description: t.Description, Category: t.Category, Parameters: t.Params, Permission: string(pol.Decide(t.Name, t.Category))})
	}
	writeJSON(w, 200, out)
}

func (a *API) getPolicy(w http.ResponseWriter, r *http.Request) {
	st, err := a.svc.Store().GetSettings(r.Context(), uid(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var p agent.Policy
	if raw, ok := st["agent_policy"]; ok {
		b, _ := json.Marshal(raw)
		_ = json.Unmarshal(b, &p)
	}
	writeJSON(w, 200, p)
}

func (a *API) putPolicy(w http.ResponseWriter, r *http.Request) {
	var p agent.Policy
	if err := decodeJSON(w, r, &p); err != nil {
		writeError(w, r, err)
		return
	}
	if _, err := a.svc.Store().MergeSettings(r.Context(), uid(r), map[string]any{"agent_policy": p}); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, p)
}

// ---------- artifacts ----------

func (a *API) templates(w http.ResponseWriter, _ *http.Request) {
	var out []artifacts.Template
	for _, t := range domain.AllArtifactTypes {
		out = append(out, artifacts.Get(t))
	}
	writeJSON(w, 200, out)
}

func (a *API) listArtifacts(w http.ResponseWriter, r *http.Request) {
	f := postgres.ArtifactFilter{UserID: uid(r), Type: domain.ArtifactType(strings.ToUpper(r.URL.Query().Get("type"))), Status: domain.ArtifactStatus(strings.ToUpper(r.URL.Query().Get("status"))), Query: r.URL.Query().Get("q")}
	var err error
	if f.IdeaID, err = queryUUID(r, "idea_id"); err != nil {
		writeError(w, r, err)
		return
	}
	arts, err := a.svc.Store().ListArtifacts(r.Context(), f)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, nonNilSlice(arts))
}

func (a *API) createArtifact(w http.ResponseWriter, r *http.Request) {
	var in service.CreateArtifactInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	res, err := a.svc.CreateArtifact(r.Context(), actor(r), in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 201, res)
}

func (a *API) generateArtifact(w http.ResponseWriter, r *http.Request) {
	var in service.GenerateArtifactInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	res, err := a.svc.GenerateArtifact(r.Context(), actor(r), in, nil)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 201, res)
}

func (a *API) getArtifact(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	art, err := a.svc.Store().GetArtifact(r.Context(), uid(r), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, art)
}

func (a *API) updateArtifact(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in service.UpdateArtifactInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	res, err := a.svc.UpdateArtifact(r.Context(), actor(r), id, in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, res)
}

func (a *API) deleteArtifact(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	if r.Header.Get("X-Confirm") != "delete" {
		writeError(w, r, domain.Invalid("confirm", "destructive: send header X-Confirm: delete"))
		return
	}
	if err := a.svc.DeleteArtifact(r.Context(), actor(r), id); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, okResp{OK: true})
}

func (a *API) regenerateArtifact(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in service.RegenerateArtifactInput
	_ = decodeJSON(w, r, &in)
	res, err := a.svc.RegenerateArtifact(r.Context(), actor(r), id, in, nil)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, res)
}

func (a *API) artifactVersions(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	v, err := a.svc.Store().ListArtifactVersions(r.Context(), uid(r), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, nonNilSlice(v))
}

func (a *API) artifactProvenance(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	p, err := a.svc.Store().ArtifactProvenance(r.Context(), uid(r), id, queryInt(r, "version", 0))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, nonNilSlice(p))
}

func (a *API) explainArtifact(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in explainReq
	_ = decodeJSON(w, r, &in)
	ex, err := a.svc.ExplainArtifact(r.Context(), uid(r), id, in.Question, in.Version)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, ex)
}

var unsafeFilename = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func attachmentName(title, ext string) string {
	name := strings.Trim(unsafeFilename.ReplaceAllString(title, "-"), "-.")
	if name == "" {
		name = "ideavault"
	}
	if len(name) > 80 {
		name = name[:80]
	}
	return name + ext
}

func (a *API) downloadArtifact(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	st := a.svc.Store()
	art, err := st.GetArtifact(r.Context(), uid(r), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	content, title := art.ContentMarkdown, art.Title
	if v := queryInt(r, "version", 0); v > 0 && v != art.CurrentVersion {
		vs, err := st.ListArtifactVersions(r.Context(), uid(r), id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		found := false
		for _, x := range vs {
			if x.Version == v {
				content, title, found = x.ContentMarkdown, x.Title+fmt.Sprintf(" v%d", v), true
			}
		}
		if !found {
			writeError(w, r, domain.NotFound("artifact version"))
			return
		}
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, attachmentName(title, ".md")))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, content)
}

// ---------- context packs ----------

func (a *API) listPacks(w http.ResponseWriter, r *http.Request) {
	ideaID, err := queryUUID(r, "idea_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	p, err := a.svc.Store().ListContextPacks(r.Context(), uid(r), ideaID, 50)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, nonNilSlice(p))
}

func (a *API) buildPack(w http.ResponseWriter, r *http.Request) {
	var in service.BuildContextPackInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	p, err := a.svc.BuildContextPack(r.Context(), actor(r), in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 201, p)
}

func (a *API) getPack(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	p, err := a.svc.Store().GetContextPack(r.Context(), uid(r), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, p)
}

func (a *API) exportPack(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	p, err := a.svc.Store().GetContextPack(r.Context(), uid(r), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if r.URL.Query().Get("format") == "json" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, attachmentName(p.Title, ".json")))
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(p.ContentJSON)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, attachmentName(p.Title, ".md")))
	_, _ = io.WriteString(w, p.ContentMarkdown)
}

// ---------- deltas ----------

func (a *API) analyzeDelta(w http.ResponseWriter, r *http.Request) {
	var in service.AnalyzeDeltaInput
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		data, name, fields, err := a.readUpload(w, r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		in.Data, in.Filename, in.Provider = data, name, fields["provider"]
		if id, err := uuid.Parse(fields["idea_id"]); err == nil {
			in.IdeaID = id
		}
		if id, err := uuid.Parse(fields["branch_id"]); err == nil {
			in.BranchID = &id
		}
	} else if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	d, err := a.svc.AnalyzeDelta(r.Context(), actor(r), in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 201, d)
}

func (a *API) listDeltas(w http.ResponseWriter, r *http.Request) {
	ideaID, err := queryUUID(r, "idea_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	d, err := a.svc.Store().ListDeltas(r.Context(), uid(r), ideaID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, nonNilSlice(d))
}

func (a *API) getDelta(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	d, err := a.svc.Store().GetDelta(r.Context(), uid(r), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, d)
}

func (a *API) mergeDelta(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in mergeDeltaReq
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	res, err := a.svc.MergeDelta(r.Context(), actor(r), id, service.MergeDeltaInput{ItemIDs: in.ItemIDs})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, res)
}

func (a *API) discardDelta(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	d, err := a.svc.DiscardDelta(r.Context(), actor(r), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, d)
}

// ---------- imports ----------

func (a *API) adapters(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, []adapterInfo{
		{"chatgpt/v1", "chatgpt"}, {"chatgpt/v2", "chatgpt"}, {"claude/v1", "claude"}, {"gemini/v1", "gemini"},
		{"markdown/v1", "markdown"}, {"text/v1", "text"}, {"json/v1", "json"}, {"web/v1", "web"},
	})
}

func (a *API) listImports(w http.ResponseWriter, r *http.Request) {
	im, err := a.svc.Store().ListImports(r.Context(), uid(r), 50)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, nonNilSlice(im))
}

// readUpload reads a multipart "file" field (bounded) and other form fields.
func (a *API) readUpload(w http.ResponseWriter, r *http.Request) ([]byte, string, map[string]string, error) {
	limit := int64(200 << 20)
	if a.cfg != nil && a.cfg.MaxUploadBytes > 0 {
		limit = a.cfg.MaxUploadBytes
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit+(1<<20))
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, "", nil, domain.Invalid("file", "expected multipart/form-data")
	}
	fields := map[string]string{}
	var data []byte
	var name string
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", nil, domain.Invalid("file", "upload failed or exceeds the size limit")
		}
		if part.FormName() == "file" {
			name = part.FileName()
			data, err = io.ReadAll(io.LimitReader(part, limit+1))
			if err != nil {
				return nil, "", nil, domain.Invalid("file", "upload failed or exceeds the size limit")
			}
			if int64(len(data)) > limit {
				return nil, "", nil, domain.Invalid("file", fmt.Sprintf("file exceeds the %d MB limit", limit>>20))
			}
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(part, 1<<20))
		fields[part.FormName()] = string(b)
	}
	if len(data) == 0 && fields["text"] == "" {
		return nil, "", nil, domain.Invalid("file", "no file uploaded")
	}
	if len(data) == 0 {
		data = []byte(fields["text"])
	}
	return data, name, fields, nil
}

func (a *API) createImport(w http.ResponseWriter, r *http.Request) {
	var req importCreateReq
	in := service.CreateImportInput{}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		data, name, fields, err := a.readUpload(w, r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		in.SourceKind, in.Data, in.Filename, in.Adapter = "file", data, name, fields["adapter"]
		if id, err := uuid.Parse(fields["target_idea_id"]); err == nil {
			in.TargetIdeaID = &id
		}
		req.Sync = fields["sync"] == "true"
		req.NewIdea = fields["new_idea"] == "true"
		req.Extract = fields["extract"] == "true"
	} else {
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, r, err)
			return
		}
		in = service.CreateImportInput{SourceKind: req.SourceKind, Text: req.Text, URL: req.URL, Filename: req.Filename, Adapter: req.Adapter, TargetIdeaID: req.TargetIdeaID}
	}
	if req.Sync {
		res, err := a.svc.ImportNow(r.Context(), actor(r), service.ImportNowInput{CreateImportInput: in, IdeaID: in.TargetIdeaID, NewIdea: req.NewIdea, Extract: req.Extract})
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 201, res.Import)
		return
	}
	im, err := a.svc.CreateImport(r.Context(), actor(r), in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if a.notify != nil {
		a.notify()
	}
	writeJSON(w, 202, im)
}

func (a *API) getImport(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	d, err := a.svc.GetImport(r.Context(), uid(r), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, d)
}

func (a *API) commitImport(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in service.CommitImportInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	im, err := a.svc.CommitImport(r.Context(), actor(r), id, in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if a.notify != nil {
		a.notify()
	}
	writeJSON(w, 202, im)
}

// ---------- search & analysis ----------

func (a *API) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	req := service.SearchRequest{Query: q.Get("q"), Mode: q.Get("mode"), Order: q.Get("order"), Limit: queryInt(r, "limit", 30)}
	for _, t := range strings.Split(q.Get("types"), ",") {
		if et := domain.EntityType(strings.TrimSpace(t)); et.Valid() {
			req.Types = append(req.Types, et)
		}
	}
	var err error
	if req.IdeaID, err = queryUUID(r, "idea_id"); err != nil {
		writeError(w, r, err)
		return
	}
	res, err := a.svc.Search(r.Context(), uid(r), req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if res.Results == nil {
		res.Results = []postgres.SearchHit{}
	}
	writeJSON(w, 200, res)
}

func (a *API) analyzePrompt(w http.ResponseWriter, r *http.Request) {
	var in promptReq
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	pa, err := a.svc.AnalyzePrompt(r.Context(), actor(r), in.Text, in.MessageID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, pa)
}

func (a *API) analyzeThinking(w http.ResponseWriter, r *http.Request) {
	var in thinkingReq
	_ = decodeJSON(w, r, &in)
	ta, err := a.svc.AnalyzeThinking(r.Context(), actor(r), in.IdeaID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, ta)
}

func (a *API) listAnalyses(w http.ResponseWriter, r *http.Request) {
	ideaID, err := queryUUID(r, "idea_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	an, err := a.svc.Store().ListAnalyses(r.Context(), uid(r), r.URL.Query().Get("kind"), ideaID, 30)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, nonNilSlice(an))
}

func (a *API) evolution(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	ev, err := a.svc.AnalyzeEvolution(r.Context(), actor(r), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, ev)
}

func (a *API) contradictions(w http.ResponseWriter, r *http.Request) {
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
	fs, analyzer, err := a.svc.DetectContradictions(r.Context(), actor(r), id, bid)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, contradictionsResp{Findings: nonNilSlice(fs), Analyzer: analyzer})
}
