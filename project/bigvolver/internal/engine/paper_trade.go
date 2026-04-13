package engine

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// PaperTradeConfig 가상매매 공통 설정
type PaperTradeConfig struct {
	InitialCapital float64 // 예: 10000 USDT
	FeeRate        float64 // 0.001 (0.1%)
	Slippage       float64 // 0.01 (1%)
	MaxPositions   int     // 최대 동시 포지션 수
	StakeCurrency  string  // "USDT"
	Timeframe      string  // "5m"
}

// PaperPosition 가상매매 포지션
type PaperPosition struct {
	ID            string
	Pair          string
	Direction     string  // "LONG", "SHORT"
	EntryPrice    float64 // 진입 가격 (슬리피지 포함)
	CurrentPrice  float64
	SizeUSDT      float64
	SizeCoin      float64 // 실제 코인 수량
	EntryTime     time.Time
	ExitPrice     float64
	ExitTime      time.Time
	Status        string  // "OPEN", "CLOSED"
	ExitReason    string
	UnrealizedPnL float64
	RealizedPnL   float64
	FeePaid       float64 // 총 수수료
}

// PaperPocket 각 시스템의 독립적인 자본/포지션 관리
type PaperPocket struct {
	mu             sync.RWMutex
	Name           string // "NFI" or "BIGVOLVER"
	InitialCapital float64
	AvailableCash  float64
	Positions      map[string]*PaperPosition // pair -> position
	TradeHistory   []PaperTrade
	TotalFeesPaid  float64
	config         *PaperTradeConfig
}

// PaperTrade 거래 이력
type PaperTrade struct {
	ID          string
	Pair        string
	Direction   string
	EntryPrice  float64
	ExitPrice   float64
	SizeUSDT    float64
	RealizedPnL float64
	FeePaid     float64
	EntryTime   time.Time
	ExitTime    time.Time
	Duration    time.Duration
	ExitReason  string
	Source      string // "NFI" or "BIGVOLVER"
}

// PocketMetrics 포켓별 성과 지표
type PocketMetrics struct {
	TotalReturn   float64
	TotalReturnPct float64
	WinRate       float64
	TotalTrades   int
	WinningTrades int
	LosingTrades  int
	SharpeRatio   float64
	MaxDrawdown   float64
	ProfitFactor  float64
	AvgWinPnL     float64
	AvgLossPnL    float64
}

// NewPaperPocket 새로운 포켓 생성
func NewPaperPocket(name string, config *PaperTradeConfig) *PaperPocket {
	return &PaperPocket{
		Name:           name,
		InitialCapital: config.InitialCapital,
		AvailableCash:  config.InitialCapital,
		Positions:      make(map[string]*PaperPosition),
		TradeHistory:   make([]PaperTrade, 0),
		config:         config,
	}
}

// OpenPosition 시그널에 따라 포지션 오픈
func (p *PaperPocket) OpenPosition(pair, direction string, price float64) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 이미 해당 페어에 포지션이 있는지 확인
	if _, exists := p.Positions[pair]; exists {
		return fmt.Errorf("이미 %s 포지션이 존재함", pair)
	}

	// 최대 포지션 수 확인
	openCount := 0
	for _, pos := range p.Positions {
		if pos.Status == "OPEN" {
			openCount++
		}
	}
	if openCount >= p.config.MaxPositions {
		return fmt.Errorf("최대 포지션 수 도달 (%d/%d)", openCount, p.config.MaxPositions)
	}

	// 자본 할당 (균등 분배)
	stakePerTrade := p.AvailableCash / float64(p.config.MaxPositions)
	if stakePerTrade < 10.0 { // 최소 진입금
		return fmt.Errorf("잔액 부족 (available: %.2f)", p.AvailableCash)
	}

	// 슬리피지 적용
	var adjustedPrice float64
	if direction == "LONG" {
		adjustedPrice = price * (1 + p.config.Slippage) // 매수 시 가격 상승
	} else {
		adjustedPrice = price * (1 - p.config.Slippage) // 매도 시 가격 하락
	}

	// 수수료 적용
	fee := stakePerTrade * p.config.FeeRate
	actualStake := stakePerTrade - fee
	sizeCoin := actualStake / adjustedPrice

	// 잔액 차감
	p.AvailableCash -= stakePerTrade
	p.TotalFeesPaid += fee

	pos := &PaperPosition{
		ID:           fmt.Sprintf("%s_%s_%d", p.Name, pair, time.Now().UnixNano()),
		Pair:         pair,
		Direction:    direction,
		EntryPrice:   adjustedPrice,
		CurrentPrice: adjustedPrice,
		SizeUSDT:     actualStake,
		SizeCoin:     sizeCoin,
		EntryTime:    time.Now(),
		Status:       "OPEN",
		FeePaid:      fee,
	}

	p.Positions[pair] = pos
	return nil
}

