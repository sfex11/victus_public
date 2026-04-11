package ml

import (
	"fmt"

	"dsl-strategy-evolver/internal/data"
)

// DataWindow provides sliding-window training data for model retraining
type DataWindow struct {
	featurePipeline *FeaturePipeline
	candleRepo      *data.MarketDataRepository
	windowDays      int
	minSamples      int
}

// NewDataWindow creates a new data window
func NewDataWindow(
	pipeline *FeaturePipeline,
	repo *data.MarketDataRepository,
	opts ...DataWindowOption,
) *DataWindow {
	dw := &DataWindow{
		featurePipeline: pipeline,
		candleRepo:      repo,
		windowDays:      30,
		minSamples:      500,
	}

	for _, opt := range opts {
		opt(dw)
	}

	return dw
}

// DataWindowOption configures the data window
type DataWindowOption func(*DataWindow)

// WithWindowDays sets the window size
func WithWindowDays(days int) DataWindowOption {
	return func(dw *DataWindow) { dw.windowDays = days }
}

// WithMinSamples sets the minimum sample threshold
func WithMinSamples(n int) DataWindowOption {
	return func(dw *DataWindow) { dw.minSamples = n }
}

// MinSamples returns the configured minimum samples
func (dw *DataWindow) MinSamples() int {
	return dw.minSamples
}

// GetTrainingWindow returns feature sets for the latest window
func (dw *DataWindow) GetTrainingWindow(symbol string) ([]*FeatureSet, error) {
	// Try default window first
	features, err := dw.computeFeatures(symbol, dw.windowDays)
	if err == nil && len(features) >= dw.minSamples {
		return features, nil
	}

	// Not enough samples — expand window up to 60 days
	for extraDays := 10; extraDays <= 30; extraDays += 10 {
		expandedDays := dw.windowDays + extraDays
		if expandedDays > 60 {
			break
		}
		features, err = dw.computeFeatures(symbol, expandedDays)
		if err == nil && len(features) >= dw.minSamples {
			return features, nil
		}
	}

	if len(features) == 0 {
		return nil, fmt.Errorf("no features generated for %s", symbol)
	}

	return features, fmt.Errorf("insufficient samples for %s: %d (need >= %d)", symbol, len(features), dw.minSamples)
}

// GetTrainingRecords returns training data as JSONL-ready records
func (dw *DataWindow) GetTrainingRecords(symbol string) ([]map[string]interface{}, error) {
	features, err := dw.GetTrainingWindow(symbol)
	if err != nil {
		return nil, err
	}

	records := make([]map[string]interface{}, 0, len(features))
	for _, fs := range features {
		records = append(records, map[string]interface{}{
			"symbol":   fs.Symbol,
			"timestamp": fs.Timestamp,
			"features": fs.Features,
			"target":   fs.Target,
		})
	}

	return records, nil
}

// computeFeatures generates features for a specific time window
func (dw *DataWindow) computeFeatures(symbol string, days int) ([]*FeatureSet, error) {
	candles, err := dw.candleRepo.GetHistoricalDataForBacktest(symbol, days)
	if err != nil {
		return nil, fmt.Errorf("get candles: %w", err)
	}

	if len(candles) < 200 {
		return nil, fmt.Errorf("insufficient candles: %d (need >= 200)", len(candles))
	}

	// Build feature sets using the pipeline
	// Reuse ComputeFeaturesForSymbol logic with specific candle set
	var features []*FeatureSet
	config := dw.featurePipeline.config

	fundingRates, _ := dw.getFundingRates(symbol)

	for i := 200; i < len(candles)-config.TargetHorizon; i++ {
		latest := candles[i]
		targetCandle := candles[i+config.TargetHorizon]

		fs := &FeatureSet{
			Symbol:    symbol,
			Timestamp: latest.Timestamp,
			Features:  make(map[string]float64),
		}

		// Technical indicators
		for _, name := range config.TechnicalIndicators {
			fs.Features[name] = dw.computeIndicator(candles, name)
		}

		// Microstructure
		if config.Microstructure {
			dw.computeMicrostructure(candles, fundingRates, latest.Timestamp, fs)
		}

		// Derived
		if config.Derived {
			dw.computeDerived(candles, fs)
		}

		// Target
		fs.Target = (targetCandle.Close - latest.Close) / latest.Close * 100

		features = append(features, fs)
	}

	return features, nil
}

// computeIndicator delegates to the package-level indicator functions
func (dw *DataWindow) computeIndicator(candles []*data.Candle, name string) float64 {
	switch name {
	case "ema_5":
		return calcEMA(candles, 5)
	case "ema_20":
		return calcEMA(candles, 20)
	case "ema_50":
		return calcEMA(candles, 50)
	case "ema_200":
		return calcEMA(candles, 200)
	case "rsi_14":
		return calcRSI(candles, 14)
	case "rsi_28":
		return calcRSI(candles, 28)
	case "macd_line":
		return calcMACDLine(candles)
	case "macd_signal":
		return calcMACDSignal(candles)
	case "macd_histogram":
		return calcMACDLine(candles) - calcMACDSignal(candles)
	case "atr_14":
		return calcATR(candles, 14)
	case "bollinger_upper":
		mid, std := calcBollinger(candles, 20)
		return mid + 2*std
	case "bollinger_lower":
		mid, std := calcBollinger(candles, 20)
		return mid - 2*std
	case "adx_14":
		return calcADX(candles, 14)
	case "obv":
		return calcOBV(candles)
	case "volume_ratio":
		return calcVolumeRatio(candles, 20)
	default:
		return 0
	}
}

// computeMicrostructure adds funding rate features
func (dw *DataWindow) computeMicrostructure(candles []*data.Candle, fundingRates []*data.FundingRate, timestamp int64, fs *FeatureSet) {
	var latestFunding float64
	for _, fr := range fundingRates {
		if fr.Timestamp <= timestamp {
			latestFunding = fr.Rate
		}
	}
	fs.Features["funding_rate"] = latestFunding

	var fundingChanges []float64
	for i := len(fundingRates) - 3; i < len(fundingRates); i++ {
		if i >= 0 && i+1 < len(fundingRates) {
			fundingChanges = append(fundingChanges, fundingRates[i+1].Rate-fundingRates[i].Rate)
		}
	}
	if len(fundingChanges) > 0 {
		fs.Features["funding_rate_change_1h"] = fundingChanges[len(fundingChanges)-1]
	}
	if len(fundingChanges) > 1 {
		fs.Features["funding_rate_change_8h"] = fundingChanges[0]
	}
}

// computeDerived adds derived features
func (dw *DataWindow) computeDerived(candles []*data.Candle, fs *FeatureSet) {
	fs.Features["volatility_1h"] = calcVolatility(candles, 1)
	fs.Features["volatility_4h"] = calcVolatility(candles, 4)
	fs.Features["volatility_24h"] = calcVolatility(candles, 24)
	fs.Features["momentum_1h"] = calcMomentum(candles, 1)
	fs.Features["momentum_4h"] = calcMomentum(candles, 4)

	n := len(candles)
	close := candles[n-1].Close
	ema20 := calcEMA(candles, 20)
	if ema20 > 0 {
		fs.Features["mean_reversion_score"] = (close - ema20) / ema20 * 100
	}

	regime := detectRegime(candles)
	fs.Features["regime_trending"] = regime.trending
	fs.Features["regime_ranging"] = regime.ranging
	fs.Features["regime_volatile"] = regime.volatile
}

func (dw *DataWindow) getFundingRates(symbol string) ([]*data.FundingRate, error) {
	return []*data.FundingRate{}, nil
}
