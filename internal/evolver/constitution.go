package evolver

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Constitution defines immutable constraints for strategy evolution
type Constitution struct {
	Mandate      string                 `yaml:"mandate"`
	RiskLimits   RiskLimits             `yaml:"risk_limits"`
	Forbidden    []ForbiddenPattern     `yaml:"forbidden_patterns"`
	AllowedInds  []AllowedIndicator     `yaml:"allowed_indicators"`
	Goal         Goal                   `yaml:"goal"`
	Evolution    EvolutionParams        `yaml:"evolution"`
	AI           AIParams               `yaml:"ai"`
	Safety       SafetyParams           `yaml:"safety"`
	Market       MarketParams           `yaml:"market"`
}

type RiskLimits struct {
	MaxDrawdown       float64 `yaml:"max_drawdown"`
	MaxLeverage       float64 `yaml:"max_leverage"`
	MaxPositionSize   float64 `yaml:"max_position_size"`
	MinTrades         int     `yaml:"min_trades"`
	MaxPositions      int     `yaml:"max_positions"`
	StopLossRange     [2]float64 `yaml:"stop_loss_range"`
	TakeProfitRange   [2]float64 `yaml:"take_profit_range"`
}

type ForbiddenPattern struct {
	Pattern  string `yaml:"pattern"`
	Reason   string `yaml:"reason"`
	Severity string `yaml:"severity"` // critical, high, medium, low
	regex    *regexp.Regexp
}

type AllowedIndicator struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Parameters  []string `yaml:"parameters"`
	Example     string   `yaml:"example"`
}

type Goal struct {
	Metric           string           `yaml:"metric"`
	Threshold        float64          `yaml:"threshold"`
	SuccessConditions []SuccessCondition `yaml:"success_conditions"`
}

type SuccessCondition struct {
	Metric   string  `yaml:"metric"`
	Operator string  `yaml:"operator"`
	Value    float64 `yaml:"value"`
}

type EvolutionParams struct {
	Interval             string  `yaml:"interval"`
	StrategiesToGenerate int     `yaml:"strategies_to_generate"`
	ReplaceBottomPercent float64 `yaml:"replace_bottom_percent"`
	MaxStrategies        int     `yaml:"max_strategies"`
	BacktestDays         int     `yaml:"backtest_days"`
}

type AIParams struct {
	Models         []string `yaml:"models"`
	Rotation       string   `yaml:"rotation"`
	RateLimitWait  int      `yaml:"rate_limit_wait"`
	MaxRetries     int      `yaml:"max_retries"`
	Temperature    float64  `yaml:"temperature"`
	MaxTokens      int      `yaml:"max_tokens"`
}

type SafetyParams struct {
	DoomLoopThreshold int    `yaml:"doom_loop_threshold"`
	ContextMaxPercent int    `yaml:"context_max_percent"`
	AutoRevert        bool   `yaml:"auto_revert"`
	RevertOnScoreDrop bool   `yaml:"revert_on_score_drop"`
	ApprovalMode      string `yaml:"approval_mode"`
}

type MarketParams struct {
	DefaultSymbol string   `yaml:"default_symbol"`
	Symbols       []string `yaml:"symbols"`
	Regimes       []string `yaml:"regimes"`
}

// Load loads constitution from YAML file
func Load(path string) (*Constitution, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read constitution file: %w", err)
	}

	var c Constitution
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("failed to parse constitution: %w", err)
	}

	// Compile regex patterns
	for i := range c.Forbidden {
		re, err := regexp.Compile(c.Forbidden[i].Pattern)
		if err != nil {
			log.Printf("[Constitution] Warning: invalid regex pattern '%s': %v, skipping", c.Forbidden[i].Pattern, err)
			continue
		}
		c.Forbidden[i].regex = re
	}

	return &c, nil
}

// Default returns default constitution
func Default() *Constitution {
	return &Constitution{
		Mandate: "Maximize Nunchi Score with strict risk control",
		RiskLimits: RiskLimits{
			MaxDrawdown:     0.20,
			MaxLeverage:     3.0,
			MaxPositionSize: 200,
			MinTrades:       10,
			MaxPositions:    2,
			StopLossRange:   [2]float64{0.01, 0.05},
			TakeProfitRange: [2]float64{0.02, 0.10},
		},
		Forbidden: []ForbiddenPattern{
			{Pattern: `shift\(-`, Reason: "look-ahead bias detected", Severity: "critical"},
			{Pattern: "martingale", Reason: "martingale betting prohibited", Severity: "critical"},
			{Pattern: "exec(", Reason: "dynamic code execution prohibited", Severity: "critical"},
			{Pattern: "eval(", Reason: "dynamic code execution prohibited", Severity: "critical"},
		},
		Goal: Goal{
			Metric:    "nunchi_score",
			Threshold: 0.5,
		},
		Evolution: EvolutionParams{
			Interval:             "1h",
			StrategiesToGenerate: 20,
			ReplaceBottomPercent: 20,
			MaxStrategies:        100,
			BacktestDays:         30,
		},
		AI: AIParams{
			Models: []string{
				"meta-llama/llama-3.2-3b-instruct:free",
				"google/gemma-3-27b-it:free",
			},
			Rotation:      "round-robin",
			RateLimitWait: 60,
			MaxRetries:    3,
			Temperature:   0.8,
			MaxTokens:     4000,
		},
		Safety: SafetyParams{
			DoomLoopThreshold:  3,
			ContextMaxPercent:  80,
			AutoRevert:         true,
			RevertOnScoreDrop:  true,
			ApprovalMode:       "semi",
		},
		Market: MarketParams{
			DefaultSymbol: "BTCUSDT",
			Symbols:       []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"},
		},
	}
}

