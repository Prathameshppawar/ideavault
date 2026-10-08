// Package connectors defines IdeaVault's connector registry. Connectors expose
// tools to the agent; their status is always real (verified), never assumed.
package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports"
)

// Tool is a connector tool exposed to the agent.
type Tool struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Category    domain.ToolCategory `json:"category"`
	Permission  string              `json:"permission"`
	Parameters  json.RawMessage     `json:"parameters"`
}

// Definition describes a connector.
type Definition struct {
	Key              string            `json:"key"`
	Name             string            `json:"name"`
	Description      string            `json:"description"`
	AuthType         string            `json:"auth_type"` // none | api_key | oauth2 | export
	Capabilities     []string          `json:"capabilities"`
	Permissions      []string          `json:"permissions"`
	Tools            []Tool            `json:"tools"`
	Implemented      bool              `json:"implemented"`
	SetupHint        string            `json:"setup_hint"`
	CredentialFields []CredentialField `json:"credential_fields,omitempty"`
}

// CredentialField describes an input needed to connect.
type CredentialField struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Secret   bool   `json:"secret"`
	Required bool   `json:"required"`
	Hint     string `json:"hint,omitempty"`
}

// Connector is an implemented integration.
type Connector interface {
	Definition() Definition
	// Verify checks credentials/connectivity.
	Verify(ctx context.Context, creds map[string]string) error
	// Invoke runs a tool.
	Invoke(ctx context.Context, tool string, args json.RawMessage, creds map[string]string) (any, error)
}

// ErrNotImplemented marks connectors whose OAuth app is not configured in this deployment.
var ErrNotImplemented = errors.New("this connector requires an OAuth application that is not configured in this deployment")

// Registry holds all connector definitions and implementations.
type Registry struct {
	order []string
	items map[string]Connector
}

// NewRegistry builds the default registry.
func NewRegistry(fetcher imports.Fetcher, httpClient *http.Client) *Registry {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	r := &Registry{items: map[string]Connector{}}
	r.add(&webConnector{fetcher: fetcher})
	r.add(&githubConnector{http: httpClient, base: "https://api.github.com"})
	r.add(&customConnector{fetcher: fetcher})
	for _, d := range plannedDefinitions() {
		r.add(&definitionOnly{def: d})
	}
	return r
}

func (r *Registry) add(c Connector) {
	k := c.Definition().Key
	r.order = append(r.order, k)
	r.items[k] = c
}

// All returns definitions in display order.
func (r *Registry) All() []Definition {
	out := make([]Definition, 0, len(r.order))
	for _, k := range r.order {
		out = append(out, r.items[k].Definition())
	}
	return out
}

// Get returns a connector by key.
func (r *Registry) Get(key string) (Connector, bool) { c, ok := r.items[key]; return c, ok }

// FindTool locates the connector providing a tool.
func (r *Registry) FindTool(name string) (Connector, Tool, bool) {
	for _, k := range r.order {
		c := r.items[k]
		for _, t := range c.Definition().Tools {
			if t.Name == name {
				return c, t, true
			}
		}
	}
	return nil, Tool{}, false
}

// ---------- Web (public pages; no auth) ----------

type webConnector struct{ fetcher imports.Fetcher }

