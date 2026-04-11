package pipeline

import (
	"math"

	"dsl-strategy-evolver/internal/data"
)

// TimingConfig configures the KAMA-based timing module
type TimingConfig struct {
	KAMAPeriod     int     // KAMA lookback period (default 10)
	KAMAFastSC     float64 // fast smoothing constant (default 2/(2+1))
	KAMASlowSC     float64 // slow smoothing constant (default 2/(30+1))
	EntryBufferPct float64 // entry buffer zone % (default 0.1)
	ExitBufferPct  float64 // exit buffer zone % (default 0.2)
}

// DefaultTimingConfig returns sensible defaults
func DefaultTimingConfig() TimingConfig {
	return TimingConfig{
		KAMAPeriod:     10,
		KAMAFastSC:     2.0 / 3.0,    // 2/(2+1)
		KAMASlowSC:     2.0 / 31.0,   // 2/(30+1)
		EntryBufferPct: 0.1,
		ExitBufferPct:  0.2,
	}
}

// TimingModule adjusts entry/exit timing using KAMA
type TimingModule struct {
	config     TimingConfig
	candleRepo *data.MarketDataRepository
}

// NewTimingModule creates a new timing module
func NewTimingModule(repo *data.MarketDataRepository, opts ...TimingOption) *TimingModule {
	t := &TimingModule{
		config:     DefaultTimingConfig(),
		candleRepo: repo,
	}

	for _, opt := range opts {
		opt(t)
	}

	return t
}

// TimingOption configures the timing module
type TimingOption func(*TimingModule)

// WithTimingConfig sets the timing configuration
func WithTimingConfig(cfg TimingConfig) TimingOption {
	return func(t *TimingModule) { t.config = cfg }
}

// Name implements PipelineStage
func (t *TimingModule) Name() string {
	return "timing"
}

// Process adjusts weights based on KAMA timing signals
func (t *TimingModule) Process(wv *WeightVector) (*WeightVector, error) {
	for i := range wv.Weights {
		w := &wv.Weights[i]

		// Get candles for this symbol
		candles, err := t.candleRepo.GetLatestCandles(w.Symbol, t.config.KAMAPeriod+10)
		if err != nil || len(candles) < t.config.KAMAPeriod+1 {
			continue // Not enough data, keep weight as-is
		}

		// Calculate KAMA
		kama := calcKAMA(candles, t.config.KAMAPeriod, t.config.KAMAFastSC, t.config.KAMASlowSC)
		currentPrice := candles[len(candles)-1].Close

		if kama == 0 {
			continue
		}

		// Calculate buffer thresholds
		entryBuffer := kama * (t.config.EntryBufferPct / 100.0)
		exitBuffer := kama * (t.config.ExitBufferPct / 100.0)

		// Apply timing adjustment
		var timingMultiplier float64

		switch w.Signal {
		case "LONG":
			if currentPrice > kama+entryBuffer {
				// Strong bullish — maintain/increase weight
				timingMultiplier = 1.0
			} else if currentPrice > kama {
				// Weak bullish — slight reduction
				timingMultiplier = 0.8
			} else if currentPrice > kama-exitBuffer {
				// Neutral zone — significant reduction
				timingMultiplier = 0.3
			} else {
				// Below KAMA - exit zone — close position
				timingMultiplier = 0.0
			}

		case "SHORT":
			if currentPrice < kama-entryBuffer {
				// Strong bearish — maintain/increase weight
				timingMultiplier = 1.0
			} else if currentPrice < kama {
				// Weak bearish — slight reduction
				timingMultiplier = 0.8
			} else if currentPrice < kama+exitBuffer {
				// Neutral zone — significant reduction
				timingMultiplier = 0.3
			} else {
				// Above KAMA - exit zone — close position
				timingMultiplier = 0.0
			}

		default:
			// NEUTRAL — no timing adjustment
			timingMultiplier = 1.0
		}

		// Apply multiplier
		w.Weight *= timingMultiplier

		// Store KAMA value in metadata
		if w.Metadata == nil {
			w.Metadata = make(map[string]interface{})
		}
		w.Metadata["kama"] = kama
		w.Metadata["timing_multiplier"] = timingMultiplier
		w.Metadata["current_price"] = currentPrice
	}

	return wv, nil
}

// calcKAMA computes Kaufman's Adaptive Moving Average
func calcKAMA(candles []*data.Candle, period int, fastSC, slowSC float64) float64 {
	n := len(candles)
	if n < period+1 {
		return 0
	}

	// Initialize KAMA with SMA of first 'period' candles
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += candles[i].Close
	}
	kama := sum / float64(period)

	// Calculate KAMA for remaining candles
	for i := period; i < n; i++ {
		// Efficiency Ratio (ER)
		direction := math.Abs(candles[i].Close - candles[i-period].Close)

		volatility := 0.0
		for j := i - period + 1; j <= i; j++ {
			volatility += math.Abs(candles[j].Close - candles[j-1].Close)
		}

		var er float64
		if volatility > 0 {
			er = direction / volatility
		}

		// Smoothing Constant (SC)
		sc := math.Pow(er*(fastSC-slowSC)+slowSC, 2)

		// KAMA update
		kama = kama + sc*(candles[i].Close-kama)
	}

	return kama
}
