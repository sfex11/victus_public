package ml

import (
	"fmt"
	"math"
	"sync"
	"time"

	"dsl-strategy-evolver/internal/data"
)

// FeatureSet represents all computed features for a single timestamp
type FeatureSet struct {
	Symbol    string
	Timestamp int64
	Features  map[string]float64
	Target    float64 // future return (for training)
}

// FeatureConfig defines which features to compute
type FeatureConfig struct {
	TechnicalIndicators []string
	Microstructure     bool
	Derived            bool
	TargetHorizon      int // hours ahead for target
}

// DefaultFeatureConfig returns the default feature set based on V2 plan
func DefaultFeatureConfig() *FeatureConfig {
	return &FeatureConfig{
		TechnicalIndicators: []string{
			"ema_5", "ema_20", "ema_50", "ema_200",
			"rsi_14", "rsi_28",
			"macd_line", "macd_signal", "macd_histogram",
			"atr_14", "bollinger_upper", "bollinger_lower",
			"adx_14", "obv",
			"volume_ratio",
		},
		Microstructure: true,
		Derived:        true,
		TargetHorizon:  4, // 4h forward return
	}
}

// FeaturePipeline computes features from raw market data
type FeaturePipeline struct {
	config     *FeatureConfig
	candleRepo *data.MarketDataRepository
	mu         sync.Mutex
}

// NewFeaturePipeline creates a new feature pipeline
func NewFeaturePipeline(config *FeatureConfig, repo *data.MarketDataRepository) *FeaturePipeline {
	if config == nil {
		config = DefaultFeatureConfig()
	}
	return &FeaturePipeline{
		config:     config,
		candleRepo: repo,
	}
}

// ComputeFeaturesForSymbol generates feature sets for a symbol over its entire history
func (fp *FeaturePipeline) ComputeFeaturesForSymbol(symbol string) ([]*FeatureSet, error) {
	// Get all candles for this symbol
	candles, err := fp.candleRepo.GetHistoricalDataForBacktest(symbol, 365)
	if err != nil {
		return nil, fmt.Errorf("failed to get candles: %w", err)
	}
	if len(candles) < 200 {
		return nil, fmt.Errorf("insufficient candles: %d (need >= 200)", len(candles))
	}

	fundingRates, _ := fp.getFundingRates(symbol)

	var features []*FeatureSet

	// Slide window from index 200 onwards (enough history for 200-period indicators)
	for i := 200; i < len(candles)-fp.config.TargetHorizon; i++ {
		window := candles[:i+1]
		featureSet := fp.computeSingle(symbol, window, fundingRates, candles[i+fp.config.TargetHorizon])
		if featureSet != nil {
			features = append(features, featureSet)
		}
	}

	return features, nil
}

// ComputeLatestFeatures computes features for the most recent data point
func (fp *FeaturePipeline) ComputeLatestFeatures(symbol string) (*FeatureSet, error) {
	candles, err := fp.candleRepo.GetLatestCandles(symbol, 250)
	if err != nil {
		return nil, fmt.Errorf("failed to get candles: %w", err)
	}
	if len(candles) < 200 {
		return nil, fmt.Errorf("insufficient candles: %d (need >= 200)", len(candles))
	}

	// Reverse to oldest-first
	for i, j := 0, len(candles)-1; i < j; i, j = i+1, j-1 {
		candles[i], candles[j] = candles[j], candles[i]
	}

	fundingRates, _ := fp.getFundingRates(symbol)
	return fp.computeSingle(symbol, candles, fundingRates, nil), nil
}

// computeSingle computes a single feature set from a candle window
func (fp *FeaturePipeline) computeSingle(symbol string, candles []*data.Candle, fundingRates []*data.FundingRate, targetCandle *data.Candle) *FeatureSet {
	latest := candles[len(candles)-1]
	fs := &FeatureSet{
		Symbol:    symbol,
		Timestamp: latest.Timestamp,
		Features:  make(map[string]float64),
	}

	// Technical indicators
	for _, name := range fp.config.TechnicalIndicators {
		val := fp.computeIndicator(candles, name)
		fs.Features[name] = val
	}

	// Microstructure features
	if fp.config.Microstructure {
		fp.computeMicrostructure(candles, fundingRates, latest.Timestamp, fs)
	}

	// Derived features
	if fp.config.Derived {
		fp.computeDerived(candles, fs)
	}

	// Target: future return
	if targetCandle != nil {
		fs.Target = (targetCandle.Close - latest.Close) / latest.Close * 100
	}

	return fs
}

