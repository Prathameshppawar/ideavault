package models

import "github.com/Prathameshppawar/ideavault/apps/api/internal/domain"

// Provider ids.
const (
	ProviderOpenAI    = "openai"
	ProviderGroq      = "groq"
	ProviderAnthropic = "anthropic"
	ProviderGemini    = "gemini"
	ProviderOllama    = "ollama"
	ProviderMock      = "mock"
)

// MockModel is the deterministic offline planner (no LLM). Used when no provider is
// configured and in automated tests. It never pretends to be a language model.
const MockModel = "offline-planner"

// BuiltinModels is the seed catalog. Prices are USD per million tokens as published
// by providers when this catalog was written (2026-10); they are editable in the
// Model Registry because prices and model availability change.
var BuiltinModels = []domain.ModelConfig{
	// Groq — very fast open-weight models; generous free tier. (Verified 2026-10-09:
	// tool calling + json_schema structured output on all three.)
	{Provider: ProviderGroq, Model: "openai/gpt-oss-20b", DisplayName: "GPT-OSS 20B (Groq)", ContextLength: 131072,
		ToolCalling: true, StructuredOutput: true, Reasoning: true, RelativeCost: 0, Speed: 5, Quality: 3, InputCostPerMTok: 0.075, OutputCostPerMTok: 0.30,
		Capabilities: []string{"chat", "tools", "json", "reasoning", "fast"}, Enabled: true},
	{Provider: ProviderGroq, Model: "openai/gpt-oss-120b", DisplayName: "GPT-OSS 120B (Groq)", ContextLength: 131072,
		ToolCalling: true, StructuredOutput: true, Reasoning: true, RelativeCost: 1, Speed: 4, Quality: 4, InputCostPerMTok: 0.15, OutputCostPerMTok: 0.60,
		Capabilities: []string{"chat", "tools", "json", "reasoning"}, Enabled: true},
	{Provider: ProviderGroq, Model: "qwen/qwen3.8-27b", DisplayName: "Qwen 3.8 27B (Groq)", ContextLength: 131072,
		ToolCalling: true, StructuredOutput: true, Reasoning: true, RelativeCost: 2, Speed: 4, Quality: 3, InputCostPerMTok: 0.60, OutputCostPerMTok: 3.00,
		Capabilities: []string{"chat", "tools", "json", "reasoning"}, Enabled: true},

	// OpenAI
	{Provider: ProviderOpenAI, Model: "gpt-5-nano", DisplayName: "GPT-5 nano", ContextLength: 400000,
		ToolCalling: true, StructuredOutput: true, Vision: true, Reasoning: true, RelativeCost: 1, Speed: 4, Quality: 3, InputCostPerMTok: 0.05, OutputCostPerMTok: 0.40,
		Capabilities: []string{"chat", "tools", "json", "vision", "fast"}, Enabled: true},
	{Provider: ProviderOpenAI, Model: "gpt-5-mini", DisplayName: "GPT-5 mini", ContextLength: 400000,
		ToolCalling: true, StructuredOutput: true, Vision: true, Reasoning: true, RelativeCost: 2, Speed: 3, Quality: 4, InputCostPerMTok: 0.25, OutputCostPerMTok: 2.00,
		Capabilities: []string{"chat", "tools", "json", "vision", "reasoning"}, Enabled: true},
	{Provider: ProviderOpenAI, Model: "gpt-5.4", DisplayName: "GPT-5.4", ContextLength: 400000,
		ToolCalling: true, StructuredOutput: true, Vision: true, Reasoning: true, RelativeCost: 4, Speed: 2, Quality: 5, InputCostPerMTok: 2.50, OutputCostPerMTok: 15.00,
		Capabilities: []string{"chat", "tools", "json", "vision", "reasoning"}, Enabled: true},

	// Anthropic
	{Provider: ProviderAnthropic, Model: "claude-haiku-5-5", DisplayName: "Claude Haiku 5.5", ContextLength: 1000000,
		ToolCalling: true, StructuredOutput: true, Vision: true, Reasoning: true, RelativeCost: 1, Speed: 5, Quality: 4, InputCostPerMTok: 0.10, OutputCostPerMTok: 0.50,
		Capabilities: []string{"chat", "tools", "json", "vision", "fast"}, Enabled: true},
	{Provider: ProviderAnthropic, Model: "claude-sonnet-5-5", DisplayName: "Claude Sonnet 5.5", ContextLength: 1000000,
		ToolCalling: true, StructuredOutput: true, Vision: true, Reasoning: true, RelativeCost: 3, Speed: 4, Quality: 5, InputCostPerMTok: 2.00, OutputCostPerMTok: 10.00,
		Capabilities: []string{"chat", "tools", "json", "vision", "reasoning"}, Enabled: true},
	{Provider: ProviderAnthropic, Model: "claude-opus-5-5", DisplayName: "Claude Opus 5.5", ContextLength: 1000000,
		ToolCalling: true, StructuredOutput: true, Vision: true, Reasoning: true, RelativeCost: 4, Speed: 3, Quality: 5, InputCostPerMTok: 4.00, OutputCostPerMTok: 20.00,
		Capabilities: []string{"chat", "tools", "json", "vision", "reasoning"}, Enabled: true},

	// Google Gemini — free tier available for personal use.
	{Provider: ProviderGemini, Model: "gemini-2.5-flash-lite", DisplayName: "Gemini 2.5 Flash-Lite", ContextLength: 1048576,
		ToolCalling: true, StructuredOutput: true, Vision: true, RelativeCost: 1, Speed: 5, Quality: 3, InputCostPerMTok: 0.10, OutputCostPerMTok: 0.40,
		Capabilities: []string{"chat", "tools", "json", "vision", "fast"}, Enabled: true},
	{Provider: ProviderGemini, Model: "gemini-2.5-flash", DisplayName: "Gemini 2.5 Flash", ContextLength: 1048576,
		ToolCalling: true, StructuredOutput: true, Vision: true, Reasoning: true, RelativeCost: 1, Speed: 4, Quality: 4, InputCostPerMTok: 0.30, OutputCostPerMTok: 2.50,
		Capabilities: []string{"chat", "tools", "json", "vision", "reasoning"}, Enabled: true},
	{Provider: ProviderGemini, Model: "gemini-3.8-flash", DisplayName: "Gemini 3.8 Flash", ContextLength: 1048576,
		ToolCalling: true, StructuredOutput: true, Vision: true, Reasoning: true, RelativeCost: 2, Speed: 4, Quality: 4, InputCostPerMTok: 0.75, OutputCostPerMTok: 3.75,
		Capabilities: []string{"chat", "tools", "json", "vision", "reasoning"}, Enabled: true},
	{Provider: ProviderGemini, Model: "gemini-3.1-pro", DisplayName: "Gemini 3.1 Pro (preview)", ContextLength: 1048576,
		ToolCalling: true, StructuredOutput: true, Vision: true, Reasoning: true, RelativeCost: 3, Speed: 2, Quality: 5, InputCostPerMTok: 2.00, OutputCostPerMTok: 12.00,
		Capabilities: []string{"chat", "tools", "json", "vision", "reasoning"}, Enabled: true},

	// Local (Ollama / any OpenAI-compatible server) — free, private.
	{Provider: ProviderOllama, Model: "llama3.1", DisplayName: "Llama 3.1 (local Ollama)", ContextLength: 131072,
		ToolCalling: true, StructuredOutput: true, RelativeCost: 0, Speed: 2, Quality: 2,
		Capabilities: []string{"chat", "tools", "json", "local"}, Enabled: true},

	// Deterministic offline planner (no LLM).
	{Provider: ProviderMock, Model: MockModel, DisplayName: "Offline rule-based planner (no AI)", ContextLength: 1000000,
		ToolCalling: true, StructuredOutput: true, RelativeCost: 0, Speed: 5, Quality: 1,
		Capabilities: []string{"chat", "tools", "offline"}, Enabled: true},
}
