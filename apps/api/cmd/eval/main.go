// Command eval runs IdeaVault's evaluations against real AI providers. It is never
// run by `go test`.
//
// Model benchmarks (no database needed) — every task in internal/evals/benchmarks.go
// on every listed model, scored for JSON validity, must_contain / must_not_contain,
// token F1 against the reference, latency, tokens and cost:
//
//	EVAL_MODELS=groq/openai/gpt-oss-20b,openai/gpt-5-mini GROQ_API_KEY=… OPENAI_API_KEY=… go run ./cmd/eval
//
// Agent cases (tests/evaluations/agent/cases.json) on one real model. A temporary
// database is created next to TEST_DATABASE_URL and dropped afterwards:
//
//	EVAL_MODEL=anthropic/claude-haiku-5-5 ANTHROPIC_API_KEY=… \
//	TEST_DATABASE_URL=postgres://ideavault:ideavault@localhost:5442/ideavault_test?sslmode=disable go run ./cmd/eval
//
// "mock/offline-planner" works in both modes without keys (pipeline smoke test).
// Reports are written to tests/evaluations/reports/ (models-<ts>.json, agent-<model>-<ts>.json).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/agent"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/app"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/config"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/db"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/evals"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/evals/agenteval"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/models"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/observability"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		os.Exit(1)
	}
}

