package engine

import (
	"math"
)

// RiskScoreState represents the current market risk state
type RiskScoreState string

const (
	RiskNormal    RiskScoreState = "NORMAL"    // 0-30: 그리드 유지, 정상 진화
	RiskCaution   RiskScoreState = "CAUTION"   // 31-60: 그리드 간격 확대, 진화 축소
	RiskWarning   RiskScoreState = "WARNING"   // 61-80: 신규 주문 중단, 진화 중단
	RiskEmergency RiskScoreState = "EMERGENCY" // 81-100: 전체 청산, 비상 모드
)

// RiskScore holds the computed market risk assessment
type RiskScore struct {
	Score           float64       `json:"score"`
	State           RiskScoreState `json:"state"`
	ATRScore        float64       `json:"atr_score"`
	RSIScore        float64       `json:"rsi_score"`
	BBScore         float64       `json:"bb_score"`
	VolumeScore     float64       `json:"volume_score"`
	ATRValue        float64       `json:"atr_value"`
	ATRPercent      float64       `json:"atr_percent"`
	RSIValue        float64       `json:"rsi_value"`
	BBWidth         float64       `json:"bb_width"`
	VolumeRatio     float64       `json:"volume_ratio"`
	Regime          MarketRegime  `json:"regime"`
	ShouldEvolve    bool          `json:"should_evolve"`
	EvolutionWeight float64       `json:"evolution_weight"`
}

// RiskScorer calculates market risk scores from raw data
// Inspired by SNOWBALL's multi-indicator scoring system
type RiskScorer struct {
	atrWeights     []float64 // ATR comparison window multipliers
	bbPeriod       int       // Bollinger Band period
	bbStdDev       float64   // BB standard deviation multiplier
	volumeLookback int       // volume average lookback
}

// NewRiskScorer creates a risk scorer with SNOWBALL defaults
func NewRiskScorer() *RiskScorer {
	return &RiskScorer{
		atrWeights:     []float64{0.5, 0.7, 1.0, 1.3, 1.6, 2.0}, // ATR spike thresholds
		bbPeriod:       20,
		bbStdDev:       2.0,
		volumeLookback: 20,
	}
}

// Calculate computes the composite risk score from market data
// Requires at least 50 candles for accurate calculation
func (s *RiskScorer) Calculate(highs, lows, closes, volumes []float64) *RiskScore {
	n := len(closes)
	rs := &RiskScore{
		Score:        0,
		State:        RiskNormal,
		Regime:       RegimeUnknown,
		ShouldEvolve: true,
	}

	if n < 50 {
		rs.ShouldEvolve = false
		return rs
	}

	// 1. ATR Score (0-30 points) — volatility surge detection
	rs.ATRValue = calculateSimpleATR(highs, lows, closes, 14)
	if closes[n-1] > 0 {
		rs.ATRPercent = rs.ATRValue / closes[n-1] * 100
	}

	// Compare current ATR to historical ATR distribution
	historicalATRs := make([]float64, 0, min(n-15, 30))
	for i := 20; i < n-14; i++ {
		atr := calculateSimpleATR(highs[:i+1], lows[:i+1], closes[:i+1], 14)
		if atr > 0 {
			historicalATRs = append(historicalATRs, atr)
		}
	}

	if len(historicalATRs) > 5 {
		atrMean := mean(historicalATRs)
		atrStd := stddev(historicalATRs)

		if atrMean > 0 {
			// Score based on how many standard deviations above mean
			zScore := 0.0
			if atrStd > 0 {
				zScore = (rs.ATRValue - atrMean) / atrStd
			}

			// Map z-score to 0-30 range
			if zScore > 3 {
				rs.ATRScore = 30
			} else if zScore > 2 {
				rs.ATRScore = 25
			} else if zScore > 1 {
				rs.ATRScore = 15
			} else if zScore > 0 {
				rs.ATRScore = 5
			}
		}
	}

	// 2. RSI Score (0-25 points) — extremity detection
	rsiVal := calculateSimpleRSI(closes, 14)
	rs.RSIValue = rsiVal

	// Score RSI distance from neutral (50)
	rsiDist := math.Abs(rsiVal - 50)
	if rsiDist > 40 { // RSI < 10 or > 90
		rs.RSIScore = 25
	} else if rsiDist > 30 { // RSI < 20 or > 80
		rs.RSIScore = 20
	} else if rsiDist > 20 { // RSI < 30 or > 70
		rs.RSIScore = 12
	} else if rsiDist > 10 {
		rs.RSIScore = 5
	}

	// 3. Bollinger Band Score (0-25 points) — bandwidth expansion
	bbWidth, bbWidthRatio := s.calculateBBWidth(closes, s.bbPeriod, s.bbStdDev)
	rs.BBWidth = bbWidth

	if bbWidthRatio > 2.0 {
		rs.BBScore = 25
	} else if bbWidthRatio > 1.5 {
		rs.BBScore = 18
	} else if bbWidthRatio > 1.2 {
		rs.BBScore = 10
	} else if bbWidthRatio > 1.0 {
		rs.BBScore = 4
	}

	// 4. Volume Score (0-20 points) — volume spike detection
	if len(volumes) >= s.volumeLookback {
		recentVol := mean(volumes[n-s.volumeLookback:])
		histVol := mean(volumes[max(0, n-s.volumeLookback*3) : n-s.volumeLookback])
		if histVol > 0 {
			rs.VolumeRatio = recentVol / histVol
			if rs.VolumeRatio > 3.0 {
				rs.VolumeScore = 20
			} else if rs.VolumeRatio > 2.0 {
				rs.VolumeScore = 15
			} else if rs.VolumeRatio > 1.5 {
				rs.VolumeScore = 8
			} else if rs.VolumeRatio > 1.2 {
				rs.VolumeScore = 3
			}
		}
	}

	// Composite score
	rs.Score = rs.ATRScore + rs.RSIScore + rs.BBScore + rs.VolumeScore

	// Determine state
	rs.State = classifyRiskState(rs.Score)

	// Determine evolution policy
	switch rs.State {
	case RiskNormal:
		rs.ShouldEvolve = true
		rs.EvolutionWeight = 1.0
	case RiskCaution:
		rs.ShouldEvolve = true
		rs.EvolutionWeight = 0.5 // halve generation count
	case RiskWarning:
		rs.ShouldEvolve = false // skip evolution
		rs.EvolutionWeight = 0.0
	case RiskEmergency:
		rs.ShouldEvolve = false // emergency, no evolution
		rs.EvolutionWeight = 0.0
	}

	return rs
}

