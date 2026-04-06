package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	"dsl-strategy-evolver/internal/dsl"
)

// AnalystRole defines the persona of a multi-agent participant
type AnalystRole string

const (
	RoleTechnical AnalystRole = "technical"  // Chart patterns, indicators
	RoleSentiment  AnalystRole = "sentiment"   // Volume patterns, fear/greed
	RoleRiskMgmt   AnalystRole = "risk_mgmt"   // Defensive, capital preservation (2x weight)
	RoleMacro       AnalystRole = "macro"       // Trend direction, market cycles
)

// AgentVerdict is one agent's evaluation and recommendation
type AgentVerdict struct {
	Role      AnalystRole `json:"role"`
	Action    string    `json:"action"`     // GENERATE/PAUSE/STOP
	Reasoning string    `json:"reasoning"`
	Confidence float64  `json:"confidence"` // 0-1
	Score     float64   `json:"score"`      // 0-10 quality assessment
}

// ConsensusResult is the final decision after multi-agent voting
func (c *ConsensusResult) Action() string {
	if c == nil {
		return "GENERATE"
	}
	return c.FinalAction
}

// ConsensusResult is the output of multi-agent consensus
type ConsensusResult struct {
	Verdicts      []*AgentVerdict `json:"verdicts"`
	FinalAction   string          `json:"final_action"`    // GENERATE/PAUSE/STOP
	Agreement     float64         `json:"agreement"`       // 0-1 consensus level
	StrategyCount int             `json:"strategy_count"`  // adjusted count based on risk
	Reason        string          `json:"reason"`
}

// MultiAgentGenerator uses multiple AI personas to generate strategies with consensus
type MultiAgentGenerator struct {
	generator *Generator
	agents    []*AnalystAgent
}

// AnalystAgent represents one expert persona
type AnalystAgent struct {
	Role       AnalystRole
	Weight     float64 // voting weight
	SystemPrompt string
}

// NewMultiAgentGenerator creates a multi-agent generator with 4 analysts
func NewMultiAgentGenerator(g *Generator) *MultiAgentGenerator {
	return &MultiAgentGenerator{
		generator: g,
		agents: []*AnalystAgent{
			{
				Role:   RoleTechnical,
				Weight: 1.0,
				SystemPrompt: `You are a technical analysis expert. Evaluate trading strategies based on:
- EMA/SMA crossover patterns
- RSI overbought/oversold signals
- ATR-based volatility assessment
- Bollinger Band squeeze/expansion

Focus on chart pattern quality and indicator signal reliability.
Output JSON: {"action":"GENERATE|PAUSE|STOP","reasoning":"...","confidence":0.0-1.0,"score":0-10}`,
			},
			{
				Role:   RoleSentiment,
				Weight: 1.0,
				SystemPrompt: `You are a market sentiment analyst. Evaluate based on:
- Volume pattern anomalies (climax, exhaustion)
- Fear/greed cycle positioning
- Price action sentiment (panic selling, euphoria)
- Order flow imbalances

Focus on market psychology and behavioral signals.
Output JSON: {"action":"GENERATE|PAUSE|STOP","reasoning":"...","confidence":0.0-1.0,"score":0-10}`,
			},
			{
				Role:   RoleRiskMgmt,
				Weight: 2.0, // 2x weight like SNOWBALL
				SystemPrompt: `You are a risk management specialist. Your priority is CAPITAL PRESERVATION.
Evaluate based on:
- Maximum possible loss scenarios
- Drawdown risk in current volatility
- Stop-loss adequacy
- Position sizing risk

You lean DEFENSIVE. When uncertain, recommend PAUSE or STOP.
Risk managers who recommend STOP get 2x voting weight.
Output JSON: {"action":"GENERATE|PAUSE|STOP","reasoning":"...","confidence":0.0-1.0,"score":0-10}`,
			},
			{
				Role:   RoleMacro,
				Weight: 1.0,
				SystemPrompt: `You are a macro strategist. Evaluate based on:
- Current trend direction (bull/bear/neutral)
- Market cycle position (accumulation/distribution)
- ADX trend strength
- Higher timeframe bias

Focus on whether this is the right time to generate new strategies.
Output JSON: {"action":"GENERATE|PAUSE|STOP","reasoning":"...","confidence":0.0-1.0,"score":0-10}`,
			},
		},
	}
}

