package engine

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ComparisonResult NFI vs BigVolver 비교 결과
type ComparisonResult struct {
	Timestamp       time.Time `json:"timestamp"`
	Period          string    `json:"period"` // "1h", "24h", "7d", "all"

	// NFI 지표
	NFI_Equity      float64 `json:"nfi_equity"`
	NFI_PnL         float64 `json:"nfi_pnl"`
	NFI_PnLPct      float64 `json:"nfi_pnl_pct"`
	NFI_Sharpe      float64 `json:"nfi_sharpe"`
	NFI_WinRate     float64 `json:"nfi_win_rate"`
	NFI_MaxDD       float64 `json:"nfi_max_dd"`
	NFI_Trades      int     `json:"nfi_trades"`
	NFI_ProfitFactor float64 `json:"nfi_profit_factor"`

	// BigVolver 지표
	BV_Equity       float64 `json:"bv_equity"`
	BV_PnL          float64 `json:"bv_pnl"`
	BV_PnLPct       float64 `json:"bv_pnl_pct"`
	BV_Sharpe       float64 `json:"bv_sharpe"`
	BV_WinRate      float64 `json:"bv_win_rate"`
	BV_MaxDD        float64 `json:"bv_max_dd"`
	BV_Trades       int     `json:"bv_trades"`
	BV_ProfitFactor float64 `json:"bv_profit_factor"`

	// 비교 지표
	DeltaPnL        float64 `json:"delta_pnl"`          // BV_PnL - NFI_PnL
	SignalAgreement float64 `json:"signal_agreement"`    // 시그널 일치도 (0~1)
	NFI_OnlyAlpha   float64 `json:"nfi_only_alpha"`      // NFI만 진입 시 수익률
	BV_OnlyAlpha    float64 `json:"bv_only_alpha"`       // BigV만 진입 시 수익률
	Both_WinAlpha   float64 `json:"both_win_alpha"`      // 둘 다 진입 시 수익률
}

// Comparator는 두 시스템의 성과를 비교
type Comparator struct {
	mu             sync.RWMutex
	nfiPocket      *PaperPocket
	bigvPocket     *PaperPocket
	signalBus      *SignalBus
	results        []ComparisonResult
	equityHistory  []EquitySnapshot // 시계열 자산 데이터 (차트용)
	maxHistory     int
}

// EquitySnapshot 시계열 자산 스냅샷
type EquitySnapshot struct {
	Timestamp time.Time `json:"timestamp"`
	NFI       float64   `json:"nfi"`
	BigV      float64   `json:"bigv"`
}

// NewComparator 새로운 비교기 생성
func NewComparator(nfiPocket, bigvPocket *PaperPocket, signalBus *SignalBus) *Comparator {
	return &Comparator{
		nfiPocket:     nfiPocket,
		bigvPocket:    bigvPocket,
		signalBus:     signalBus,
		results:       make([]ComparisonResult, 0),
		equityHistory: make([]EquitySnapshot, 0),
		maxHistory:    10000,
	}
}

// Evaluate 현재 상태 기준으로 비교 평가
func (c *Comparator) Evaluate(period string) ComparisonResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	nfiMetrics := c.nfiPocket.GetMetrics()
	bvMetrics := c.bigvPocket.GetMetrics()

	result := ComparisonResult{
		Timestamp:       time.Now(),
		Period:          period,
		NFI_Equity:      c.nfiPocket.GetEquity(),
		NFI_PnL:         nfiMetrics.TotalReturn,
		NFI_PnLPct:      nfiMetrics.TotalReturnPct,
		NFI_Sharpe:      nfiMetrics.SharpeRatio,
		NFI_WinRate:     nfiMetrics.WinRate,
		NFI_MaxDD:       nfiMetrics.MaxDrawdown,
		NFI_Trades:      nfiMetrics.TotalTrades,
		NFI_ProfitFactor: nfiMetrics.ProfitFactor,
		BV_Equity:       c.bigvPocket.GetEquity(),
		BV_PnL:          bvMetrics.TotalReturn,
		BV_PnLPct:       bvMetrics.TotalReturnPct,
		BV_Sharpe:       bvMetrics.SharpeRatio,
		BV_WinRate:      bvMetrics.WinRate,
		BV_MaxDD:        bvMetrics.MaxDrawdown,
		BV_Trades:       bvMetrics.TotalTrades,
		BV_ProfitFactor: bvMetrics.ProfitFactor,
		DeltaPnL:        bvMetrics.TotalReturn - nfiMetrics.TotalReturn,
		SignalAgreement: c.signalBus.GetSignalAgreement(24 * time.Hour),
	}

	// Alpha 분석 (시그널 출처별 수익률)
	calculateAlphaAnalysis(c, &result)

	// 결과 저장
	c.results = append(c.results, result)
	if len(c.results) > c.maxHistory {
		c.results = c.results[len(c.results)-c.maxHistory:]
	}

	// 자산 스냅샷 저장
	snapshot := EquitySnapshot{
		Timestamp: time.Now(),
		NFI:       c.nfiPocket.GetEquity(),
		BigV:      c.bigvPocket.GetEquity(),
	}
	c.equityHistory = append(c.equityHistory, snapshot)
	if len(c.equityHistory) > c.maxHistory {
		c.equityHistory = c.equityHistory[len(c.equityHistory)-c.maxHistory:]
	}

	return result
}

