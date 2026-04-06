package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"text/template"
	"time"

	"dsl-strategy-evolver/internal/dsl"
)

// Generator generates trading strategies using AI
type Generator struct {
	client    *http.Client
	modelPool *ModelPool
	apiKey    string
	baseURL   string
}

// NewGenerator creates a new AI generator
func NewGenerator(apiKey, baseURL string) *Generator {
	return &Generator{
		client:    &http.Client{Timeout: 90 * time.Second},
		modelPool: NewModelPool(),
		apiKey:    apiKey,
		baseURL:   baseURL,
	}
}

// ModelPool manages model rotation with rate limit handling
type ModelPool struct {
	models      []string
	current     int
	rateLimited map[string]bool // Track rate-limited models
	mu          sync.Mutex
}

// NewModelPool creates a new model pool with 5 free models
func NewModelPool() *ModelPool {
	return &ModelPool{
		models: []string{
			"meta-llama/llama-3.2-3b-instruct:free",
			"google/gemma-3-27b-it:free",
			"stepfun/step-3.5-flash:free",
			"z-ai/glm-4.5-air:free",
			"mistralai/mistral-small-3.1-24b-instruct:free",
		},
		current:     0,
		rateLimited: make(map[string]bool),
	}
}

// Next returns the next available model (skips rate-limited ones)
func (mp *ModelPool) Next() string {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	// Try to find a non-rate-limited model
	for i := 0; i < len(mp.models); i++ {
		model := mp.models[mp.current]
		mp.current = (mp.current + 1) % len(mp.models)
		
		if !mp.rateLimited[model] {
			return model
		}
	}

	// All rate-limited, reset and return first
	mp.rateLimited = make(map[string]bool)
	return mp.models[0]
}

// MarkRateLimited marks a model as rate-limited
func (mp *ModelPool) MarkRateLimited(model string) {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	mp.rateLimited[model] = true
}

// ResetRateLimits clears all rate limits (call after some time)
func (mp *ModelPool) ResetRateLimits() {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	mp.rateLimited = make(map[string]bool)
}

// GetCurrent returns the current model
func (mp *ModelPool) GetCurrent() string {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	index := (mp.current - 1 + len(mp.models)) % len(mp.models)
	return mp.models[index]
}

// GetAllModels returns all models in the pool
func (mp *ModelPool) GetAllModels() []string {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	return append([]string{}, mp.models...)
}

