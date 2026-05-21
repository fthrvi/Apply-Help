package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

var mathSqrt = math.Sqrt

// Local LLM provider — talks Ollama's HTTP API. The configured endpoint
// is just "host:port" (e.g. http://prithvi-system-product-name:11434);
// we hit /api/chat and /api/embed against it.
//
// Why this matters: the autofill agent calls this in a tight loop while
// a user watches a form fill in real-time. Cloud Claude at 2-10s per
// call is unusable; a 7b model on a local GPU is ~100-300ms.
// KV-cache keepalive means the second call onward is even faster.

const defaultLocalLLMTimeout = 60 * time.Second

// localLLMClient is reused across calls so the underlying TCP
// connection stays warm.
var localLLMClient = &http.Client{Timeout: defaultLocalLLMTimeout}

// ollamaChatRequest mirrors the Ollama /api/chat schema. stream=false
// gives us a single JSON response (simpler than handling SSE).
type ollamaChatRequest struct {
	Model    string              `json:"model"`
	Messages []ollamaChatMessage `json:"messages"`
	Stream   bool                `json:"stream"`
	Format   string              `json:"format,omitempty"`
	Options  map[string]any      `json:"options,omitempty"`
}

type ollamaChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatResponse struct {
	Message ollamaChatMessage `json:"message"`
	Done    bool              `json:"done"`
	Error   string            `json:"error,omitempty"`
}

// LocalLLMChat runs a single chat completion against the configured
// Ollama endpoint. systemPrompt is the persistent context (typically
// the user's profile JSON); userPrompt is the per-call instruction.
// jsonMode requests structured JSON output (the model is asked to
// emit only JSON; Ollama enforces a JSON-shaped response).
func LocalLLMChat(systemPrompt, userPrompt string, jsonMode bool) (string, error) {
	endpoint := strings.TrimSpace(GetSetting(GlobalDB, KeyLocalLLMEndpoint))
	model := strings.TrimSpace(GetSetting(GlobalDB, KeyLocalLLMModel))
	if endpoint == "" {
		return "", fmt.Errorf("local LLM endpoint not configured (Settings → API Keys → Local LLM)")
	}
	if model == "" {
		model = "qwen2.5:7b-instruct"
	}

	req := ollamaChatRequest{
		Model:  model,
		Stream: false,
		Options: map[string]any{
			// Lower temperature for deterministic field decisions. The
			// autofill agent benefits from "given these clues, the
			// best value is X" rather than creative variation.
			"temperature": 0.2,
			// Cap to keep latency predictable; autofill answers are short.
			"num_predict": 512,
		},
		Messages: []ollamaChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}
	if jsonMode {
		req.Format = "json"
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	url := strings.TrimRight(endpoint, "/") + "/api/chat"
	ctx, cancel := context.WithTimeout(context.Background(), defaultLocalLLMTimeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := localLLMClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("local LLM %s: %w", url, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("local LLM HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed ollamaChatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("local LLM decode: %w (raw: %.200s)", err, string(respBody))
	}
	if parsed.Error != "" {
		return "", fmt.Errorf("local LLM: %s", parsed.Error)
	}
	return parsed.Message.Content, nil
}

// ollamaEmbedRequest is the new /api/embed schema (Ollama 0.5+).
// Older Ollama installs use /api/embeddings — kept as a fallback.
type ollamaEmbedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type ollamaEmbedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
	Error      string      `json:"error,omitempty"`
}

type ollamaEmbedLegacyRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaEmbedLegacyResponse struct {
	Embedding []float32 `json:"embedding"`
	Error     string    `json:"error,omitempty"`
}

// LocalLLMEmbed returns the embedding vector for text using the
// configured embed model (nomic-embed-text by default). Tries
// /api/embed first (newer Ollama); falls back to /api/embeddings.
func LocalLLMEmbed(text string) ([]float32, error) {
	endpoint := strings.TrimSpace(GetSetting(GlobalDB, KeyLocalLLMEndpoint))
	model := strings.TrimSpace(GetSetting(GlobalDB, KeyLocalLLMEmbedModel))
	if endpoint == "" {
		return nil, fmt.Errorf("local LLM endpoint not configured")
	}
	if model == "" {
		model = "nomic-embed-text"
	}
	endpoint = strings.TrimRight(endpoint, "/")

	// New API first.
	body, _ := json.Marshal(ollamaEmbedRequest{Model: model, Input: text})
	resp, err := localLLMClient.Post(endpoint+"/api/embed", "application/json", bytes.NewReader(body))
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode < 300 {
			respBody, _ := io.ReadAll(resp.Body)
			var parsed ollamaEmbedResponse
			if json.Unmarshal(respBody, &parsed) == nil && parsed.Error == "" && len(parsed.Embeddings) > 0 {
				return parsed.Embeddings[0], nil
			}
		}
	}

	// Legacy API fallback.
	body, _ = json.Marshal(ollamaEmbedLegacyRequest{Model: model, Prompt: text})
	resp2, err := localLLMClient.Post(endpoint+"/api/embeddings", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("local embed: %w", err)
	}
	defer resp2.Body.Close()
	respBody, _ := io.ReadAll(resp2.Body)
	if resp2.StatusCode >= 300 {
		return nil, fmt.Errorf("local embed HTTP %d: %s", resp2.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var parsed ollamaEmbedLegacyResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("local embed decode: %w", err)
	}
	if parsed.Error != "" {
		return nil, fmt.Errorf("local embed: %s", parsed.Error)
	}
	return parsed.Embedding, nil
}

// LocalLLMPing tries a tiny chat completion to confirm the endpoint
// is up, the model is loaded, and the network path works. Used by
// the Settings UI to surface a green/red indicator.
func LocalLLMPing() error {
	out, err := LocalLLMChat("You are a connection test.", "Reply with the single word OK.", false)
	if err != nil {
		return err
	}
	if !strings.Contains(strings.ToUpper(out), "OK") {
		return fmt.Errorf("unexpected response: %.80s", out)
	}
	return nil
}

// CosineSim returns the cosine similarity between two embedding
// vectors. Returns 0 for mismatched dimensions (defensive — different
// embed models produce different sizes).
func CosineSim(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float32
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (sqrt32(na) * sqrt32(nb))
}

func sqrt32(x float32) float32 {
	// Newton's method is overkill; math.Sqrt is fine. Pull in math
	// only via this helper to avoid sprinkling imports.
	return float32(mathSqrt(float64(x)))
}