// computeIndicator computes a single named technical indicator
func (fp *FeaturePipeline) computeIndicator(candles []*data.Candle, name string) float64 {
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

// computeMicrostructure adds funding rate and market structure features
func (fp *FeaturePipeline) computeMicrostructure(candles []*data.Candle, fundingRates []*data.FundingRate, timestamp int64, fs *FeatureSet) {
	// Find nearest funding rate
	var latestFunding float64
	for _, fr := range fundingRates {
		if fr.Timestamp <= timestamp {
			latestFunding = fr.Rate
		}
	}
	fs.Features["funding_rate"] = latestFunding

	// Funding rate changes
	var fundingChanges []float64
	for i := len(fundingRates) - 3; i < len(fundingRates); i++ {
		if i >= 0 && i+1 < len(fundingRates) {
			change := fundingRates[i+1].Rate - fundingRates[i].Rate
			fundingChanges = append(fundingChanges, change)
		}
	}
	if len(fundingChanges) > 0 {
		fs.Features["funding_rate_change_1h"] = fundingChanges[len(fundingChanges)-1]
	}
	if len(fundingChanges) > 1 {
		fs.Features["funding_rate_change_8h"] = fundingChanges[0]
	}
}

// computeDerived adds derived features (volatility, momentum, regime)
func (fp *FeaturePipeline) computeDerived(candles []*data.Candle, fs *FeatureSet) {
	n := len(candles)

	// Multi-timeframe volatility
	fs.Features["volatility_1h"] = calcVolatility(candles, 1)
	fs.Features["volatility_4h"] = calcVolatility(candles, 4)
	fs.Features["volatility_24h"] = calcVolatility(candles, 24)

	// Multi-timeframe momentum
	fs.Features["momentum_1h"] = calcMomentum(candles, 1)
	fs.Features["momentum_4h"] = calcMomentum(candles, 4)

	// Mean reversion score
	close := candles[n-1].Close
	ema20 := calcEMA(candles, 20)
	if ema20 > 0 {
		fs.Features["mean_reversion_score"] = (close - ema20) / ema20 * 100
	}

	// Regime detection (trending/ranging/volatile)
	regime := detectRegime(candles)
	fs.Features["regime_trending"] = regime.trending
	fs.Features["regime_ranging"] = regime.ranging
	fs.Features["regime_volatile"] = regime.volatile
}

// regime represents market regime probabilities
type regime struct {
	trending float64
	ranging  float64
	volatile float64
}

// detectRegime classifies the current market regime
func detectRegime(candles []*data.Candle) regime {
	if len(candles) < 50 {
		return regime{trending: 0.33, ranging: 0.34, volatile: 0.33}
	}

	// ADX for trend strength
	adx := calcADX(candles, 14)

	// Volatility for volatility regime
	vol := calcVolatility(candles, 24)

	// Simple regime classification
	trending := math.Min(adx/50.0, 1.0) * 0.7
	volatile := math.Min(vol/5.0, 1.0) * 0.7
	ranging := 1.0 - trending - volatile
	if ranging < 0 {
		ranging = 0
	}

	// Normalize
	total := trending + ranging + volatile
	if total > 0 {
		trending /= total
		ranging /= total
		volatile /= total
	}

	return regime{trending: trending, ranging: ranging, volatile: volatile}
}

// --- Indicator calculation functions ---

func calcEMA(candles []*data.Candle, period int) float64 {
	n := len(candles)
	if n < period {
		return 0
	}

	sum := 0.0
	for i := 0; i < period; i++ {
		sum += candles[i].Close
	}
	ema := sum / float64(period)

	multiplier := 2.0 / (float64(period) + 1.0)
	for i := period; i < n; i++ {
		ema = (candles[i].Close - ema)*multiplier + ema
	}
	return ema
}

func calcSMA(candles []*data.Candle, period int) float64 {
	n := len(candles)
	if n < period {
		return 0
	}

	sum := 0.0
	start := n - period
	for i := start; i < n; i++ {
		sum += candles[i].Close
	}
	return sum / float64(period)
}

func calcRSI(candles []*data.Candle, period int) float64 {
	n := len(candles)
	if n < period+1 {
		return 50.0
	}

	gains := make([]float64, 0, period)
	losses := make([]float64, 0, period)

	for i := n - period; i < n; i++ {
		change := candles[i].Close - candles[i-1].Close
		if change > 0 {
			gains = append(gains, change)
			losses = append(losses, 0)
		} else {
			gains = append(gains, 0)
			losses = append(losses, -change)
		}
	}

	avgGain := 0.0
	avgLoss := 0.0
	for _, g := range gains {
		avgGain += g
	}
	for _, l := range losses {
		avgLoss += l
	}
	avgGain /= float64(period)
	avgLoss /= float64(period)

	if avgLoss == 0 {
		return 100.0
	}

	rs := avgGain / avgLoss
	return 100 - (100 / (1 + rs))
}

func calcMACDLine(candles []*data.Candle) float64 {
	if len(candles) < 26 {
		return 0
	}
	ema12 := calcEMA(candles, 12)
	ema26 := calcEMA(candles, 26)
	return ema12 - ema26
}

func calcMACDSignal(candles []*data.Candle) float64 {
	// MACD Signal Line = 9-period EMA of MACD Line series
	// Requires at least 26 (for EMA26) + 9 (for signal EMA) = 35 candles
	if len(candles) < 35 {
		return 0
	}

	// Build MACD Line time series
	macdSeries := make([]float64, len(candles)-25)
	for i := 25; i < len(candles); i++ {
		window := candles[:i+1]
		ema12 := calcEMA(window, 12)
		ema26 := calcEMA(window, 26)
		macdSeries[i-25] = ema12 - ema26
	}

	// Apply 9-period EMA on MACD series
	if len(macdSeries) < 9 {
		return 0
	}

	// SMA seed
	sum := 0.0
	for i := 0; i < 9; i++ {
		sum += macdSeries[i]
	}
	signal := sum / 9.0

	multiplier := 2.0 / 10.0
	for i := 9; i < len(macdSeries); i++ {
		signal = (macdSeries[i]-signal)*multiplier + signal
	}
	return signal
}

func calcATR(candles []*data.Candle, period int) float64 {
	n := len(candles)
	if n < period+1 {
		return 0
	}

	sum := 0.0
	for i := n - period; i < n; i++ {
		high := candles[i].High
		low := candles[i].Low
		prevClose := candles[i-1].Close

		tr := high - low
		if hPrevClose := high - prevClose; hPrevClose > tr {
			tr = hPrevClose
		}
		if lPrevClose := low - prevClose; lPrevClose > tr {
			tr = lPrevClose
		}
		sum += tr
	}
	return sum / float64(period)
}

func calcBollinger(candles []*data.Candle, period int) (middle, std float64) {
	n := len(candles)
	if n < period {
		return 0, 0
	}

	middle = calcSMA(candles, period)

	sumSquares := 0.0
	start := n - period
	for i := start; i < n; i++ {
		diff := candles[i].Close - middle
		sumSquares += diff * diff
	}
	std = math.Sqrt(sumSquares / float64(period))
	return
}

func calcADX(candles []*data.Candle, period int) float64 {
	n := len(candles)
	if n < period*2+1 {
		return 0
	}

	// Calculate +DM, -DM, and TR series
	dmPlusSeries := make([]float64, 0, n)
	dmMinusSeries := make([]float64, 0, n)
	trSeries := make([]float64, 0, n)

	for i := 1; i < n; i++ {
		high := candles[i].High
		low := candles[i].Low
		prevHigh := candles[i-1].High
		prevLow := candles[i-1].Low
		prevClose := candles[i-1].Close

		// True Range
		tr := high - low
		if hPrev := high - prevClose; hPrev > tr {
			tr = hPrev
		}
		if lPrev := low - prevClose; lPrev > tr {
			tr = lPrev
		}
		trSeries = append(trSeries, tr)

		// +DM / -DM
		upMove := high - prevHigh
		downMove := prevLow - low

		var dmPlus, dmMinus float64
		if upMove > downMove && upMove > 0 {
			dmPlus = upMove
		}
		if downMove > upMove && downMove > 0 {
			dmMinus = downMove
		}
		dmPlusSeries = append(dmPlusSeries, dmPlus)
		dmMinusSeries = append(dmMinusSeries, dmMinus)
	}

	// Wilder smoothing: first value is SMA, then EMA with alpha = 1/period
	smoothTR := wilderSmooth(trSeries, period)
	smoothDMPlus := wilderSmooth(dmPlusSeries, period)
	smoothDMMinus := wilderSmooth(dmMinusSeries, period)

	// Build DX series
	dxSeries := make([]float64, 0, len(smoothTR))
	for i := 0; i < len(smoothTR); i++ {
		if smoothTR[i] == 0 {
			dxSeries = append(dxSeries, 0)
			continue
		}
		plusDI := (smoothDMPlus[i] / smoothTR[i]) * 100
		minusDI := (smoothDMMinus[i] / smoothTR[i]) * 100
		diSum := plusDI + minusDI
		if diSum == 0 {
			dxSeries = append(dxSeries, 0)
			continue
		}
		dx := math.Abs(plusDI-minusDI) / diSum * 100
		dxSeries = append(dxSeries, dx)
	}

	// ADX = Wilder smoothing of DX
	adxValues := wilderSmooth(dxSeries, period)
	if len(adxValues) == 0 {
		return 0
	}
	return adxValues[len(adxValues)-1]
}

// wilderSmooth applies Wilder's smoothing (first value = SMA, then EMA with alpha=1/period)
func wilderSmooth(series []float64, period int) []float64 {
	if len(series) < period {
		return nil
	}

	result := make([]float64, len(series)-period+1)

	// First value: simple average
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += series[i]
	}
	result[0] = sum / float64(period)

	// Subsequent values: smoothed = prev - prev/period + current
	alpha := 1.0 / float64(period)
	for i := 1; i < len(result); i++ {
		result[i] = result[i-1] - result[i-1]*alpha + series[i+period-1]
	}

	return result
}

