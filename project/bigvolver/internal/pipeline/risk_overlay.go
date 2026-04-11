package pipeline

import (
	"math"
	"sync"
)

// RiskConfig defines risk constraints for the portfolio
type RiskConfig struct {
	MaxPositionSize   float64 // single position max weight (default 0.3)
	MaxTotalExposure  float64 // total leverage limit (default 1.0)
	MaxNetExposure    float64 // net directional limit (default 0.5)
	StopLossPct       float64 // per-position stop loss % (default 2.0)
	MaxDrawdownPct    float64 // portfolio max DD % (default 15.0)
	HighVolMultiplier float64 // weight reduction for high-vol assets (default 0.5)
	HighVolATRMultiplier float64 // ATR threshold for high-vol (default 2.0)
}

// DefaultRiskConfig returns sensible defaults
func DefaultRiskConfig() RiskConfig {
	return RiskConfig{
		MaxPositionSize:   0.3,
		MaxTotalExposure:  1.0,
		MaxNetExposure:    0.5,
		StopLossPct:       2.0,
		MaxDrawdownPct:    15.0,
		HighVolMultiplier: 0.5,
		HighVolATRMultiplier: 2.0,
	}
}

// RiskOverlay applies risk constraints to the weight vector
type RiskOverlay struct {
	config      RiskConfig
	equityCurve []float64
	peakEquity  float64
	mu          sync.RWMutex
}