// GenerateStrategies generates new strategies using AI with automatic failover to Claude Code CLI
func (g *Generator) GenerateStrategies(ctx context.Context, count int, symbol string, topStrategies []*dsl.Strategy) ([]*dsl.Strategy, error) {
	var lastErr error
	
	// Try each OpenRouter model until one succeeds
	for i := 0; i < len(g.modelPool.models); i++ {
		model := g.modelPool.Next()
		
		prompt, err := g.buildPrompt(count, symbol, topStrategies)
		if err != nil {
			return nil, fmt.Errorf("failed to build prompt: %w", err)
		}

		response, err := g.callAI(ctx, model, prompt, false)
		if err != nil {
			lastErr = err
			// Check if rate limited - immediately fallback to Claude Code CLI
			if isRateLimited(err) {
				g.modelPool.MarkRateLimited(model)
				log.Printf("[AI] Model %s rate limited, falling back to Claude Code CLI...", model)
				
				// Fallback to Claude Code CLI immediately
				response, err = g.callClaudeCodeCLI(ctx, prompt)
				if err != nil {
					return nil, fmt.Errorf("Claude Code CLI fallback failed: %w", err)
				}

				// Reset rate limits so next request tries OpenRouter again
				g.modelPool.ResetRateLimits()

				strategies, err := g.parseStrategies(response)
				if err != nil {
					return nil, fmt.Errorf("failed to parse Claude Code CLI response: %w", err)
				}

				if len(strategies) > count {
					strategies = strategies[:count]
				}

				// Set metadata
				for i, strategy := range strategies {
					strategy.ID = fmt.Sprintf("ai_gen_claude_%d_%d", time.Now().Unix(), i)
					strategy.Source = "ai:claude-code-cli"
					strategy.Generation = 1
				}

				log.Printf("[AI] Generated %d strategies using Claude Code CLI (fallback)", len(strategies))
				return strategies, nil
			}
			continue
		}

		// Success - reset rate limits for next time
		g.modelPool.ResetRateLimits()

		strategies, err := g.parseStrategies(response)
		if err != nil {
			lastErr = err
			continue
		}

		if len(strategies) > count {
			strategies = strategies[:count]
		}

		// Set metadata
		for i, strategy := range strategies {
			strategy.ID = fmt.Sprintf("ai_gen_%d_%d", time.Now().Unix(), i)
			strategy.Source = fmt.Sprintf("ai:%s", model)
			strategy.Generation = 1
		}

		log.Printf("[AI] Generated %d strategies using %s", len(strategies), model)
		return strategies, nil
	}

	// All OpenRouter models failed — ultimate fallback to Claude Code CLI
	log.Printf("[AI] All OpenRouter models failed, falling back to Claude Code CLI...")
	
	prompt, err := g.buildPrompt(count, symbol, topStrategies)
	if err != nil {
		return nil, fmt.Errorf("all models failed, prompt build also failed: %w", err)
	}

	response, err := g.callClaudeCodeCLI(ctx, prompt, "You are an expert quantitative trading strategist. Generate valid YAML trading strategies. Use ONLY simple expressions with single comparison operators. DO NOT use && or ||.")
	if err != nil {
		return nil, fmt.Errorf("all models failed, Claude Code CLI fallback also failed: %w (last OpenRouter err: %v)", err, lastErr)
	}

	g.modelPool.ResetRateLimits()

	strategies, err := g.parseStrategies(response)
	if err != nil {
		return nil, fmt.Errorf("Claude Code CLI response parse failed: %w", err)
	}

	if len(strategies) > count {
		strategies = strategies[:count]
	}

	for i, strategy := range strategies {
		strategy.ID = fmt.Sprintf("ai_gen_claude_%d_%d", time.Now().Unix(), i)
		strategy.Source = "ai:claude-code-cli"
		strategy.Generation = 1
	}

	log.Printf("[AI] Generated %d strategies using Claude Code CLI (ultimate fallback)", len(strategies))
	return strategies, nil
}

// callClaudeCodeCLI invokes Claude as ultimate fallback.
// It tries two methods in order:
//   1. Anthropic REST API (fastest, no subprocess overhead)
//   2. claude -p CLI command (requires `claude` installed and logged in)
func (g *Generator) callClaudeCodeCLI(ctx context.Context, prompt, systemPrompt string) (string, error) {
	// ── Method 1: Anthropic REST API ──
	log.Printf("[AI] Fallback attempt: Anthropic REST API...")
	if result, err := g.callAnthropicAPI(ctx, prompt, systemPrompt); err == nil {
		return result, nil
	} else {
		log.Printf("[AI] Anthropic API failed: %v, trying claude CLI...", err)
	}

	// ── Method 2: claude -p CLI ──
	log.Printf("[AI] Fallback attempt: claude -p CLI...")
	return g.callClaudeCLIPrint(ctx, prompt, systemPrompt)
}