// GenerateWithConsensus generates strategies using multi-agent consensus
// Flow: Each agent independently evaluates → Coordinator aggregates → Generate or Skip
func (m *MultiAgentGenerator) GenerateWithConsensus(
	ctx context.Context,
	symbol string,
	riskScore float64, // 0-100 from RiskScorer
	topStrategies []*dsl.Strategy,
) ([]*dsl.Strategy, *ConsensusResult, error) {

	// Step 1: Gather verdicts from all agents (parallel)
	verdicts := make([]*AgentVerdict, len(m.agents))
	var wg sync.WaitGroup
	errChan := make(chan error, len(m.agents))

	idx := 0
	for _, agent := range m.agents {
		wg.Add(1)
		currentIdx := idx
		go func(a *AnalystAgent) {
			defer wg.Done()
			verdict, err := m.getAgentVerdict(ctx, a, symbol, riskScore, topStrategies)
			if err != nil {
				log.Printf("[MultiAgent] %s agent failed: %v", a.Role, err)
				verdicts[currentIdx] = &AgentVerdict{
					Role:      a.Role,
					Action:    "PAUSE",
					Reasoning: fmt.Sprintf("Agent failed: %v", err),
					Confidence: 0.3,
					Score:     5.0,
				}
				return
			}
			verdicts[currentIdx] = verdict
		}(agent)
		idx++
	}
	wg.Wait()
	close(errChan)

	// Log errors
	for err := range errChan {
		log.Printf("[MultiAgent] Error: %v", err)
	}

	// Step 2: Coordinator aggregates verdicts (SNOWBALL consensus rules)
	result := m.coordinate(verdicts, riskScore)

	log.Printf("[MultiAgent] Consensus: action=%s agreement=%.2f strategies=%d reason=%s",
		result.FinalAction, result.Agreement, result.StrategyCount, result.Reason)

	// Step 3: If GENERATE, produce strategies
	if result.FinalAction != "GENERATE" {
		return nil, result, nil
	}

	// Adjust generation count based on agreement and risk
	count := result.StrategyCount
	if count <= 0 {
		count = 1
	}

	strategies, err := m.generator.GenerateStrategies(ctx, count, symbol, topStrategies)
	if err != nil {
		return nil, result, fmt.Errorf("strategy generation failed: %w", err)
	}

	return strategies, result, nil
}

// getAgentVerdict asks one agent for its evaluation
func (m *MultiAgentGenerator) getAgentVerdict(
	ctx context.Context,
	agent *AnalystAgent,
	symbol string,
	riskScore float64,
	topStrategies []*dsl.Strategy,
) (*AgentVerdict, error) {

	prompt := m.buildAgentPrompt(agent, symbol, riskScore, topStrategies)

	model := m.generator.modelPool.Next()
	response, err := m.generator.callAI(ctx, model, prompt, false)
	if err != nil {
		// If OpenRouter failed (rate limit or other error), try Claude Code CLI fallback
		m.generator.modelPool.MarkRateLimited(model)
		if isRateLimited(err) {
			log.Printf("[MultiAgent] %s agent: OpenRouter rate limited, trying Claude Code CLI fallback...", agent.Role)
		} else {
			log.Printf("[MultiAgent] %s agent: OpenRouter error, trying Claude Code CLI fallback: %v", agent.Role, err)
		}
		response, err = m.generator.callClaudeCodeCLI(ctx, prompt, a.SystemPrompt)
		if err != nil {
			return nil, fmt.Errorf("Claude Code CLI fallback also failed for %s agent: %w", agent.Role, err)
		}
		m.generator.modelPool.ResetRateLimits()
	}

	verdict := parseAgentVerdict(response, agent.Role)
	return verdict, nil
}

// buildAgentPrompt creates the evaluation prompt for an agent
func (m *MultiAgentGenerator) buildAgentPrompt(
	agent *AnalystAgent,
	symbol string,
	riskScore float64,
	topStrategies []*dsl.Strategy,
) string {
	topInfo := ""
	if len(topStrategies) > 0 {
		topInfo = fmt.Sprintf("\nCurrent best strategies:\n")
		for i, s := range topStrategies[:minInt(3, len(topStrategies))] {
			topInfo += fmt.Sprintf("  %d. %s\n", i+1, s.Name)
		}
	}

	return fmt.Sprintf(`MARKET RISK SCORE: %.0f/100
SYMBOL: %s
CURRENT DATE: %s
%s

Given this market context, should we GENERATE new trading strategies, PAUSE generation, or STOP entirely?

Respond ONLY with valid JSON:
{"action":"GENERATE|PAUSE|STOP","reasoning":"brief explanation","confidence":0.0-1.0,"score":0-10}`, 
		riskScore, symbol, "2026-03-27", topInfo)
}