// NewRiskOverlay creates a new risk overlay
func NewRiskOverlay(opts ...RiskOption) *RiskOverlay {
	r := &RiskOverlay{
		config: DefaultRiskConfig(),
		equityCurve: []float64{10000}, // Starting equity
		peakEquity: 10000,
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// RiskOption configures the risk overlay
type RiskOption func(*RiskOverlay)

// WithRiskConfig sets the risk configuration
func WithRiskConfig(cfg RiskConfig) RiskOption {
	return func(r *RiskOverlay) { r.config = cfg }
}

// WithStartingEquity sets the initial equity
func WithStartingEquity(e float64) RiskOption {
	return func(r *RiskOverlay) {
		r.equityCurve = []float64{e}
		r.peakEquity = e
	}
}

// Name implements PipelineStage
func (r *RiskOverlay) Name() string {
	return "risk_overlay"
}

// Process applies all risk rules to the weight vector
func (r *RiskOverlay) Process(wv *WeightVector) (*WeightVector, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Save raw score before risk adjustments
	wv.RawScore = wv.TotalExposure()

	// 1. Position size limit
	r.applyPositionSizeLimit(wv)

	// 2. Total exposure limit
	r.applyTotalExposureLimit(wv)

	// 3. Net exposure limit
	r.applyNetExposureLimit(wv)

	// 4. High volatility reduction
	r.applyHighVolReduction(wv)

	// 5. Max drawdown check
	r.applyMaxDrawdownReduction(wv)

	// 6. Re-normalize after all adjustments
	wv.NormalizeWeights()

	wv.FinalScore = wv.TotalExposure()
	wv.TotalRisk = r.calculatePortfolioRisk(wv)

	return wv, nil
}

// applyPositionSizeLimit caps individual position weights
func (r *RiskOverlay) applyPositionSizeLimit(wv *WeightVector) {
	maxSize := r.config.MaxPositionSize
	for i := range wv.Weights {
		if wv.Weights[i].Weight > maxSize {
			wv.Weights[i].Weight = maxSize
		} else if wv.Weights[i].Weight < -maxSize {
			wv.Weights[i].Weight = -maxSize
		}
	}
}

// applyTotalExposureLimit scales all weights if total exposure exceeds limit
func (r *RiskOverlay) applyTotalExposureLimit(wv *WeightVector) {
	maxExposure := r.config.MaxTotalExposure
	currentExposure := wv.TotalExposure()

	if currentExposure <= maxExposure {
		return
	}

	scaler := maxExposure / currentExposure
	for i := range wv.Weights {
		wv.Weights[i].Weight *= scaler
	}
}

// applyNetExposureLimit adjusts weights to limit net directional exposure
func (r *RiskOverlay) applyNetExposureLimit(wv *WeightVector) {
	maxNet := r.config.MaxNetExposure
	netExposure := wv.NetExposure()

	if math.Abs(netExposure) <= maxNet {
		return
	}

	// Scale down both long and short proportionally
	if netExposure > maxNet {
		// Too long: reduce longs
		reductionFactor := (netExposure - maxNet) / netExposure * 0.5
		for i := range wv.Weights {
			if wv.Weights[i].Weight > 0 {
				wv.Weights[i].Weight *= (1 - reductionFactor)
			}
		}
	} else if netExposure < -maxNet {
		// Too short: reduce shorts
		reductionFactor := (math.Abs(netExposure) - maxNet) / math.Abs(netExposure) * 0.5
		for i := range wv.Weights {
			if wv.Weights[i].Weight < 0 {
				wv.Weights[i].Weight *= (1 - reductionFactor)
			}
		}
	}
}

// applyHighVolReduction reduces weights for high-volatility assets
func (r *RiskOverlay) applyHighVolReduction(wv *WeightVector) {
	multiplier := r.config.HighVolMultiplier
	atrThreshold := r.config.HighVolATRMultiplier

	// Calculate average ATR across all positions
	var avgATR float64
	count := 0
	for _, w := range wv.Weights {
		if atr, ok := w.Metadata["atr_14"].(float64); ok && atr > 0 {
			avgATR += atr
			count++
		}
	}
	if count == 0 {
		return
	}
	avgATR /= float64(count)

	// Reduce weights for assets with ATR > threshold * average
	for i := range wv.Weights {
		if atr, ok := wv.Weights[i].Metadata["atr_14"].(float64); ok {
			if atr > avgATR*atrThreshold {
				wv.Weights[i].Weight *= multiplier
			}
		}
	}
}

// applyMaxDrawdownReduction halves all weights if DD exceeds limit
func (r *RiskOverlay) applyMaxDrawdownReduction(wv *WeightVector) {
	if len(r.equityCurve) < 2 {
		return
	}

	currentEquity := r.equityCurve[len(r.equityCurve)-1]
	if currentEquity > r.peakEquity {
		r.peakEquity = currentEquity
	}

	drawdownPct := (r.peakEquity - currentEquity) / r.peakEquity * 100

	if drawdownPct > r.config.MaxDrawdownPct {
		// Halve all weights
		for i := range wv.Weights {
			wv.Weights[i].Weight *= 0.5
		}
	}
}

// calculatePortfolioRisk computes a 0-1 risk score for the portfolio
func (r *RiskOverlay) calculatePortfolioRisk(wv *WeightVector) float64 {
	// Simple risk score based on concentration and exposure
	risk := 0.0

	// Concentration risk: Herfindahl index
	for _, w := range wv.Weights {
		risk += w.Weight * w.Weight
	}

	// Exposure risk
	exposure := wv.TotalExposure()
	if exposure > 0.8 {
		risk += 0.2
	}

	// Net directionality risk
	net := math.Abs(wv.NetExposure())
	if net > 0.5 {
		risk += 0.1 * (net / 0.5)
	}

	// Clamp to [0, 1]
	if risk > 1.0 {
		risk = 1.0
	}

	return risk
}

// UpdateEquity updates the equity curve (call after each period's PnL realization)
func (r *RiskOverlay) UpdateEquity(newEquity float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.equityCurve = append(r.equityCurve, newEquity)
	if newEquity > r.peakEquity {
		r.peakEquity = newEquity
	}
}

// CurrentDrawdown returns the current drawdown percentage
func (r *RiskOverlay) CurrentDrawdown() float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.equityCurve) < 2 {
		return 0
	}

	current := r.equityCurve[len(r.equityCurve)-1]
	if r.peakEquity == 0 {
		return 0
	}

	return (r.peakEquity - current) / r.peakEquity * 100
}
