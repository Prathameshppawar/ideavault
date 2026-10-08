package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/models"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/repository/postgres"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/service"
)

type modelPatch struct {
	Enabled *bool `json:"enabled,omitempty"`
}

type providerKeyReq struct {
	APIKey  string `json:"api_key,omitempty"`
	BaseURL string `json:"base_url,omitempty"`
}

type availableModels struct {
	Provider string   `json:"provider"`
	Models   []string `json:"models"`
}

type systemStatus struct {
	Version     string                   `json:"version"`
	Env         string                   `json:"env"`
	Providers   []service.ProviderStatus `json:"providers"`
	Embedder    map[string]any           `json:"embedder"`
	Jobs        map[string]int           `json:"jobs"`
	Health      map[string]any           `json:"health"`
	OfflineMode bool                     `json:"offline_mode"`
	Coverage    map[string]int           `json:"embedding_coverage"`
}

type backfillResp struct {
	Queued bool `json:"queued"`
}

func (a *API) registerPlatform() {
	// ---------- Dashboard ----------
	a.add(Route{Method: "GET", Path: "/v1/dashboard/today", Tag: "dashboard", Summary: "Thinking / Today: open loops and recent thinking", Auth: true, Resp: service.TodayView{}, Handler: a.today})
	a.add(Route{Method: "GET", Path: "/v1/dashboard/timeline", Tag: "dashboard", Summary: "Thinking activity over time", Auth: true, Resp: service.TimelineView{}, Query: []Param{{"days", "integer", ""}, {"idea_id", "uuid", ""}}, Handler: a.timeline})
	a.add(Route{Method: "GET", Path: "/v1/dashboard/decisions", Tag: "dashboard", Summary: "Decision history and reversals", Auth: true, Resp: service.DecisionsView{}, Query: []Param{{"idea_id", "uuid", ""}}, Handler: a.decisions})
	a.add(Route{Method: "GET", Path: "/v1/dashboard/learning", Tag: "dashboard", Summary: "Insights and lessons", Auth: true, Resp: service.LearningView{}, Handler: a.learning})
	a.add(Route{Method: "GET", Path: "/v1/dashboard/momentum", Tag: "dashboard", Summary: "Ideas changing rapidly", Auth: true, Resp: []postgres.IdeaMomentum{}, Query: []Param{{"days", "integer", ""}}, Handler: a.momentum})
	a.add(Route{Method: "GET", Path: "/v1/dashboard/archive", Tag: "dashboard", Summary: "Parked, abandoned, concluded and merged ideas", Auth: true, Resp: []domain.Idea{}, Handler: a.archive})
	a.add(Route{Method: "GET", Path: "/v1/graph", Tag: "dashboard", Summary: "Universe / knowledge graph", Auth: true, Resp: service.Graph{},
		Query: []Param{{"idea_id", "uuid", ""}, {"branch_id", "uuid", ""}, {"conversations", "boolean", ""}, {"artifacts", "boolean", ""}, {"history", "boolean", ""}}, Handler: a.graph})
	a.add(Route{Method: "GET", Path: "/v1/activity", Tag: "dashboard", Summary: "Activity log", Auth: true, Resp: []domain.ActivityEvent{}, Query: []Param{{"idea_id", "uuid", ""}, {"limit", "integer", ""}}, Handler: a.activity})

	// ---------- Models ----------
	a.add(Route{Method: "GET", Path: "/v1/models", Tag: "models", Summary: "Model registry with availability", Auth: true, Resp: []domain.ModelConfig{}, Handler: a.listModels})
	a.add(Route{Method: "POST", Path: "/v1/models", Tag: "models", Summary: "Add or update a model config", Auth: true, Req: domain.ModelConfig{}, Resp: domain.ModelConfig{}, Handler: a.upsertModel})
	a.add(Route{Method: "PATCH", Path: "/v1/models/{id}", Tag: "models", Summary: "Enable/disable a model", Auth: true, Req: modelPatch{}, Resp: okResp{}, Handler: a.patchModel})
	a.add(Route{Method: "DELETE", Path: "/v1/models/{id}", Tag: "models", Summary: "Delete a custom model", Auth: true, Resp: okResp{}, Handler: a.deleteModel})
	a.add(Route{Method: "GET", Path: "/v1/models/providers", Tag: "models", Summary: "AI providers and configuration state", Auth: true, Resp: []service.ProviderStatus{}, Handler: a.providers})
	a.add(Route{Method: "PUT", Path: "/v1/models/providers/{provider}", Tag: "models", Summary: "Store (encrypted) and verify a provider key", Auth: true, Req: providerKeyReq{}, Resp: service.ProviderStatus{}, Limit: "auth", Handler: a.setProvider})
	a.add(Route{Method: "DELETE", Path: "/v1/models/providers/{provider}", Tag: "models", Summary: "Remove stored provider credentials", Auth: true, Resp: okResp{}, Handler: a.removeProvider})
	a.add(Route{Method: "GET", Path: "/v1/models/providers/{provider}/available", Tag: "models", Summary: "Models offered by the provider", Auth: true, Resp: availableModels{}, Handler: a.availableModels})
	a.add(Route{Method: "GET", Path: "/v1/models/routing", Tag: "models", Summary: "Routing table: task → ranked models", Auth: true, Resp: []service.RoutingPreview{}, Handler: a.routing})
	a.add(Route{Method: "PUT", Path: "/v1/models/routing", Tag: "models", Summary: "Update routing preferences", Auth: true, Req: models.RoutingPolicy{}, Resp: models.RoutingPolicy{}, Handler: a.setRouting})
	a.add(Route{Method: "GET", Path: "/v1/models/lab", Tag: "models", Summary: "Model Lab runs", Auth: true, Resp: []postgres.LabRun{}, Handler: a.labRuns})
	a.add(Route{Method: "POST", Path: "/v1/models/lab", Tag: "models", Summary: "Run a task across models and compare", Auth: true, Req: service.LabInput{}, Resp: postgres.LabRun{}, Limit: "agent", Handler: a.runLab})
	a.add(Route{Method: "GET", Path: "/v1/models/lab/{id}", Tag: "models", Summary: "Model Lab run with results", Auth: true, Resp: postgres.LabRun{}, Handler: a.labRun})
	a.add(Route{Method: "GET", Path: "/v1/models/benchmarks", Tag: "models", Summary: "Built-in IdeaVault benchmark tasks", Auth: true, Resp: BenchmarkTasks, Handler: a.benchmarks})

	// ---------- Usage ----------
	a.add(Route{Method: "GET", Path: "/v1/usage", Tag: "usage", Summary: "Model and connector usage analytics", Auth: true, Resp: service.UsageSummary{},
		Query: []Param{{"days", "integer", ""}, {"provider", "string", ""}, {"model", "string", ""}, {"task", "string", ""}, {"idea_id", "uuid", ""}}, Handler: a.usage})

	// ---------- Connectors ----------
	a.add(Route{Method: "GET", Path: "/v1/connectors", Tag: "connectors", Summary: "Connectors with honest status", Auth: true, Resp: []service.ConnectorView{}, Handler: a.listConnectors})
	a.add(Route{Method: "POST", Path: "/v1/connectors/{key}/connect", Tag: "connectors", Summary: "Verify and connect (credentials encrypted at rest)", Auth: true, Req: service.ConnectInput{}, Resp: service.ConnectorView{}, Limit: "auth", Handler: a.connect})
	a.add(Route{Method: "POST", Path: "/v1/connectors/{key}/disconnect", Tag: "connectors", Summary: "Disconnect and delete credentials", Auth: true, Resp: okResp{}, Handler: a.disconnect})
	a.add(Route{Method: "GET", Path: "/v1/connectors/events", Tag: "connectors", Summary: "Connector usage events", Auth: true, Resp: []domain.ConnectorEvent{}, Query: []Param{{"connector", "string", ""}, {"days", "integer", ""}}, Handler: a.connectorEvents})

	// ---------- Settings & system ----------
	a.add(Route{Method: "GET", Path: "/v1/settings", Tag: "settings", Summary: "User settings", Auth: true, Resp: map[string]any{}, Handler: a.getSettings})
	a.add(Route{Method: "PATCH", Path: "/v1/settings", Tag: "settings", Summary: "Merge user settings", Auth: true, Req: map[string]any{}, Resp: map[string]any{}, Handler: a.patchSettings})
	a.add(Route{Method: "POST", Path: "/v1/embeddings/backfill", Tag: "settings", Summary: "Queue embedding backfill", Auth: true, Resp: backfillResp{}, Handler: a.backfill})
	a.add(Route{Method: "GET", Path: "/v1/system/status", Tag: "system", Summary: "System status (providers, embedder, jobs, health)", Auth: true, Resp: systemStatus{}, Handler: a.systemStatus})
}