// ValidateStrategy validates a strategy against constitution rules
func (c *Constitution) ValidateStrategy(code string) (bool, []string) {
	var violations []string

	for _, fp := range c.Forbidden {
		if fp.regex != nil && fp.regex.MatchString(code) {
			violations = append(violations, 
				fmt.Sprintf("[%s] %s: %s", fp.Severity, fp.Reason, fp.Pattern))
		}
	}

	return len(violations) == 0, violations
}

// ValidateRisk validates risk parameters
func (c *Constitution) ValidateRisk(positionSize, leverage float64, stopLoss, takeProfit float64) (bool, []string) {
	var violations []string

	if positionSize > c.RiskLimits.MaxPositionSize {
		violations = append(violations, 
			fmt.Sprintf("position_size %.2f exceeds max %.2f", positionSize, c.RiskLimits.MaxPositionSize))
	}

	if leverage > c.RiskLimits.MaxLeverage {
		violations = append(violations, 
			fmt.Sprintf("leverage %.2f exceeds max %.2f", leverage, c.RiskLimits.MaxLeverage))
	}

	if stopLoss < c.RiskLimits.StopLossRange[0] || stopLoss > c.RiskLimits.StopLossRange[1] {
		violations = append(violations, 
			fmt.Sprintf("stop_loss %.4f outside range [%.4f, %.4f]", 
				stopLoss, c.RiskLimits.StopLossRange[0], c.RiskLimits.StopLossRange[1]))
	}

	if takeProfit < c.RiskLimits.TakeProfitRange[0] || takeProfit > c.RiskLimits.TakeProfitRange[1] {
		violations = append(violations, 
			fmt.Sprintf("take_profit %.4f outside range [%.4f, %.4f]", 
				takeProfit, c.RiskLimits.TakeProfitRange[0], c.RiskLimits.TakeProfitRange[1]))
	}

	return len(violations) == 0, violations
}

// CheckSuccessConditions checks if metrics meet success conditions
func (c *Constitution) CheckSuccessConditions(metrics map[string]float64) (bool, []string) {
	var failures []string

	for _, cond := range c.Goal.SuccessConditions {
		value, ok := metrics[cond.Metric]
		if !ok {
			failures = append(failures, fmt.Sprintf("metric %s not found", cond.Metric))
			continue
		}

		switch cond.Operator {
		case ">":
			if !(value > cond.Value) {
				failures = append(failures, 
					fmt.Sprintf("%s (%.4f) not > %.4f", cond.Metric, value, cond.Value))
			}
		case ">=":
			if !(value >= cond.Value) {
				failures = append(failures, 
					fmt.Sprintf("%s (%.4f) not >= %.4f", cond.Metric, value, cond.Value))
			}
		case "<":
			if !(value < cond.Value) {
				failures = append(failures, 
					fmt.Sprintf("%s (%.4f) not < %.4f", cond.Metric, value, cond.Value))
			}
		case "<=":
			if !(value <= cond.Value) {
				failures = append(failures, 
					fmt.Sprintf("%s (%.4f) not <= %.4f", cond.Metric, value, cond.Value))
			}
		}
	}

	return len(failures) == 0, failures
}

// GetAllowedIndicatorNames returns list of allowed indicator names
func (c *Constitution) GetAllowedIndicatorNames() []string {
	names := make([]string, len(c.AllowedInds))
	for i, ind := range c.AllowedInds {
		names[i] = ind.Name
	}
	return names
}

// IsIndicatorAllowed checks if an indicator is allowed
func (c *Constitution) IsIndicatorAllowed(name string) bool {
	// If no indicators defined, allow all
	if len(c.AllowedInds) == 0 {
		return true
	}

	name = strings.ToLower(name)
	for _, ind := range c.AllowedInds {
		if strings.ToLower(ind.Name) == name {
			return true
		}
	}
	return false
}

// GetThinkingModel returns the model for thinking phase (first model)
func (c *Constitution) GetThinkingModel() string {
	if len(c.AI.Models) > 0 {
		return c.AI.Models[0]
	}
	return "meta-llama/llama-3.2-3b-instruct:free"
}

// GetReasoningModel returns the model for reasoning phase (second model or first if only one)
func (c *Constitution) GetReasoningModel() string {
	if len(c.AI.Models) > 1 {
		return c.AI.Models[1]
	}
	if len(c.AI.Models) > 0 {
		return c.AI.Models[0]
	}
	return "google/gemma-3-27b-it:free"
}
