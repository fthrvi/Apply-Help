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
	"runtime"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/sashabaranov/go-openai"
)

// ═════════════════════════════════════════════════════════════════════════════
// GLOBAL SETUP & CONFIGURATION
// ═════════════════════════════════════════════════════════════════════════════

const (
	FastModel       = "google/gemma-3-27b-it"
	GenerationModel = "google/gemma-3-27b-it"
)

var (
	openAIClient *openai.Client
	scriptDir    string
)

func init() {
	_, filename, _, ok := runtime.Caller(0)
	if ok {
		scriptDir = filepath.Dir(filename)
	} else {
		scriptDir = "."
	}

	envPath := filepath.Join(scriptDir, ".env")
	_ = godotenv.Load(envPath)
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
		res := CallLLM(prompt, "You are a helpful assistant.", FastModel, 4096)
		if !res.Success {
			return "", res.Error
		}
		// Convert the map response to a string representation for generic use
		bytes, _ := json.Marshal(res.Data)
		return string(bytes), nil
	default:
		return "", fmt.Errorf("unsupported model choice: %s", modelChoice)
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

	openAIKey := GetSetting(GlobalDB, "OPENAI_API_KEY")
	if openAIKey == "" {
		openAIKey = os.Getenv("AIKEY")
	}

	if openAIKey != "" {
		config := openai.DefaultConfig(openAIKey)
		config.BaseURL = "https://integrate.api.nvidia.com/v1"
		openAIClient = openai.NewClientWithConfig(config)
	}
	return openAIClient
}

func CallLLM(prompt, system, model string, maxTokens int) LLMResult {
	client := getOpenAIClient()
	if client == nil {
		return LLMResult{Success: false, Error: fmt.Errorf("OpenAI client not initialized")}
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
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		},
	}

	resp, err := client.CreateChatCompletion(ctx, req)
	if err != nil {
		return LLMResult{Success: false, Error: err}
	}

	raw := strings.TrimSpace(resp.Choices[0].Message.Content)

	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return LLMResult{Success: false, Error: fmt.Errorf("invalid JSON: %w\nRaw: %s", err, raw)}
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
	apiKey := GetSetting(GlobalDB, "GEMINI_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("API_KEY")
	}
	apiURL := GetSetting(GlobalDB, "GEMINI_URL")
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
	apiKey := GetSetting(GlobalDB, "CLAUDE_API_KEY")
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
		Model:     "claude-3-7-sonnet-latest",
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