// ClosePosition 포지션 청산
func (p *PaperPocket) ClosePosition(pair string, currentPrice float64, reason string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	pos, exists := p.Positions[pair]
	if !exists || pos.Status != "OPEN" {
		return fmt.Errorf("오픈된 %s 포지션이 없음", pair)
	}

	// 슬리피지 적용
	var adjustedPrice float64
	if pos.Direction == "LONG" {
		adjustedPrice = currentPrice * (1 - p.config.Slippage) // 매도 시 가격 하락
	} else {
		adjustedPrice = currentPrice * (1 + p.config.Slippage) // 매수 시 가격 상승
	}

	// 수수료 적용
	exitValue := pos.SizeCoin * adjustedPrice
	fee := exitValue * p.config.FeeRate

	// PnL 계산
	var realizedPnL float64
	if pos.Direction == "LONG" {
		realizedPnL = exitValue - pos.SizeUSDT
	} else {
		realizedPnL = pos.SizeUSDT - exitValue
	}
	realizedPnL -= fee // 청산 수수료 차감

	// 잔액 환입
	p.AvailableCash += exitValue - fee
	p.TotalFeesPaid += fee

	pos.ExitPrice = adjustedPrice
	pos.ExitTime = time.Now()
	pos.Status = "CLOSED"
	pos.RealizedPnL = realizedPnL
	pos.ExitReason = reason

	// 거래 이력 기록
	trade := PaperTrade{
		ID:          pos.ID,
		Pair:        pair,
		Direction:   pos.Direction,
		EntryPrice:  pos.EntryPrice,
		ExitPrice:   adjustedPrice,
		SizeUSDT:    pos.SizeUSDT,
		RealizedPnL: realizedPnL,
		FeePaid:     pos.FeePaid + fee,
		EntryTime:   pos.EntryTime,
		ExitTime:    pos.ExitTime,
		Duration:    pos.ExitTime.Sub(pos.EntryTime),
		ExitReason:  reason,
		Source:      p.Name,
	}
	p.TradeHistory = append(p.TradeHistory, trade)

	// 닫힌 포지션 제거
	delete(p.Positions, pair)

	return nil
}

// UpdatePrice 현재가 업데이트 (미실현 PnL 계산)
func (p *PaperPocket) UpdatePrice(pair string, price float64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	pos, exists := p.Positions[pair]
	if !exists || pos.Status != "OPEN" {
		return
	}

	pos.CurrentPrice = price

	if pos.Direction == "LONG" {
		pos.UnrealizedPnL = (price-pos.EntryPrice)/pos.EntryPrice*pos.SizeUSDT
	} else {
		pos.UnrealizedPnL = (pos.EntryPrice-price)/pos.EntryPrice*pos.SizeUSDT
	}
}

// CloseAllPositions 모든 포지션 강제 청산
func (p *PaperPocket) CloseAllPositions(priceFunc func(pair string) float64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for pair, pos := range p.Positions {
		if pos.Status != "OPEN" {
			continue
		}
		currentPrice := priceFunc(pair)
		if currentPrice <= 0 {
			currentPrice = pos.CurrentPrice
		}
		p.mu.Unlock()
		p.ClosePosition(pair, currentPrice, "force_close")
		p.mu.Lock()
	}
}

// GetMetrics 성과 지표 계산
func (p *PaperPocket) GetMetrics() *PocketMetrics {
	p.mu.RLock()
	defer p.mu.RUnlock()

	m := &PocketMetrics{}
	totalPnL := 0.0
	totalWins := 0.0
	totalLosses := 0.0
	var returns []float64
	var peak float64

	for _, trade := range p.TradeHistory {
		totalPnL += trade.RealizedPnL
		m.TotalTrades++
		if trade.RealizedPnL > 0 {
			m.WinningTrades++
			totalWins += trade.RealizedPnL
		} else {
			m.LosingTrades++
			totalLosses += math.Abs(trade.RealizedPnL)
		}
		returns = append(returns, trade.RealizedPnL/trade.SizeUSDT)
	}

	// 미실현 PnL 포함
	unrealized := 0.0
	for _, pos := range p.Positions {
		if pos.Status == "OPEN" {
			unrealized += pos.UnrealizedPnL
		}
	}

	m.TotalReturn = totalPnL + unrealized
	m.TotalReturnPct = m.TotalReturn / p.InitialCapital * 100

	if m.TotalTrades > 0 {
		m.WinRate = float64(m.WinningTrades) / float64(m.TotalTrades) * 100
	}
	if totalLosses > 0 {
		m.ProfitFactor = totalWins / totalLosses
	} else if totalWins > 0 {
		m.ProfitFactor = 10.0
	}
	if m.WinningTrades > 0 {
		m.AvgWinPnL = totalWins / float64(m.WinningTrades)
	}
	if m.LosingTrades > 0 {
		m.AvgLossPnL = totalLosses / float64(m.LosingTrades)
	}

	// Sharpe Ratio (간이 계산)
	if len(returns) > 1 {
		mean := 0.0
		for _, r := range returns {
			mean += r
		}
		mean /= float64(len(returns))

		variance := 0.0
		for _, r := range returns {
			variance += (r - mean) * (r - mean)
		}
		variance /= float64(len(returns))
		stddev := math.Sqrt(variance)

		if stddev > 0 {
			// 연간화 (5분봉, 288봉/일, 365일)
			m.SharpeRatio = (mean / stddev) * math.Sqrt(288*365)
		}
	}

	// Max Drawdown
	peak = p.InitialCapital
	drawdown := 0.0
	for _, trade := range p.TradeHistory {
		peak += trade.RealizedPnL
		if peak > 0 {
			dd := (peak - (peak - trade.RealizedPnL)) / peak
			if dd > drawdown {
				drawdown = dd
			}
		}
	}
	// 현재 포지션 고려
	currentValue := p.InitialCapital + m.TotalReturn
	if peak > 0 && currentValue < peak {
		dd := (peak - currentValue) / peak
		if dd > drawdown {
			drawdown = dd
		}
	}
	m.MaxDrawdown = drawdown * 100

	return m
}

// GetEquity 현재 자산 가치 (현금 + 포지션)
func (p *PaperPocket) GetEquity() float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()

	equity := p.AvailableCash
	for _, pos := range p.Positions {
		if pos.Status == "OPEN" {
			equity += pos.SizeCoin * pos.CurrentPrice
		}
	}
	return equity
}

// GetOpenPositionCount 오픈된 포지션 수
func (p *PaperPocket) GetOpenPositionCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	count := 0
	for _, pos := range p.Positions {
		if pos.Status == "OPEN" {
			count++
		}
	}
	return count
}