func calcOBV(candles []*data.Candle) float64 {
	if len(candles) < 2 {
		return 0
	}

	obvSeries := make([]float64, len(candles))
	obvSeries[0] = 0
	for i := 1; i < len(candles); i++ {
		if candles[i].Close > candles[i-1].Close {
			obvSeries[i] = obvSeries[i-1] + candles[i].Volume
		} else if candles[i].Close < candles[i-1].Close {
			obvSeries[i] = obvSeries[i-1] - candles[i].Volume
		} else {
			obvSeries[i] = obvSeries[i-1]
		}
	}

	latestOBV := obvSeries[len(obvSeries)-1]

	// Normalize: OBV / SMA(OBV, 20)
	if len(obvSeries) >= 20 {
		obvSMA := 0.0
		for i := len(obvSeries) - 20; i < len(obvSeries); i++ {
			obvSMA += obvSeries[i]
		}
		obvSMA /= 20.0
		if obvSMA != 0 {
			return latestOBV / obvSMA
		}
	}

	return latestOBV
}

func calcVolumeRatio(candles []*data.Candle, period int) float64 {
	n := len(candles)
	if n < period {
		return 1.0
	}

	currentVol := candles[n-1].Volume
	avgVol := 0.0
	for i := n - period; i < n; i++ {
		avgVol += candles[i].Volume
	}
	avgVol /= float64(period)

	if avgVol == 0 {
		return 1.0
	}
	return currentVol / avgVol
}