func run() error {
	casesPath := flag.String("cases", "", "agent case file (default tests/evaluations/agent/cases.json)")
	outDir := flag.String("out", "", "report directory (default tests/evaluations/reports)")
	only := flag.String("tasks", "", "comma-separated benchmark task ids or agent case ids to run (default all)")
	timeout := flag.Duration("timeout", 3*time.Minute, "per model call / agent case timeout")
	flag.Parse()

	evalModel := strings.TrimSpace(os.Getenv("EVAL_MODEL"))
	evalModels := splitList(os.Getenv("EVAL_MODELS"))
	if evalModel == "" && len(evalModels) == 0 {
		fmt.Fprintln(os.Stderr, "usage: set EVAL_MODELS=provider/model[,…] for model benchmarks and/or EVAL_MODEL=provider/model (+ TEST_DATABASE_URL) for agent cases")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	dir := *outDir
	if dir == "" {
		root, err := agenteval.RepoRoot()
		if err != nil {
			return err
		}
		dir = filepath.Join(root, "tests", "evaluations", "reports")
	}
	filter := map[string]bool{}
	for _, id := range splitList(*only) {
		filter[id] = true
	}
	if len(evalModels) > 0 {
		if err := runBenchmarks(ctx, evalModels, filter, *timeout, dir); err != nil {
			return err
		}
	}
	if evalModel != "" {
		if err := runAgentCases(ctx, evalModel, *casesPath, filter, *timeout, dir); err != nil {
			return err
		}
	}
	return nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func stamp() string { return time.Now().UTC().Format("20060102T150405Z") }

// envCredentials maps provider ids to credentials from the environment.
func envCredentials(cfg *config.Config) map[string]models.ProviderCredentials {
	return map[string]models.ProviderCredentials{
		models.ProviderGroq:      {APIKey: cfg.GroqKey},
		models.ProviderOpenAI:    {APIKey: cfg.OpenAIKey, BaseURL: cfg.OpenAIBaseURL},
		models.ProviderAnthropic: {APIKey: cfg.AnthropicKey},
		models.ProviderGemini:    {APIKey: cfg.GeminiKey},
		models.ProviderOllama:    {BaseURL: cfg.OllamaBaseURL},
	}
}

var keyEnv = map[string]string{models.ProviderGroq: "GROQ_API_KEY", models.ProviderOpenAI: "OPENAI_API_KEY", models.ProviderAnthropic: "ANTHROPIC_API_KEY",
	models.ProviderGemini: "GEMINI_API_KEY", models.ProviderOllama: "OLLAMA_BASE_URL"}

func splitKey(key string) (provider, model string, err error) {
	i := strings.Index(key, "/")
	if i <= 0 || i == len(key)-1 {
		return "", "", fmt.Errorf("model %q must look like provider/model", key)
	}
	return key[:i], key[i+1:], nil
}

// ---------- model benchmarks ----------

type modelReport struct {
	GeneratedAt time.Time               `json:"generated_at"`
	Models      []string                `json:"models"`
	Tasks       []string                `json:"tasks"`
	Summary     []evals.ModelSummary    `json:"summary"`
	Results     []evals.BenchmarkResult `json:"results"`
}

func runBenchmarks(ctx context.Context, keys []string, filter map[string]bool, timeout time.Duration, dir string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := observability.NewLogger(os.Stderr, "warn", "text")
	gw := models.NewGateway(app.ProviderFactory, nil, log)
	gw.RegisterProvider(agent.NewOfflinePlanner())
	creds := envCredentials(cfg)
	catalog := append([]domain.ModelConfig(nil), models.BuiltinModels...)
	known := map[string]bool{}
	for _, m := range catalog {
		known[m.Provider+"/"+m.Model] = true
	}
	for _, key := range keys {
		provider, model, err := splitKey(key)
		if err != nil {
			return err
		}
		if provider != models.ProviderMock {
			c, ok := creds[provider]
			if !ok {
				return fmt.Errorf("unknown provider %q in %s", provider, key)
			}
			if c.APIKey == "" && !(provider == models.ProviderOllama && c.BaseURL != "") {
				return fmt.Errorf("%s needs %s", key, keyEnv[provider])
			}
			if err := gw.ConfigureProvider(provider, c); err != nil {
				return fmt.Errorf("configure %s: %w", provider, err)
			}
		}
		if !known[key] {
			// Models outside the built-in catalog are benchmarked with zero pricing.
			catalog = append(catalog, domain.ModelConfig{Provider: provider, Model: model, DisplayName: key, ContextLength: 128000, Quality: 3, Speed: 3, Enabled: true})
			known[key] = true
		}
	}
	gw.SetModels(catalog)
	var tasks []evals.BenchmarkTask
	var ids []string
	for _, b := range evals.Benchmarks {
		if len(filter) == 0 || filter[b.ID] {
			tasks = append(tasks, b)
			ids = append(ids, b.ID)
		}
	}
	if len(tasks) == 0 {
		return errors.New("no benchmark task matches -tasks")
	}
	fmt.Printf("Running %d benchmark task(s) on %d model(s)…\n", len(tasks), len(keys))
	results := evals.RunBenchmarks(ctx, gw, keys, tasks, timeout)
	summary := evals.Summarize(results)
	fmt.Print(evals.FormatTable(summary))
	for _, r := range results {
		if r.Error != "" {
			fmt.Printf("  error %s on %s: %s\n", r.TaskID, r.Model, observability.RedactString(r.Error))
		} else if !r.Score.Passed {
			fmt.Printf("  fail  %s on %s: missing=%v forbidden=%v schema=%v\n", r.TaskID, r.Model, r.Score.Missing, r.Score.Forbidden, r.Score.SchemaErrors)
		}
	}
	rep := modelReport{GeneratedAt: time.Now().UTC(), Models: keys, Tasks: ids, Summary: summary, Results: results}
	path := filepath.Join(dir, "models-"+stamp()+".json")
	if err := writeJSON(path, rep); err != nil {
		return err
	}
	fmt.Println("report:", path)
	return nil
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// ---------- agent cases on a real model ----------

func runAgentCases(ctx context.Context, key, casesPath string, filter map[string]bool, timeout time.Duration, dir string) error {
	provider, _, err := splitKey(key)
	if err != nil {
		return err
	}
	base := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if base == "" {
		return errors.New("agent evaluation needs TEST_DATABASE_URL (a temporary database is created next to it)")
	}
	if casesPath == "" {
		if casesPath, err = agenteval.DefaultSuitePath(); err != nil {
			return err
		}
	}
	suite, err := agenteval.LoadSuite(casesPath)
	if err != nil {
		return err
	}
	if len(filter) > 0 {
		var keep []agenteval.Case
		for _, c := range suite.Cases {
			if filter[c.ID] {
				keep = append(keep, c)
			}
		}
		suite.Cases = keep
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if provider != models.ProviderMock {
		c := envCredentials(cfg)[provider]
		if c.APIKey == "" && !(provider == models.ProviderOllama && c.BaseURL != "") {
			return fmt.Errorf("%s needs %s", key, keyEnv[provider])
		}
	}
	dbURL, cleanup, err := tempDatabase(ctx, base)
	if err != nil {
		return err
	}
	defer cleanup()
	cfg.Env = "test"
	cfg.DatabaseURL = dbURL
	cfg.AutoMigrate = true
	cfg.RedisURL = ""
	cfg.BootstrapEmail, cfg.BootstrapPass = "", ""
	cfg.MockAI = provider == models.ProviderMock
	if os.Getenv("EVAL_EMBEDDINGS") == "" {
		cfg.EmbeddingProvider = "local" // keep retrieval free and deterministic unless asked otherwise
	}
	log := observability.NewLogger(os.Stderr, "warn", "text")
	a, err := app.Build(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer a.Close()
	found := false
	for _, m := range a.Gateway.Models() {
		if m.Provider+"/"+m.Model == key {
			found = m.Available
		}
	}
	if !found {
		return fmt.Errorf("%s is not an available model (check the key and the model registry)", key)
	}
	fmt.Printf("Running %d agent case(s) on %s…\n", len(suite.Cases), key)
	r := &agenteval.Runner{App: a, Pin: key, Strict: provider == models.ProviderMock, Timeout: timeout}
	rep := r.Run(ctx, suite)
	rep.Model = key
	fmt.Print(rep.Summary())
	name := regexp.MustCompile(`[^A-Za-z0-9._-]+`).ReplaceAllString(key, "_")
	path := filepath.Join(dir, "agent-"+name+"-"+stamp()+".json")
	if err := agenteval.WriteReport(path, rep); err != nil {
		return err
	}
	fmt.Println("report:", path)
	return nil
}

// tempDatabase creates a uniquely named database on the server of baseURL and returns
// its URL and a cleanup function that drops it.
func tempDatabase(ctx context.Context, baseURL string) (string, func(), error) {
	admin, err := db.Connect(ctx, baseURL)
	if err != nil {
		return "", nil, err
	}
	name := fmt.Sprintf("ideavault_eval_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		admin.Close()
		return "", nil, fmt.Errorf("create temporary database: %w", err)
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		admin.Close()
		return "", nil, err
	}
	u.Path = "/" + name
	cleanup := func() {
		cctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := admin.Exec(cctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			fmt.Fprintln(os.Stderr, "eval: drop temporary database:", err)
		}
		admin.Close()
	}
	return u.String(), cleanup, nil
}
