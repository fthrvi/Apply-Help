package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/sashabaranov/go-openai"
)

// ═════════════════════════════════════════════════════════════════════════════
// GLOBAL SETUP & CONFIGURATION
// ═════════════════════════════════════════════════════════════════════════════

var (
	openAIClient *openai.Client
)

// openAIModel returns the configured OpenAI/NVIDIA model from settings,
// falling back to DefaultOpenAIModel when unset.
func openAIModel() string {
	if m := GetSetting(GlobalDB, KeyOpenAIModel); m != "" {
		return m
	}
	return DefaultOpenAIModel
}

// claudeModel returns the configured Anthropic model from settings,
// falling back to DefaultClaudeModel when unset.
func claudeModel() string {
	if m := GetSetting(GlobalDB, KeyClaudeModel); m != "" {
		return m
	}
	return DefaultClaudeModel
}

func init() {
	// Load .env from (a) the current working directory (dev convenience) or
	// (b) the user's config dir under "applyhelp". The previous implementation
	// used runtime.Caller(0), which returns the build-time source path and is
	// meaningless in a shipped binary.
	candidates := []string{".env"}
	if cfgDir, err := os.UserConfigDir(); err == nil {
		candidates = append(candidates, filepath.Join(cfgDir, "applyhelp", ".env"))
	}
	for _, p := range candidates {
		if godotenv.Load(p) == nil {
			return
		}
	}
}

// PromptAI is a master router that calls the correct Go service based on the UI dropdown.
func PromptAI(prompt string, modelChoice string) (string, error) {
	switch strings.ToLower(modelChoice) {
	case "gemini":
		svc := NewGeminiService()
		return svc.Chat(prompt)
	case "claude":
		svc := NewAnthropicService()
		return svc.Chat(prompt)
	case "openai":
		// Fallback to the JSON-enforced OpenAI call if needed, or implement a raw text return here.
		res := CallLLM(prompt, "You are a helpful assistant.", openAIModel(), 4096)
		if !res.Success {
			return "", res.Error
		}
		// Convert the map response to a string representation for generic use
		bytes, _ := json.Marshal(res.Data)
		return string(bytes), nil
	default:
		err := fmt.Errorf("unsupported model choice: %s", modelChoice)
		LogError(GlobalDB, err.Error())
		return "", err
	}
}

// ═════════════════════════════════════════════════════════════════════════════
// 1. OPENAI / NVIDIA IMPLEMENTATION
// ═════════════════════════════════════════════════════════════════════════════

type LLMResult struct {
	Success bool
	Data    map[string]any
	Error   error
}

func getOpenAIClient() *openai.Client {
	if openAIClient != nil {
		return openAIClient
	}

	openAIKey := GetSetting(GlobalDB, KeyOpenAIAPI)
	if openAIKey == "" {
		openAIKey = os.Getenv("AIKEY")
	}

	if openAIKey != "" {
		config := openai.DefaultConfig(openAIKey)
		// If it's an NVIDIA key, use their endpoint. Otherwise, default to OpenAI.
		if strings.HasPrefix(openAIKey, "nvapi-") {
			config.BaseURL = "https://integrate.api.nvidia.com/v1"
		}
		openAIClient = openai.NewClientWithConfig(config)
	}
	return openAIClient
}

func CallLLM(prompt, system, model string, maxTokens int) LLMResult {
	client := getOpenAIClient()
	if client == nil {
		return LLMResult{Success: false, Error: fmt.Errorf("OpenAI/NVIDIA API Key not found in settings")}
	}

	// Convenience: if the user pasted a real OpenAI key (sk-) but never
	// updated the model from the gemma-on-NVIDIA default, swap in gpt-4o so
	// the first call works. Once they set KeyOpenAIModel themselves we trust it.
	apiKey := GetSetting(GlobalDB, KeyOpenAIAPI)
	if model == DefaultOpenAIModel && strings.HasPrefix(apiKey, "sk-") {
		model = "gpt-4o"
	}

	ctx := context.Background()
	req := openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: system},
			{Role: openai.ChatMessageRoleUser, Content: prompt},
		},
		Temperature: 0.7,
		MaxTokens:   maxTokens,
	}

	// Only request JSON format if the model is an OpenAI one (Gemma via NVIDIA can be finicky with this flag)
	if strings.HasPrefix(model, "gpt-") {
		req.ResponseFormat = &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		}
	}

	resp, err := client.CreateChatCompletion(ctx, req)
	if err != nil {
		return LLMResult{Success: false, Error: err}
	}

	raw := strings.TrimSpace(resp.Choices[0].Message.Content)

	// Clean up markdown code blocks if present
	if strings.Contains(raw, "```") {
		// Find the first '{' and last '}' to extract the JSON object
		reSimple := regexp.MustCompile(`(?s)\{.*\}`)
		if match := reSimple.FindString(raw); match != "" {
			raw = match
		}
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		errStr := fmt.Errorf("invalid JSON: %w\nRaw: %s", err, raw)
		LogError(GlobalDB, errStr.Error())
		return LLMResult{Success: false, Error: errStr}
	}

	return LLMResult{Success: true, Data: parsed}
}

