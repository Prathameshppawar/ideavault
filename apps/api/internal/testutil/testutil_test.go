package testutil

import (
	"strings"
	"testing"
)

func TestParseSSE(t *testing.T) {
	stream := ": keep-alive\n\n" +
		"event: run_started\ndata: {\"run_id\":\"r1\"}\n\n" +
		"event: token\ndata: {\"text\":\"Hel\"}\n\n" +
		"data: line one\ndata: line two\n\n" +
		"event: done\ndata: {}\n"
	evs, err := ParseSSE(strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	names := EventNames(evs)
	if strings.Join(names, ",") != "run_started,token,message,done" {
		t.Fatalf("events = %v", names)
	}
	var started struct {
		RunID string `json:"run_id"`
	}
	evs[0].Decode(t, &started)
	if started.RunID != "r1" {
		t.Errorf("run_id = %q", started.RunID)
	}
	if string(evs[2].Data) != "line one\nline two" {
		t.Errorf("multi-line data = %q", evs[2].Data)
	}
	if len(EventsNamed(evs, "token")) != 1 || len(EventsNamed(evs, "missing")) != 0 {
		t.Error("EventsNamed")
	}
}

func TestConfigIsOffline(t *testing.T) {
	c := Config("postgres://x")
	if !c.MockAI || c.AutoMigrate || len(c.MasterKey) != 32 || c.RedisURL != "" || c.EmbeddingProvider != "local" {
		t.Errorf("test config must be offline and deterministic: %+v", c.Redacted())
	}
	if c.OpenAIKey != "" || c.GroqKey != "" || c.AnthropicKey != "" || c.GeminiKey != "" {
		t.Error("test config must never carry provider keys")
	}
}

func TestRepoRootHasFixtures(t *testing.T) {
	root, err := RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(FixturePath(t, "imports", "chatgpt", "conversations.json"), "tests/fixtures/imports/chatgpt/conversations.json") || root == "" {
		t.Error("fixture path resolution broken")
	}
	if len(Fixture(t, "imports", "injection", "malicious_chatgpt.json")) == 0 {
		t.Error("fixture empty")
	}
}