func (a *API) today(w http.ResponseWriter, r *http.Request) {
	v, err := a.svc.Today(r.Context(), uid(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, v)
}

func (a *API) timeline(w http.ResponseWriter, r *http.Request) {
	ideaID, err := queryUUID(r, "idea_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	v, err := a.svc.Timeline(r.Context(), uid(r), ideaID, queryInt(r, "days", 90))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, v)
}

func (a *API) decisions(w http.ResponseWriter, r *http.Request) {
	ideaID, err := queryUUID(r, "idea_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	v, err := a.svc.Decisions(r.Context(), uid(r), ideaID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if v.Decisions == nil {
		v.Decisions = []service.DecisionEntry{}
	}
	writeJSON(w, 200, v)
}

func (a *API) learning(w http.ResponseWriter, r *http.Request) {
	v, err := a.svc.Learning(r.Context(), uid(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, v)
}

func (a *API) momentum(w http.ResponseWriter, r *http.Request) {
	v, err := a.svc.Momentum(r.Context(), uid(r), queryInt(r, "days", 7))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, nonNilSlice(v))
}

func (a *API) archive(w http.ResponseWriter, r *http.Request) {
	v, err := a.svc.Archive(r.Context(), uid(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, nonNilSlice(v))
}

func (a *API) graph(w http.ResponseWriter, r *http.Request) {
	f := service.GraphFilter{IncludeConversations: queryBool(r, "conversations"), IncludeArtifacts: queryBool(r, "artifacts"), IncludeHistory: queryBool(r, "history")}
	var err error
	if f.IdeaID, err = queryUUID(r, "idea_id"); err != nil {
		writeError(w, r, err)
		return
	}
	if f.BranchID, err = queryUUID(r, "branch_id"); err != nil {
		writeError(w, r, err)
		return
	}
	g, err := a.svc.BuildGraph(r.Context(), uid(r), f)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, g)
}

func (a *API) activity(w http.ResponseWriter, r *http.Request) {
	ideaID, err := queryUUID(r, "idea_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	ev, err := a.svc.Store().ListActivity(r.Context(), postgres.ActivityFilter{UserID: uid(r), IdeaID: ideaID, Limit: queryInt(r, "limit", 100)})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, nonNilSlice(ev))
}

// ---------- models ----------

func (a *API) listModels(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, nonNilSlice(a.svc.Gateway().Models()))
}

func (a *API) upsertModel(w http.ResponseWriter, r *http.Request) {
	var m domain.ModelConfig
	if err := decodeJSON(w, r, &m); err != nil {
		writeError(w, r, err)
		return
	}
	m.Provider = strings.ToLower(strings.TrimSpace(m.Provider))
	m.Model = strings.TrimSpace(m.Model)
	if m.Provider == "" || m.Model == "" {
		writeError(w, r, domain.Invalid("model", "provider and model are required"))
		return
	}
	if m.ContextLength <= 0 {
		m.ContextLength = 32000
	}
	clamp := func(v *int, lo, hi, def int) {
		if *v < lo || *v > hi {
			*v = def
		}
	}
	clamp(&m.Speed, 1, 5, 3)
	clamp(&m.Quality, 1, 5, 3)
	clamp(&m.RelativeCost, 0, 5, 2)
	if err := a.svc.Store().UpsertModel(r.Context(), &m); err != nil {
		writeError(w, r, err)
		return
	}
	_ = a.svc.RefreshModels(r.Context())
	writeJSON(w, 201, m)
}

func (a *API) patchModel(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in modelPatch
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	if in.Enabled != nil {
		if err := a.svc.Store().SetModelEnabled(r.Context(), id, *in.Enabled); err != nil {
			writeError(w, r, err)
			return
		}
	}
	_ = a.svc.RefreshModels(r.Context())
	writeJSON(w, 200, okResp{OK: true})
}

func (a *API) deleteModel(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := a.svc.Store().DeleteModel(r.Context(), id); err != nil {
		writeError(w, r, err)
		return
	}
	_ = a.svc.RefreshModels(r.Context())
	writeJSON(w, 200, okResp{OK: true})
}

func (a *API) providers(w http.ResponseWriter, r *http.Request) {
	p, err := a.svc.ProviderStatuses(r.Context(), uid(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, p)
}

func (a *API) setProvider(w http.ResponseWriter, r *http.Request) {
	var in providerKeyReq
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	st, err := a.svc.SetProviderCredentials(r.Context(), uid(r), chi.URLParam(r, "provider"), in.APIKey, in.BaseURL)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, st)
}

func (a *API) removeProvider(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.RemoveProviderCredentials(r.Context(), uid(r), chi.URLParam(r, "provider")); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, okResp{OK: true})
}

func (a *API) availableModels(w http.ResponseWriter, r *http.Request) {
	p := chi.URLParam(r, "provider")
	ids, err := a.svc.SyncProviderModels(r.Context(), p)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, availableModels{Provider: p, Models: nonNilSlice(ids)})
}

