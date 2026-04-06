package engine

import (
	"math"
)

// MarketRegime represents the current market regime
type MarketRegime string

const (
	RegimeBull      MarketRegime = "BULL"
	RegimeBear      MarketRegime = "BEAR"
	RegimeHighVol   MarketRegime = "HIGH_VOL"
	RegimeLowVol    MarketRegime = "LOW_VOL"
	RegimeRecovery  MarketRegime = "RECOVERY"
	RegimeUnknown   MarketRegime = "UNKNOWN"
)

// RegimeDetector detects market regime from price data
type RegimeDetector struct {
	atrPeriod    int     // ATR calculation period
	emaFast      int     // Fast EMA period
	emaSlow      int     // Slow EMA period
	rsiPeriod    int     // RSI period
	volMultiplier float64 // Volatility threshold multiplier
}

// NewRegimeDetector creates a new regime detector
func NewRegimeDetector() *RegimeDetector {
	return &RegimeDetector{
		atrPeriod:    14,
		emaFast:      20,
		emaSlow:      50,
		rsiPeriod:    14,
		volMultiplier: 1.5,
	}
}

// RegimeInfo contains detailed regime information
type RegimeInfo struct {
	Regime        MarketRegime `json:"regime"`
	EMAFast       float64      `json:"ema_fast"`
	EMASlow       float64      `json:"ema_slow"`
	ATR           float64      `json:"atr"`
	ATRPercent    float64      `json:"atr_percent"`
	ATRMean       float64      `json:"atr_mean"`
	RSI           float64      `json:"rsi"`
	TrendStrength float64      `json:"trend_strength"` // -1 to 1
	VolLevel      float64      `json:"vol_level"`     // 0 to 2
}

// Detect analyzes price data and returns the current regime
func (d *RegimeDetector) Detect(highs, lows, closes []float64) *RegimeInfo {
	if len(closes) < d.emaSlow+1 {
		return &RegimeInfo{Regime: RegimeUnknown}
	}

	// Calculate indicators
	emaFast := d.calculateEMA(closes, d.emaFast)
	emaSlow := d.calculateEMA(closes, d.emaSlow)
	atr := d.calculateATR(highs, lows, closes, d.atrPeriod)
	atrPercent := atr / closes[len(closes)-1] * 100
	atrMean := d.calculateATRMean(highs, lows, closes, d.atrPeriod, 20)
	rsi := d.calculateRSI(closes, d.rsiPeriod)

	// Determine trend strength
	trendStrength := 0.0
	if emaSlow > 0 {
		trendStrength = (emaFast - emaSlow) / emaSlow
	}

	// Determine volatility level
	volLevel := 0.0
	if atrMean > 0 {
		volLevel = atr / atrMean
	}

	// Detect regime
	regime := d.classifyRegime(emaFast, emaSlow, atr, atrMean, rsi, trendStrength, volLevel)

	return &RegimeInfo{
		Regime:        regime,
		EMAFast:       emaFast,
		EMASlow:       emaSlow,
		ATR:           atr,
		ATRPercent:    atrPercent,
		ATRMean:       atrMean,
		RSI:           rsi,
		TrendStrength: trendStrength,
		VolLevel:      volLevel,
	}
}

// classifyRegime determines the market regime based on indicators
func (d *RegimeDetector) classifyRegime(emaFast, emaSlow, atr, atrMean, rsi, trendStrength, volLevel float64) MarketRegime {
	// High volatility check (priority 1)
	if volLevel > d.volMultiplier {
		return RegimeHighVol
	}

	// Low volatility check
	if volLevel < 0.7 {
		return RegimeLowVol
	}

	// Trend direction
	isBullTrend := emaFast > emaSlow

	// Recovery check (RSI oversold in bull trend)
	if isBullTrend && rsi < 40 {
		return RegimeRecovery
	}

	// Bear market
	if !isBullTrend && trendStrength < -0.01 {
		return RegimeBear
	}

	// Bull market
	if isBullTrend && trendStrength > 0.01 {
		return RegimeBull
	}

	// Default to bull if EMA fast > slow
	if isBullTrend {
		return RegimeBull
	}

	return RegimeBear
}

// calculateEMA calculates Exponential Moving Average
func (d *RegimeDetector) calculateEMA(data []float64, period int) float64 {
	if len(data) < period {
		return 0
	}

	k := 2.0 / float64(period+1)
	ema := data[len(data)-period]

	for i := len(data) - period + 1; i < len(data); i++ {
		ema = data[i]*k + ema*(1-k)
	}

	return ema
}

// calculateATR calculates Average True Range
func (d *RegimeDetector) calculateATR(highs, lows, closes []float64, period int) float64 {
	if len(closes) < period+1 {
		return 0
	}

	trueRanges := make([]float64, 0, len(closes)-1)
	for i := 1; i < len(closes); i++ {
		hl := highs[i] - lows[i]
		hc := math.Abs(highs[i] - closes[i-1])
		lc := math.Abs(lows[i] - closes[i-1])

		tr := math.Max(hl, math.Max(hc, lc))
		trueRanges = append(trueRanges, tr)
	}

	if len(trueRanges) < period {
		return 0
	}

	// Simple moving average of TR
	sum := 0.0
	for i := len(trueRanges) - period; i < len(trueRanges); i++ {
		sum += trueRanges[i]
	}

	return sum / float64(period)
}

// calculateATRMean calculates the mean ATR over a longer period
func (d *RegimeDetector) calculateATRMean(highs, lows, closes []float64, atrPeriod, meanPeriod int) float64 {
	if len(closes) < atrPeriod+meanPeriod {
		return 0
	}

	atrValues := make([]float64, 0, meanPeriod)
	for i := len(closes) - meanPeriod - atrPeriod; i < len(closes)-atrPeriod; i++ {
		atr := d.calculateATR(highs[:i+atrPeriod+1], lows[:i+atrPeriod+1], closes[:i+atrPeriod+1], atrPeriod)
		atrValues = append(atrValues, atr)
	}

	if len(atrValues) == 0 {
		return 0
	}

	sum := 0.0
	for _, atr := range atrValues {
		sum += atr
	}

	return sum / float64(len(atrValues))
}

// calculateRSI calculates Relative Strength Index
func (d *RegimeDetector) calculateRSI(closes []float64, period int) float64 {
	if len(closes) < period+1 {
		return 50
	}

	gains := 0.0
	losses := 0.0

	for i := len(closes) - period; i < len(closes); i++ {
		change := closes[i] - closes[i-1]
		if change > 0 {
			gains += change
		} else {
			losses -= change
		}
	}

	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)

	if avgLoss == 0 {
		return 100
	}

	rs := avgGain / avgLoss
	rsi := 100 - (100 / (1 + rs))

	return rsi
}