// callAnthropicAPI calls Anthropic Messages API directly using stored/OAuth credentials.
func (g *Generator) callAnthropicAPI(ctx context.Context, prompt, systemPrompt string) (string, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		// Fallback: read from Claude config
		homeDir, _ := os.UserHomeDir()
		configPath := filepath.Join(homeDir, ".claude", "config.json")
		if data, err := os.ReadFile(configPath); err == nil {
			var cfg struct {
				PrimaryAPIKey string `json:"primaryApiKey"`
			}
			if json.Unmarshal(data, &cfg) == nil && cfg.PrimaryAPIKey != "" {
				apiKey = cfg.PrimaryAPIKey
			}
		}
		// Fallback 2: read OAuth access token from Claude credentials
		if apiKey == "" {
			credPath := filepath.Join(homeDir, ".claude", ".credentials.json")
			if data, err := os.ReadFile(credPath); err == nil {
				var creds struct {
					ClaudeAiOauth struct {
						AccessToken string `json:"accessToken"`
					} `json:"claudeAiOauth"`
				}
				if json.Unmarshal(data, &creds) == nil && creds.ClaudeAiOauth.AccessToken != "" {
					apiKey = creds.ClaudeAiOauth.AccessToken
				}
			}
		}
	}
	if apiKey == "" {
		return "", fmt.Errorf("ANTHROPIC_API_KEY not set, Claude config/OAuth not found")
	}

	messages := []map[string]string{
		{"role": "user", "content": prompt},
	}
	if systemPrompt != "" {
		messages = []map[string]string{
			{"role": "user", "content": "System instructions:\n" + systemPrompt + "\n\nNow respond to the following:"},
			{"role": "assistant", "content": "Understood. I will follow the system instructions."},
			{"role": "user", "content": prompt},
		}
	}

	reqBody := map[string]interface{}{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 4096,
		"messages":  messages,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	log.Printf("[AI] Calling Anthropic API (claude-sonnet-4)...")

	resp, err := g.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Anthropic API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Anthropic API error %d: %s", resp.StatusCode, string(body))
	}

	var anthResp struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}

	if err := json.Unmarshal(body, &anthResp); err != nil {
		return "", fmt.Errorf("failed to parse Anthropic response: %w", err)
	}

	if len(anthResp.Content) == 0 || anthResp.Content[0].Text == "" {
		return "", fmt.Errorf("empty response from Anthropic API")
	}

	log.Printf("[AI] Anthropic API response length: %d bytes", len(anthResp.Content[0].Text))
	return anthResp.Content[0].Text, nil
}

// callClaudeCLIPrint runs `claude -p "<prompt>"` as a subprocess and returns its output.
// This is the ultimate fallback — it works even without API keys as long as `claude` is logged in.
func (g *Generator) callClaudeCLIPrint(ctx context.Context, prompt, systemPrompt string) (string, error) {
	// Determine the claude binary name
 claudeBin := "claude"
	if runtime.GOOS == "windows" {
 claudeBin = "claude.cmd"
	}

	// Use a temp file for the prompt to avoid shell escaping issues
	tmpFile, err := os.CreateTemp("", "claude-evolver-*.txt")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.WriteString(prompt); err != nil {
 tmpFile.Close()
		return "", fmt.Errorf("failed to write prompt to temp file: %w", err)
	}
	tmpFile.Close()

	// Build command: claude -p "$(cat tmpFile)" --system-prompt "..." --output-format text
	args := []string{"-p", prompt, "--output-format", "text"}
	if systemPrompt != "" {
		args = append(args, "--system-prompt", systemPrompt)
	}

	log.Printf("[AI] Running claude CLI print mode...")

	cmd := exec.CommandContext(ctx, claudeBin, args...)
	cmd.Env = os.Environ()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		return "", fmt.Errorf("claude CLI failed: %w, stderr: %s", err, stderr.String())
	}

	result := strings.TrimSpace(stdout.String())
	if result == "" {
		return "", fmt.Errorf("claude CLI returned empty output, stderr: %s", stderr.String())
	}

	log.Printf("[AI] Claude CLI response length: %d bytes", len(result))
	return result, nil
}

// WebSearch performs a web search using AI (with plugins)
func (g *Generator) WebSearch(ctx context.Context, query string) (string, error) {
	var lastErr error
	
	// Try each model until one succeeds
	for i := 0; i < len(g.modelPool.models); i++ {
		model := g.modelPool.Next()
		
		response, err := g.callAIWithWebSearch(ctx, model, query)
		if err != nil {
			lastErr = err
			if isRateLimited(err) {
				g.modelPool.MarkRateLimited(model)
				continue
			}
			continue
		}

		return response, nil
	}

	return "", fmt.Errorf("web search failed on all models: %w", lastErr)
}