func (a *API) routing(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, a.svc.Routing())
}

func (a *API) setRouting(w http.ResponseWriter, r *http.Request) {
	var p models.RoutingPolicy
	if err := decodeJSON(w, r, &p); err != nil {
		writeError(w, r, err)
		return
	}
	out, err := a.svc.SetRoutingPolicy(r.Context(), uid(r), p)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}

func (a *API) labRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := a.svc.Store().ListLabRuns(r.Context(), uid(r), 30)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, nonNilSlice(runs))
}

func (a *API) runLab(w http.ResponseWriter, r *http.Request) {
	var in service.LabInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	run, err := a.svc.RunLab(r.Context(), uid(r), in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 201, run)
}

func (a *API) labRun(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	run, err := a.svc.Store().GetLabRun(r.Context(), uid(r), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, run)
}

func (a *API) benchmarks(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, BenchmarkTasks)
}

// ---------- usage ----------

func (a *API) usage(w http.ResponseWriter, r *http.Request) {
	days := queryInt(r, "days", 30)
	if days <= 0 || days > 365 {
		days = 30
	}
	f := postgres.UsageFilter{UserID: uid(r), Since: time.Now().AddDate(0, 0, -days), Provider: r.URL.Query().Get("provider"), Model: r.URL.Query().Get("model"), Task: r.URL.Query().Get("task"), Limit: 200}
	var err error
	if f.IdeaID, err = queryUUID(r, "idea_id"); err != nil {
		writeError(w, r, err)
		return
	}
	u, err := a.svc.Usage(r.Context(), f)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, u)
}

