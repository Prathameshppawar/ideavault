package agent

import (
	"fmt"
	"strings"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
)

// Decision is the permission outcome for a tool call.
type Decision string

const (
	Allow   Decision = "allow"
	Confirm Decision = "confirm"
	Deny    Decision = "deny"
)

// Policy decides whether a tool call may run automatically. The model cannot
// influence it: it is evaluated by the supervisor on every call.
type Policy struct {
	// ConfirmWrites requires confirmation for WRITE tools too (default false).
	ConfirmWrites bool `json:"confirm_writes"`
	// AutoExternal lets EXTERNAL connector tools run without confirmation (default false).
	AutoExternal bool `json:"auto_external"`
	// Disabled tools are never callable.
	Disabled []string `json:"disabled_tools,omitempty"`
}

// Decide returns the permission decision for a tool in a given category.
func (p Policy) Decide(tool string, cat domain.ToolCategory) Decision {
	for _, d := range p.Disabled {
		if d == tool {
			return Deny
		}
	}
	switch cat {
	case domain.ToolRead, domain.ToolAnalyze:
		return Allow
	case domain.ToolWrite:
		if p.ConfirmWrites {
			return Confirm
		}
		return Allow
	case domain.ToolDestructive:
		return Confirm // always
	case domain.ToolExternal:
		if p.AutoExternal {
			return Allow
		}
		return Confirm
	}
	return Deny
}

// describeCall renders a human confirmation prompt for a tool call.
func describeCall(t Tool, args map[string]any) string {
	get := func(k string) string {
		if v, ok := args[k]; ok {
			return strings.TrimSpace(fmt.Sprint(v))
		}
		return ""
	}
	switch t.Name {
	case "delete_idea":
		return fmt.Sprintf("Permanently delete the idea %q and ALL of its history (branches, checkpoints, knowledge, artifacts). This cannot be undone.", get("idea"))
	case "delete_artifact":
		return "Permanently delete this artifact and its entire version history. This cannot be undone."
	case "web_fetch":
		return fmt.Sprintf("Fetch the public web page %s. Its content will be treated as untrusted data.", get("url"))
	case "github_create_issue":
		return fmt.Sprintf("Create a GitHub issue in %s titled %q.", get("repo"), get("title"))
	case "github_list_repos":
		return "List your GitHub repositories using your stored token."
	case "github_get_readme":
		return fmt.Sprintf("Read the README of %s from GitHub.", get("repo"))
	case "custom_get":
		return fmt.Sprintf("GET %s from your custom connector.", get("path"))
	}
	return fmt.Sprintf("Run %s (%s).", t.Name, strings.ToLower(string(t.Category)))
}