func calcVolatility(candles []*data.Candle, hours int) float64 {
	n := len(candles)
	if n < hours+1 {
		return 0
	}

	returns := make([]float64, 0, hours)
	for i := n - hours; i < n; i++ {
		if i > 0 {
			ret := (candles[i].Close - candles[i-1].Close) / candles[i-1].Close
			returns = append(returns, ret)
		}
	}

	if len(returns) == 0 {
		return 0
	}

	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))

	sumSquares := 0.0
	for _, r := range returns {
		diff := r - mean
		sumSquares += diff * diff
	}
	return math.Sqrt(sumSquares / float64(len(returns))) * 100
}

func calcMomentum(candles []*data.Candle, hours int) float64 {
	n := len(candles)
	if n < hours+1 {
		return 0
	}

	current := candles[n-1].Close
	past := candles[n-1-hours].Close
	if past == 0 {
		return 0
	}
	return (current - past) / past * 100
}

// getFundingRates fetches funding rates from the database
func (fp *FeaturePipeline) getFundingRates(symbol string) ([]*data.FundingRate, error) {
	// Use the candle repo's underlying DB
	// Funding rates are stored in market_funding_rate table
	// For now return empty — will be populated when Binance data pipeline provides it
	return []*data.FundingRate{}, nil
}

// FeatureNames returns the list of all feature column names
func (fp *FeaturePipeline) FeatureNames() []string {
	names := make([]string, 0)
	for _, name := range fp.config.TechnicalIndicators {
		names = append(names, name)
	}

	if fp.config.Microstructure {
		names = append(names,
			"funding_rate", "funding_rate_change_1h", "funding_rate_change_8h",
		)
	}

	if fp.config.Derived {
		names = append(names,
			"volatility_1h", "volatility_4h", "volatility_24h",
			"momentum_1h", "momentum_4h",
			"mean_reversion_score",
			"regime_trending", "regime_ranging", "regime_volatile",
		)
	}

	return names
}

// CurrentTimestamp returns the current Unix timestamp
func CurrentTimestamp() int64 {
	return time.Now().Unix()
}