// ---------- connectors ----------

func (a *API) listConnectors(w http.ResponseWriter, r *http.Request) {
	c, err := a.svc.ListConnectors(r.Context(), uid(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, c)
}

func (a *API) connect(w http.ResponseWriter, r *http.Request) {
	var in service.ConnectInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	v, err := a.svc.ConnectConnector(r.Context(), uid(r), chi.URLParam(r, "key"), in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, v)
}

func (a *API) disconnect(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.DisconnectConnector(r.Context(), uid(r), chi.URLParam(r, "key")); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, okResp{OK: true})
}

func (a *API) connectorEvents(w http.ResponseWriter, r *http.Request) {
	days := queryInt(r, "days", 30)
	ev, err := a.svc.Store().ListConnectorEvents(r.Context(), uid(r), r.URL.Query().Get("connector"), time.Now().AddDate(0, 0, -days), 200)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, nonNilSlice(ev))
}

// ---------- settings/system ----------

func (a *API) getSettings(w http.ResponseWriter, r *http.Request) {
	s, err := a.svc.Store().GetSettings(r.Context(), uid(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, s)
}

func (a *API) patchSettings(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	for _, reserved := range []string{"routing", "agent_policy"} {
		delete(in, reserved) // managed by dedicated endpoints with validation
	}
	s, err := a.svc.Store().MergeSettings(r.Context(), uid(r), in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, s)
}

func (a *API) backfill(w http.ResponseWriter, r *http.Request) {
	u := uid(r)
	j, err := a.svc.Store().EnqueueJob(r.Context(), &u, service.JobEmbedBackfill, service.EmbedJob{UserID: u}, "backfill:"+u.String(), time.Time{})
	if err != nil {
		writeError(w, r, err)
		return
	}
	if a.notify != nil {
		a.notify()
	}
	writeJSON(w, 202, backfillResp{Queued: j != nil})
}

func (a *API) systemStatus(w http.ResponseWriter, r *http.Request) {
	st := systemStatus{Version: Version, Embedder: map[string]any{}}
	if a.cfg != nil {
		st.Env = a.cfg.Env
	}
	st.Providers, _ = a.svc.ProviderStatuses(r.Context(), uid(r))
	if e := a.svc.Gateway().Embedder(); e != nil {
		st.Embedder = map[string]any{"provider": e.ProviderID(), "model": e.Model(), "dims": models.EmbeddingDims}
		st.Coverage, _ = a.svc.Store().EmbeddingCoverage(r.Context(), uid(r), e.Model())
	}
	st.Jobs, _ = a.svc.Store().JobStats(r.Context())
	if a.health != nil {
		st.Health = a.health(r.Context())
	}
	st.OfflineMode = !a.svc.Gateway().HasRealModel(models.TaskSupervisor)
	writeJSON(w, 200, st)
}