// calculateAlphaAnalysis 시그널 출처별 Alpha 분석
func calculateAlphaAnalysis(c *Comparator, result *ComparisonResult) {
	history := c.signalBus.GetHistory()
	if len(history) == 0 {
		return
	}

	// 페어별로 NFI/BigV 시그널을 매칭하여 수익률 분석
	type pairSignals struct {
		nfiEntry  *PaperTrade
		bvEntry   *PaperTrade
	}

	pairTrades := make(map[string]*pairSignals)

	// NFI 거래 매핑
	for _, trade := range c.nfiPocket.TradeHistory {
		if _, exists := pairTrades[trade.Pair]; !exists {
			pairTrades[trade.Pair] = &pairSignals{}
		}
		pairTrades[trade.Pair].nfiEntry = &trade
	}

	// BigV 거래 매핑
	for _, trade := range c.bigvPocket.TradeHistory {
		if _, exists := pairTrades[trade.Pair]; !exists {
			pairTrades[trade.Pair] = &pairSignals{}
		}
		pairTrades[trade.Pair].bvEntry = &trade
	}

	nfiOnlyPnL := 0.0
	nfiOnlyCount := 0
	bvOnlyPnL := 0.0
	bvOnlyCount := 0
	bothPnL := 0.0
	bothCount := 0

	for _, ps := range pairTrades {
		nfi := ps.nfiEntry
		bv := ps.bvEntry

		if nfi != nil && bv != nil {
			// 둘 다 진입 → 평균 수익률
			avgPnL := (nfi.RealizedPnL + bv.RealizedPnL) / 2
			bothPnL += avgPnL
			bothCount++
		} else if nfi != nil {
			// NFI만 진입
			nfiOnlyPnL += nfi.RealizedPnL
			nfiOnlyCount++
		} else if bv != nil {
			// BigV만 진입
			bvOnlyPnL += bv.RealizedPnL
			bvOnlyCount++
		}
	}

	if nfiOnlyCount > 0 {
		result.NFI_OnlyAlpha = (nfiOnlyPnL / float64(nfiOnlyCount)) / c.nfiPocket.InitialCapital * 100
	}
	if bvOnlyCount > 0 {
		result.BV_OnlyAlpha = (bvOnlyPnL / float64(bvOnlyCount)) / c.bigvPocket.InitialCapital * 100
	}
	if bothCount > 0 {
		result.Both_WinAlpha = (bothPnL / float64(bothCount)) / (c.nfiPocket.InitialCapital) * 100
	}
}

// GetLatestResult 최근 비교 결과
func (c *Comparator) GetLatestResult() *ComparisonResult {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.results) == 0 {
		return nil
	}
	r := c.results[len(c.results)-1]
	return &r
}

// GetResults 모든 비교 결과
func (c *Comparator) GetResults() []ComparisonResult {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return append([]ComparisonResult(nil), c.results...)
}

// GetEquityHistory 자산 변화 시계열 (차트 데이터)
func (c *Comparator) GetEquityHistory() []EquitySnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return append([]EquitySnapshot(nil), c.equityHistory...)
}

// ToJSON 비교 결과를 JSON으로 변환
func (r *ComparisonResult) ToJSON() string {
	b, _ := json.MarshalIndent(r, "", "  ")
	return string(b)
}

// Summary 사람이 읽기 쉬운 요약
func (r *ComparisonResult) Summary() string {
	winner := "NFI"
	if r.BV_PnLPct > r.NFI_PnLPct {
		winner = "BigVolver"
	}

	return fmt.Sprintf(
		"[%s] NFI: %.2f%% (Sharpe %.2f, WinRate %.1f%%) vs BigV: %.2f%% (Sharpe %.2f, WinRate %.1f%%) | 승자: %s | 시그널 일치도: %.0f%%",
		r.Period,
		r.NFI_PnLPct, r.NFI_Sharpe, r.NFI_WinRate,
		r.BV_PnLPct, r.BV_Sharpe, r.BV_WinRate,
		winner,
		r.SignalAgreement*100,
	)
}