// coordinate implements SNOWBALL consensus rules:
// 1. 3/4+ agree → adopt majority action
// 2. Risk manager STOP/PAUSE → 2x weight
// 3. Disagreement → most defensive action
// 4. Low average confidence → PAUSE (uncertainty)
func (m *MultiAgentGenerator) coordinate(verdicts []*AgentVerdict, riskScore float64) *ConsensusResult {
	result := &ConsensusResult{
		Verdicts: verdicts,
	}

	// Weighted vote counting
	actions := map[string]float64{"GENERATE": 0, "PAUSE": 0, "STOP": 0}
	totalWeight := 0.0
	confidenceSum := 0.0

	for _, v := range verdicts {
		if v == nil {
			continue
		}
		weight := 1.0
		for _, agent := range m.agents {
			if agent.Role == v.Role {
				weight = agent.Weight
				break
			}
		}
		// Risk manager gets 2x weight on defensive actions
		if v.Role == RoleRiskMgmt && (v.Action == "STOP" || v.Action == "PAUSE") {
			weight *= 2.0
		}

		actions[v.Action] += weight
		totalWeight += weight
		confidenceSum += v.Confidence
	}

	// Determine final action
	maxWeight := 0.0
	winner := "PAUSE" // default defensive
	for action, weight := range actions {
		if weight > maxWeight {
			maxWeight = weight
			winner = action
		}
	}

	// Agreement level
	if totalWeight > 0 {
		result.Agreement = maxWeight / totalWeight
	}

	// Average confidence
	avgConfidence := 0.0
	if len(verdicts) > 0 {
		avgConfidence = confidenceSum / float64(len(verdicts))
	}

	// Rule 4: Low confidence → PAUSE
	if avgConfidence < 0.4 {
		winner = "PAUSE"
		result.Reason = fmt.Sprintf("Low confidence (%.2f)", avgConfidence)
	}

	// Rule 3: Disagreement (< 0.5 agreement) → most defensive action present
	if result.Agreement < 0.5 {
		if actions["STOP"] > 0 {
			winner = "STOP"
		} else if actions["PAUSE"] > 0 {
			winner = "PAUSE"
		}
		result.Reason = fmt.Sprintf("Disagreement (agreement=%.2f)", result.Agreement)
	}

	// Rule 2 (already handled via weight doubling above)

	result.FinalAction = winner

	// Calculate strategy count based on risk
	switch winner {
	case "GENERATE":
		// Scale by agreement and risk score
		baseCount := 20
		if result.Agreement > 0.75 {
			baseCount = 20 // high consensus
		} else if result.Agreement > 0.5 {
			baseCount = 10 // moderate consensus
		} else {
			baseCount = 5 // low consensus
		}

		// Reduce by risk score
		if riskScore > 50 {
			baseCount = int(float64(baseCount) * 0.5)
		}
		result.StrategyCount = max(baseCount, 1)

	case "PAUSE":
		result.StrategyCount = 0
		if result.Reason == "" {
			result.Reason = "Agents recommend waiting"
		}

	case "STOP":
		result.StrategyCount = 0
		if result.Reason == "" {
			result.Reason = "Agents recommend emergency stop"
		}
	}

	return result
}

// parseAgentVerdict extracts a verdict from agent response
func parseAgentVerdict(response string, role AnalystRole) *AgentVerdict {
	verdict := &AgentVerdict{Role: role}

	// Try to parse JSON
	jsonStr := extractJSON(response)
	if err := json.Unmarshal([]byte(jsonStr), verdict); err != nil {
		// Fallback: heuristic parsing
		upper := strings.ToUpper(response)
		switch {
		case strings.Contains(upper, "STOP"):
			verdict.Action = "STOP"
		case strings.Contains(upper, "PAUSE"):
			verdict.Action = "PAUSE"
		default:
			verdict.Action = "GENERATE"
		}
		verdict.Confidence = 0.5
		verdict.Score = 5.0
		verdict.Reasoning = "parsed from text"
	}

	// Validate action
	switch verdict.Action {
	case "GENERATE", "PAUSE", "STOP":
		// valid
	default:
		verdict.Action = "PAUSE"
	}

	// Clamp confidence and score
	if verdict.Confidence < 0 {
		verdict.Confidence = 0
	}
	if verdict.Confidence > 1 {
		verdict.Confidence = 1
	}
	if verdict.Score < 0 {
		verdict.Score = 0
	}
	if verdict.Score > 10 {
		verdict.Score = 10
	}

	return verdict
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