func (w *webConnector) Definition() Definition {
	return Definition{Key: "web", Name: "Web", Description: "Read public web pages (SSRF-protected: public addresses only, size and time limits).",
		AuthType: "none", Capabilities: []string{"read_public_web"}, Permissions: []string{"web.read"}, Implemented: true,
		SetupHint: "No credentials needed. Connecting grants the agent permission to read public pages; each fetch still asks for confirmation.",
		Tools: []Tool{{Name: "web_fetch", Description: "Fetch a public web page and return its readable text (treated as untrusted data).",
			Category: domain.ToolExternal, Permission: "web.read",
			Parameters: json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","description":"http(s) URL of a public page"}},"required":["url"]}`)}}}
}

func (w *webConnector) Verify(ctx context.Context, _ map[string]string) error {
	if w.fetcher == nil {
		return errors.New("web fetcher not configured")
	}
	return nil
}

func (w *webConnector) Invoke(ctx context.Context, tool string, args json.RawMessage, _ map[string]string) (any, error) {
	if tool != "web_fetch" {
		return nil, fmt.Errorf("unknown tool %s", tool)
	}
	var in struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(args, &in); err != nil || in.URL == "" {
		return nil, errors.New("url is required")
	}
	body, ctype, final, err := w.fetcher.Fetch(ctx, in.URL)
	if err != nil {
		return nil, err
	}
	text := htmlToText(string(body), ctype)
	return map[string]any{"url": final, "content_type": ctype, "untrusted_content": truncate(text, 20000), "truncated": len(text) > 20000}, nil
}

// ---------- GitHub (personal access token) ----------

type githubConnector struct {
	http *http.Client
	base string
}

func (g *githubConnector) Definition() Definition {
	return Definition{Key: "github", Name: "GitHub", Description: "Search your repositories, read READMEs and create issues from ideas.",
		AuthType: "api_key", Capabilities: []string{"read_repos", "create_issues"}, Permissions: []string{"github.read", "github.issues.write"}, Implemented: true,
		SetupHint:        "Create a fine-grained personal access token (Contents: read, Issues: read & write) at github.com/settings/tokens.",
		CredentialFields: []CredentialField{{Name: "api_key", Label: "Personal access token", Secret: true, Required: true, Hint: "github_pat_… or ghp_…"}},
		Tools: []Tool{
			{Name: "github_list_repos", Description: "List the authenticated user's repositories (most recently updated first).", Category: domain.ToolExternal, Permission: "github.read",
				Parameters: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"optional name filter"}}}`)},
			{Name: "github_get_readme", Description: "Read a repository README (owner/repo).", Category: domain.ToolExternal, Permission: "github.read",
				Parameters: json.RawMessage(`{"type":"object","properties":{"repo":{"type":"string","description":"owner/repo"}},"required":["repo"]}`)},
			{Name: "github_create_issue", Description: "Create an issue in a repository (external action).", Category: domain.ToolExternal, Permission: "github.issues.write",
				Parameters: json.RawMessage(`{"type":"object","properties":{"repo":{"type":"string"},"title":{"type":"string"},"body":{"type":"string"}},"required":["repo","title"]}`)},
		}}
}

func (g *githubConnector) do(ctx context.Context, method, path, token string, body any) (json.RawMessage, error) {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = strings.NewReader(string(b))
	}
	req, err := http.NewRequestWithContext(ctx, method, g.base+path, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := g.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &e)
		return nil, fmt.Errorf("github: HTTP %d: %s", resp.StatusCode, e.Message)
	}
	return data, nil
}

func (g *githubConnector) Verify(ctx context.Context, creds map[string]string) error {
	if creds["api_key"] == "" {
		return errors.New("personal access token is required")
	}
	_, err := g.do(ctx, http.MethodGet, "/user", creds["api_key"], nil)
	return err
}

func validRepo(r string) bool {
	parts := strings.Split(r, "/")
	if len(parts) != 2 {
		return false
	}
	for _, p := range parts {
		if p == "" || strings.ContainsAny(p, " ?#%\\") || p == "." || p == ".." {
			return false
		}
	}
	return true
}

func (g *githubConnector) Invoke(ctx context.Context, tool string, args json.RawMessage, creds map[string]string) (any, error) {
	tok := creds["api_key"]
	var in struct {
		Query string `json:"query"`
		Repo  string `json:"repo"`
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	_ = json.Unmarshal(args, &in)
	switch tool {
	case "github_list_repos":
		data, err := g.do(ctx, http.MethodGet, "/user/repos?sort=updated&per_page=50", tok, nil)
		if err != nil {
			return nil, err
		}
		var repos []struct {
			FullName    string `json:"full_name"`
			Description string `json:"description"`
			HTMLURL     string `json:"html_url"`
			UpdatedAt   string `json:"updated_at"`
			Private     bool   `json:"private"`
		}
		_ = json.Unmarshal(data, &repos)
		var out []map[string]any
		for _, r := range repos {
			if in.Query != "" && !strings.Contains(strings.ToLower(r.FullName+" "+r.Description), strings.ToLower(in.Query)) {
				continue
			}
			out = append(out, map[string]any{"repo": r.FullName, "description": r.Description, "url": r.HTMLURL, "updated_at": r.UpdatedAt, "private": r.Private})
		}
		return map[string]any{"repositories": out}, nil
	case "github_get_readme":
		if !validRepo(in.Repo) {
			return nil, errors.New("repo must be owner/name")
		}
		data, err := g.do(ctx, http.MethodGet, "/repos/"+in.Repo+"/readme", tok, nil)
		if err != nil {
			return nil, err
		}
		var rd struct {
			Content  string `json:"content"`
			Encoding string `json:"encoding"`
		}
		_ = json.Unmarshal(data, &rd)
		text := decodeB64(rd.Content)
		return map[string]any{"repo": in.Repo, "untrusted_content": truncate(text, 20000)}, nil
	case "github_create_issue":
		if !validRepo(in.Repo) || strings.TrimSpace(in.Title) == "" {
			return nil, errors.New("repo (owner/name) and title are required")
		}
		data, err := g.do(ctx, http.MethodPost, "/repos/"+in.Repo+"/issues", tok, map[string]string{"title": in.Title, "body": in.Body})
		if err != nil {
			return nil, err
		}
		var iss struct {
			Number  int    `json:"number"`
			HTMLURL string `json:"html_url"`
		}
		_ = json.Unmarshal(data, &iss)
		return map[string]any{"number": iss.Number, "url": iss.HTMLURL}, nil
	}
	return nil, fmt.Errorf("unknown tool %s", tool)
}

// ---------- Custom (GET JSON/text from a configured public base URL) ----------

type customConnector struct{ fetcher imports.Fetcher }

func (c *customConnector) Definition() Definition {
	return Definition{Key: "custom", Name: "Custom HTTP", Description: "Read JSON or text from your own public HTTP endpoint (e.g. a personal API or knowledge base).",
		AuthType: "api_key", Capabilities: []string{"read_custom_endpoint"}, Permissions: []string{"custom.read"}, Implemented: true,
		SetupHint:        "Provide a public https base URL. Requests are GET-only and SSRF-protected.",
		CredentialFields: []CredentialField{{Name: "base_url", Label: "Base URL", Required: true, Hint: "https://example.com/api"}},
		Tools: []Tool{{Name: "custom_get", Description: "GET a path under the configured base URL.", Category: domain.ToolExternal, Permission: "custom.read",
			Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)}}}
}

func (c *customConnector) Verify(ctx context.Context, creds map[string]string) error {
	u, err := url.Parse(creds["base_url"])
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return errors.New("base_url must be an http(s) URL")
	}
	return nil
}

func (c *customConnector) Invoke(ctx context.Context, tool string, args json.RawMessage, creds map[string]string) (any, error) {
	var in struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(args, &in)
	base, err := url.Parse(creds["base_url"])
	if err != nil {
		return nil, err
	}
	rel, err := url.Parse(strings.TrimLeft(in.Path, "/"))
	if err != nil || rel.IsAbs() || strings.Contains(in.Path, "..") {
		return nil, errors.New("path must be relative to the base URL")
	}
	if !strings.HasSuffix(base.Path, "/") {
		base.Path += "/"
	}
	target := base.ResolveReference(rel)
	body, ctype, final, err := c.fetcher.Fetch(ctx, target.String())
	if err != nil {
		return nil, err
	}
	return map[string]any{"url": final, "content_type": ctype, "untrusted_content": truncate(string(body), 20000)}, nil
}

// ---------- Planned / OAuth connectors (definitions only) ----------

type definitionOnly struct{ def Definition }

func (d *definitionOnly) Definition() Definition { return d.def }
func (d *definitionOnly) Verify(context.Context, map[string]string) error {
	if d.def.AuthType == "export" {
		return errors.New("this platform has no API for reading your conversations; use its official export in the Import Center")
	}
	return ErrNotImplemented
}
func (d *definitionOnly) Invoke(context.Context, string, json.RawMessage, map[string]string) (any, error) {
	return nil, ErrNotImplemented
}

func plannedDefinitions() []Definition {
	oauth := func(key, name, desc string, caps, perms []string) Definition {
		return Definition{Key: key, Name: name, Description: desc, AuthType: "oauth2", Capabilities: caps, Permissions: perms,
			SetupHint: "Requires registering an OAuth application with " + name + " and setting its client id/secret on the server. Not configured in this deployment."}
	}
	export := func(key, name, how string) Definition {
		return Definition{Key: key, Name: name, AuthType: "export", Capabilities: []string{"import_conversations"}, Permissions: []string{},
			Description: "Bring " + name + " conversations into IdeaVault via the official export or a public share link.",
			SetupHint:   how + " IdeaVault never asks for your " + name + " password and cannot open private conversation links."}
	}
	return []Definition{
		oauth("gdrive", "Google Drive", "Attach documents from Drive as sources.", []string{"read_documents"}, []string{"drive.read"}),
		oauth("gmail", "Gmail", "Use email threads as evidence or sources.", []string{"read_email"}, []string{"gmail.read"}),
		oauth("slack", "Slack", "Bring discussions from Slack channels into ideas.", []string{"read_messages"}, []string{"slack.read"}),
		oauth("calendar", "Google Calendar", "Schedule action items and reviews.", []string{"create_events"}, []string{"calendar.write"}),
		oauth("notion", "Notion", "Import pages and export artifacts to Notion.", []string{"read_pages", "write_pages"}, []string{"notion.read", "notion.write"}),
		export("chatgpt", "ChatGPT", "Settings → Data controls → Export data, then upload conversations.json (or the .zip). Or paste a public share link (chatgpt.com/share/…)."),
		export("claude", "Claude", "Settings → Privacy → Export data, then upload conversations.json (or the .zip)."),
		export("gemini", "Gemini", "Google Takeout → My Activity → Gemini Apps (JSON), then upload MyActivity.json."),
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