// callAI calls the OpenRouter API (without web search)
func (g *Generator) callAI(ctx context.Context, model, prompt string, webSearch bool) (string, error) {
	reqBody := AIRequest{
		Model: model,
		Messages: []Message{
			{
				Role:    "system",
				Content: "You are an expert quantitative trading strategist. Generate valid YAML trading strategies.",
			},
			{
				Role:    "user",
				Content: prompt,
			},
		},
		MaxTokens:   4000,
		Temperature: 0.8,
	}

	return g.doRequest(ctx, reqBody)
}

// callAIWithWebSearch calls the OpenRouter API with web search plugin
func (g *Generator) callAIWithWebSearch(ctx context.Context, model, query string) (string, error) {
	reqBody := AIRequest{
		Model: model,
		Messages: []Message{
			{
				Role:    "user",
				Content: query,
			},
		},
		MaxTokens:   2000,
		Temperature: 0.3,
		Plugins: []Plugin{
			{ID: "web"},
		},
	}

	return g.doRequest(ctx, reqBody)
}

// doRequest performs the HTTP request to OpenRouter
func (g *Generator) doRequest(ctx context.Context, reqBody AIRequest) (string, error) {
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", g.baseURL+"/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.apiKey)
	req.Header.Set("HTTP-Referer", "https://dsl-strategy-evolver.local")
	req.Header.Set("X-Title", "DSL Strategy Evolver")

	resp, err := g.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var aiResp AIResponse
	if err := json.Unmarshal(body, &aiResp); err != nil {
		return "", err
	}

	if len(aiResp.Choices) == 0 {
		return "", fmt.Errorf("no response from AI")
	}

	return aiResp.Choices[0].Message.Content, nil
}