// ═════════════════════════════════════════════════════════════════════════════
// 2. GEMINI API IMPLEMENTATION
// ═════════════════════════════════════════════════════════════════════════════

type GeminiRequest struct {
	Contents []struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	} `json:"contents"`
}

type GeminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

type GeminiService struct {
	APIKey string
	URL    string
	Client *http.Client
}

func NewGeminiService() *GeminiService {
	keyName := KeyGeminiAPI1
	if GetSetting(GlobalDB, KeyActiveGemini) == "2" {
		keyName = KeyGeminiAPI2
	}

	apiKey := GetSetting(GlobalDB, keyName)
	if apiKey == "" {
		apiKey = os.Getenv("API_KEY")
	}
	apiURL := GetSetting(GlobalDB, KeyGeminiURL)
	if apiURL == "" {
		apiURL = os.Getenv("URL")
	}

	return &GeminiService{
		APIKey: apiKey,
		URL:    apiURL,
		Client: &http.Client{Timeout: 60 * time.Second},
	}
}

func (s *GeminiService) Chat(prompt string) (string, error) {
	if s.APIKey == "" || s.URL == "" {
		return "", fmt.Errorf("API_KEY or URL missing")
	}

	payload := GeminiRequest{
		Contents: []struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		}{
			{Parts: []struct {
				Text string `json:"text"`
			}{{Text: prompt}}},
		},
	}

	jsonData, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", s.URL, bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-goog-api-key", s.APIKey)

	resp, err := s.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var geminiResp GeminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return "", err
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("unexpected response structure")
	}

	return geminiResp.Candidates[0].Content.Parts[0].Text, nil
}

// ═════════════════════════════════════════════════════════════════════════════
// 3. ANTHROPIC / CLAUDE API IMPLEMENTATION
// ═════════════════════════════════════════════════════════════════════════════

type AnthropicRequest struct {
	Model     string `json:"model"`
	MaxTokens int    `json:"max_tokens"`
	Messages  []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

type AnthropicResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type AnthropicService struct {
	APIKey string
	URL    string
	Client *http.Client
}

func NewAnthropicService() *AnthropicService {
	apiKey := GetSetting(GlobalDB, KeyClaudeAPI)
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}

	return &AnthropicService{
		APIKey: apiKey,
		URL:    "https://api.anthropic.com/v1/messages",
		Client: &http.Client{Timeout: 60 * time.Second},
	}
}

func (s *AnthropicService) Chat(prompt string) (string, error) {
	if s.APIKey == "" {
		return "", fmt.Errorf("ANTHROPIC_API_KEY missing")
	}

	payload := AnthropicRequest{
		Model:     claudeModel(),
		MaxTokens: 4096,
		Messages: []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{
			{Role: "user", Content: prompt},
		},
	}

	jsonData, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", s.URL, bytes.NewBuffer(jsonData))

	req.Header.Set("x-api-key", s.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	resp, err := s.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var anthropicResp AnthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&anthropicResp); err != nil {
		return "", err
	}

	if anthropicResp.Error != nil {
		return "", fmt.Errorf("%s - %s", anthropicResp.Error.Type, anthropicResp.Error.Message)
	}

	if len(anthropicResp.Content) == 0 {
		return "", fmt.Errorf("unexpected response structure")
	}

	return anthropicResp.Content[0].Text, nil
}