// classifyRiskState maps score to state (SNOWBALL thresholds)
func classifyRiskState(score float64) RiskScoreState {
	switch {
	case score <= 30:
		return RiskNormal
	case score <= 60:
		return RiskCaution
	case score <= 80:
		return RiskWarning
	default:
		return RiskEmergency
	}
}

// calculateBBWidth computes Bollinger Band width and ratio to historical average
func (s *RiskScorer) calculateBBWidth(closes []float64, period int, stdMult float64) (width, ratio float64) {
	n := len(closes)
	if n < period+10 {
		return 0, 1.0
	}

	// Current BB width
	sma := mean(closes[n-period:])
	variance := 0.0
	for i := n - period; i < n; i++ {
		diff := closes[i] - sma
		variance += diff * diff
	}
	std := math.Sqrt(variance / float64(period))
	width = (sma + stdMult*std) - (sma - stdMult*std)

	// Historical BB widths
	widths := make([]float64, 0, min(n-period-10, 20))
	for i := period + 10; i < n; i++ {
		hSma := mean(closes[i-period : i])
		hVar := 0.0
		for j := i - period; j < i; j++ {
			diff := closes[j] - hSma
			hVar += diff * diff
		}
		hStd := math.Sqrt(hVar / float64(period))
		w := (hSma + stdMult*hStd) - (hSma - stdMult*hStd)
		if w > 0 {
			widths = append(widths, w)
		}
	}

	if len(widths) > 3 && width > 0 {
		avgWidth := mean(widths)
		if avgWidth > 0 {
			ratio = width / avgWidth
		}
	}

	return width, ratio
}

// calculateSimpleATR computes ATR without needing a full detector
func calculateSimpleATR(highs, lows, closes []float64, period int) float64 {
	n := len(closes)
	if n < period+1 {
		return 0
	}

	trueRanges := make([]float64, 0, n-1)
	for i := 1; i < n; i++ {
		hl := highs[i] - lows[i]
		hc := math.Abs(highs[i] - closes[i-1])
		lc := math.Abs(lows[i] - closes[i-1])
		tr := math.Max(hl, math.Max(hc, lc))
		trueRanges = append(trueRanges, tr)
	}

	if len(trueRanges) < period {
		return 0
	}

	sum := 0.0
	for i := len(trueRanges) - period; i < len(trueRanges); i++ {
		sum += trueRanges[i]
	}
	return sum / float64(period)
}

// calculateSimpleRSI computes RSI
func calculateSimpleRSI(closes []float64, period int) float64 {
	n := len(closes)
	if n < period+1 {
		return 50
	}

	gains := 0.0
	losses := 0.0
	for i := n - period; i < n; i++ {
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
	return 100 - (100 / (1 + rs))
}

func mean(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range data {
		sum += v
	}
	return sum / float64(len(data))
}

func stddev(data []float64) float64 {
	if len(data) < 2 {
		return 0
	}
	m := mean(data)
	sum := 0.0
	for _, v := range data {
		d := v - m
		sum += d * d
	}
	return math.Sqrt(sum / float64(len(data)))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