// isRateLimited checks if the error is a rate limit error
func isRateLimited(err error) bool {
	return err != nil && (contains(err.Error(), "429") || contains(err.Error(), "rate"))
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// buildPrompt builds the AI prompt
func (g *Generator) buildPrompt(count int, symbol string, topStrategies []*dsl.Strategy) (string, error) {
	promptTemplate := `
You are a quantitative trading strategy designer. Generate {{.Count}} unique trading strategies for crypto perpetual futures.

Symbol: {{.Symbol}}

IMPORTANT: Use ONLY simple expressions with single comparison operators. DO NOT use && or ||.

Requirements:
1. Use technical indicators: EMA, SMA, RSI
2. Use SINGLE comparison per entry/exit (price < ema(20) is OK, but NOT price < ema(20) * 0.99 * 0.5)
3. Stop-loss: 0.01 to 0.05 (1-5%)
4. Position size: 50-200 USDT
5. Max positions: 1-2

Available operators (use ONLY these):
- Comparison: <, >, <=, >= (use ONE per expression)

Available variables:
- price: Current price
- ema(period): EMA (e.g., ema(20))
- sma(period): SMA (e.g., sma(50))
- rsi(period): RSI (e.g., rsi(14))

Output YAML format (generate exactly {{.Count}} strategies):
name: "SimpleEMALong"
symbol: "{{.Symbol}}"
type: "hedge"
long:
  entry: "price < ema(20)"
  exit: "price > ema(20)"
  stop_loss: 0.02
short:
  entry: "price > ema(20)"
  exit: "price < ema(20)"
  stop_loss: 0.02
risk:
  position_size: 100
  max_positions: 2

Generate {{.Count}} different strategies with SIMPLE single-comparison expressions.
`

	tmpl, err := template.New("prompt").Parse(promptTemplate)
	if err != nil {
		return "", err
	}

	data := struct {
		Count          int
		Symbol         string
		TopStrategies  []*dsl.Strategy
	}{
		Count:         count,
		Symbol:        symbol,
		TopStrategies: topStrategies,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// parseStrategies parses strategies from AI response
func (g *Generator) parseStrategies(response string) ([]*dsl.Strategy, error) {
	// Extract YAML from markdown code blocks if present
	yamlContent := extractYAMLFromMarkdown(response)
	
	// Split by YAML document separator
	parts := bytes.Split([]byte(yamlContent), []byte("---"))

	var strategies []*dsl.Strategy
	parser := dsl.NewParser()

	for _, part := range parts {
		part = bytes.TrimSpace(part)
		if len(part) == 0 {
			continue
		}

		strategy, err := parser.Parse(part)
		if err != nil {
			// Skip invalid strategies
			log.Printf("[AI] Failed to parse strategy: %v", err)
			continue
		}

		strategies = append(strategies, strategy)
	}

	if len(strategies) == 0 {
		return nil, fmt.Errorf("no valid strategies found in response")
	}

	return strategies, nil
}

// extractYAMLFromMarkdown extracts YAML content from markdown code blocks
func extractYAMLFromMarkdown(response string) string {
	// Look for ```yaml ... ``` blocks
	re := regexp.MustCompile("(?s)```ya?ml?\\s*\n(.*?)```")
	matches := re.FindAllStringSubmatch(response, -1)
	
	if len(matches) > 0 {
		// Combine all YAML blocks
		var yamlParts []string
		for _, match := range matches {
			if len(match) > 1 {
				yamlParts = append(yamlParts, strings.TrimSpace(match[1]))
			}
		}
		return strings.Join(yamlParts, "\n---\n")
	}
	
	// No code blocks, return as-is
	return response
}

// AIRequest represents an AI API request
type AIRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	Plugins     []Plugin  `json:"plugins,omitempty"`
}

// Plugin represents a plugin to enable
type Plugin struct {
	ID string `json:"id"`
}

// Message represents a chat message
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// AIResponse represents an AI API response
type AIResponse struct {
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice represents a response choice
type Choice struct {
	Message Message `json:"message"`
}

// Usage represents token usage
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ============================================================================
// Multi-Model Thinking/Reasoning (Quant-Autoresearch inspired)
// ============================================================================

// ThinkingResult represents the result of thinking phase
type ThinkingResult struct {
	Hypothesis  string   `json:"hypothesis"`
	Approach    string   `json:"approach"`
	Keywords    []string `json:"keywords"`
	Confidence  float64  `json:"confidence"`
}

// ThinkingPhase generates hypotheses using a fast model
func (g *Generator) ThinkingPhase(ctx context.Context, currentSituation string) (*ThinkingResult, error) {
	// Use fast model for thinking (first model in pool)
	thinkingModel := g.modelPool.models[0]
	
	prompt := fmt.Sprintf(`
You are a quantitative researcher analyzing trading strategy performance.

CURRENT SITUATION:
%s

Based on the current situation, propose:
1. A hypothesis for improving the strategy
2. An approach to test this hypothesis
3. Key technical concepts to explore
4. Confidence level (0.0-1.0)

Output in JSON format:
{
  "hypothesis": "...",
  "approach": "...",
  "keywords": ["..."],
  "confidence": 0.8
}
`, currentSituation)

	response, err := g.callAI(ctx, thinkingModel, prompt, false)
	if err != nil {
		return nil, fmt.Errorf("thinking phase failed: %w", err)
	}

	// Parse JSON response
	var result ThinkingResult
	jsonStr := extractJSON(response)
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		// Fallback to basic result
		return &ThinkingResult{
			Hypothesis: "Improve strategy based on momentum",
			Approach:   "Test EMA crossover with RSI filter",
			Keywords:   []string{"momentum", "ema", "rsi"},
			Confidence: 0.5,
		}, nil
	}

	return &result, nil
}

// ReasoningPhase generates strategies using a strong model
func (g *Generator) ReasoningPhase(ctx context.Context, thinking *ThinkingResult, symbol string, constitution map[string]interface{}) ([]*dsl.Strategy, error) {
	// Use strong model for reasoning (second model in pool, or first if only one)
	reasoningModel := g.modelPool.models[0]
	if len(g.modelPool.models) > 1 {
		reasoningModel = g.modelPool.models[1]
	}

	// Build prompt with thinking result
	prompt := g.buildReasoningPrompt(thinking, symbol, constitution)

	response, err := g.callAI(ctx, reasoningModel, prompt, false)
	if err != nil {
		return nil, fmt.Errorf("reasoning phase failed: %w", err)
	}

	return g.parseStrategies(response)
}

// buildReasoningPrompt builds a prompt for the reasoning phase
func (g *Generator) buildReasoningPrompt(thinking *ThinkingResult, symbol string, constitution map[string]interface{}) string {
	maxPositionSize := 200.0
	if v, ok := constitution["risk_limits"].(map[string]interface{}); ok {
		if ms, ok := v["max_position_size"].(float64); ok {
			maxPositionSize = ms
		}
	}

	return fmt.Sprintf(`
You are a quantitative trading strategy designer. Generate 3 unique trading strategies.

HYPOTHESIS: %s
APPROACH: %s
KEYWORDS: %v
SYMBOL: %s
MAX POSITION SIZE: %.0f USDT

IMPORTANT CONSTRAINTS:
1. Use ONLY simple expressions with single comparison operators
2. DO NOT use && or ||
3. DO NOT use shift(-1) or look-ahead patterns
4. DO NOT use martingale or averaging down
5. Stop-loss: 0.01 to 0.05 (1-5%%)
6. Position size: 50 to %.0f USDT
7. Max positions: 1-2

Available indicators:
- ema(period): Exponential Moving Average
- sma(period): Simple Moving Average
- rsi(period): Relative Strength Index
- atr(period): Average True Range

Available operators (use ONE per expression):
- Comparison: <, >, <=, >=

Output YAML format for each strategy:
---
name: "StrategyName"
symbol: "%s"
type: "hedge"
long:
  entry: "price < ema(20)"
  exit: "price > ema(20)"
  stop_loss: 0.02
short:
  entry: "price > ema(20)"
  exit: "price < ema(20)"
  stop_loss: 0.02
risk:
  position_size: 100
  max_positions: 2
---

Generate 3 different strategies based on the hypothesis.
`, thinking.Hypothesis, thinking.Approach, thinking.Keywords, symbol, maxPositionSize, maxPositionSize, symbol)
}

// extractJSON extracts JSON from text
func extractJSON(text string) string {
	// Try to find JSON in code blocks
	re := regexp.MustCompile("(?s)```json\\s*\n(.*?)```")
	matches := re.FindAllStringSubmatch(text, -1)
	if len(matches) > 0 && len(matches[0]) > 1 {
		return matches[0][1]
	}

	// Try to find JSON directly
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start != -1 && end != -1 && end > start {
		return text[start : end+1]
	}

	return "{}"
}

// GenerateWithThinking generates strategies using the thinking/reasoning pattern
func (g *Generator) GenerateWithThinking(ctx context.Context, symbol, currentSituation string, constitution map[string]interface{}) ([]*dsl.Strategy, error) {
	// Phase 1: Thinking
	thinking, err := g.ThinkingPhase(ctx, currentSituation)
	if err != nil {
		log.Printf("[AI] Thinking phase failed: %v, using fallback", err)
		thinking = &ThinkingResult{
			Hypothesis: "Test momentum-based strategy",
			Approach:   "EMA crossover with RSI filter",
			Keywords:   []string{"momentum", "ema", "rsi"},
			Confidence: 0.5,
		}
	}

	log.Printf("[AI] Thinking: hypothesis=%s, confidence=%.2f", thinking.Hypothesis, thinking.Confidence)

	// Phase 2: Reasoning
	strategies, err := g.ReasoningPhase(ctx, thinking, symbol, constitution)
	if err != nil {
		return nil, fmt.Errorf("reasoning phase failed: %w", err)
	}

	log.Printf("[AI] Generated %d strategies via thinking/reasoning", len(strategies))
	return strategies, nil
}

// GetModelStats returns model usage statistics
func (g *Generator) GetModelStats() map[string]interface{} {
	return map[string]interface{}{
		"available_models": g.modelPool.models,
		"rate_limited":     g.modelPool.rateLimited,
		"current_index":    g.modelPool.current,
	}
}
