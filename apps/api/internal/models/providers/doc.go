// Package providers implements IdeaVault's concrete LLM chat providers and
// embedding backends behind the provider-neutral interfaces in package models
// (models.Provider and models.Embedder).
//
// # Backends
//
//   - OpenAICompatible (NewOpenAICompatible): any /chat/completions API —
//     OpenAI, Groq, and local servers such as Ollama or LM Studio. Also
//     exposes ListModels for "sync models from provider".
//   - OpenAIEmbedder (NewOpenAIEmbedder): /embeddings on the same APIs.
//   - Anthropic (NewAnthropic): Claude via the official Anthropic Go SDK.
//   - Gemini (NewGemini) and GeminiEmbedder (NewGeminiEmbedder): the native
//     Gemini REST API (generateContent / streamGenerateContent /
//     batchEmbedContents).
//   - HashEmbedder (NewHashEmbedder): an offline, zero-cost lexical
//     feature-hashing embedder ("local" / "hash-768-v1").
//
// # Configuration and environment
//
// Providers never read environment variables; the registry wiring resolves
// configuration and passes it in via each constructor's Config struct. The
// conventional variables are:
//
//	OPENAI_API_KEY                   OpenAICompatConfig{ID: "openai"}, OpenAIEmbedConfig
//	GROQ_API_KEY                     OpenAICompatConfig{ID: "groq"}
//	ANTHROPIC_API_KEY                AnthropicConfig
//	GEMINI_API_KEY (or GOOGLE_API_KEY)  GeminiConfig, GeminiEmbedConfig
//	OLLAMA_BASE_URL                  OpenAICompatConfig{ID: "ollama", BaseURL: ...}
//	                                 (default http://localhost:11434/v1; include /v1)
//
// A provider without required credentials returns an error wrapping
// models.ErrNotConfigured from every call.
//
// # Behavior shared by all providers
//
//   - Context cancellation stops generation promptly; the returned error wraps
//     ctx.Err().
//   - HTTP failures are returned as *models.ProviderError, with Retryable set
//     for 408/409/425/429/5xx (incl. Anthropic 529) and transient network
//     errors. Error text never contains API keys (see redact).
//   - Response.Model is the requested model ID. When the provider reports no
//     usage, tokens are estimated (≈ chars/4) and Usage.Estimated is set.
//   - Complete and Stream share one response assembler, so they produce the
//     same Response for the same output.
//   - Hidden reasoning (Anthropic thinking blocks, Gemini thought parts,
//     reasoning/reasoning_content fields, inline <think>/<thinking> blocks
//     anywhere in OpenAI-compatible content) is never put into
//     Response.Content nor streamed to onDelta. Groq reasoning models are also
//     asked not to emit it (see GroqReasoningParams and
//     OpenAICompatConfig.ReasoningParams). Opaque content that must be returned verbatim
//     (Anthropic thinking blocks, Gemini thought signatures) travels in
//     Response.Replay and is replayed only to the same provider and model.
//   - Structured output (Request.JSONSchema) uses native support where
//     available — OpenAI response_format json_schema, Anthropic
//     output_config.format, Gemini responseSchema — and otherwise falls back to
//     instructions plus JSON extraction from the reply.
package providers
