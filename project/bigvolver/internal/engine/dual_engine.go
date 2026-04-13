package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// DualEngine은 NFI와 BigVolver ML의 듀얼 가상매매를 관리
type DualEngine struct {
	mu             sync.RWMutex
	config         *PaperTradeConfig
	nfiPocket      *PaperPocket
	bigvPocket     *PaperPocket
	signalBus      *SignalBus
	comparator     *Comparator
	httpServer     *http.Server
	ctx            context.Context
	cancel         context.CancelFunc
	running        bool

	// 가격 업데이트 콜백 (Freqtrade Adapter 연동용)
	priceProvider func(pair string) float64
}

// DualEngineConfig 듀얼 엔진 설정
type DualEngineConfig struct {
	PaperTrade   *PaperTradeConfig
	WebhookPort  int  // NFI webhook 수신 포트
	EvalInterval time.Duration // 평가 주기
}

// NewDualEngine 새로운 듀얼 엔진 생성
func NewDualEngine(cfg *DualEngineConfig) *DualEngine {
	ctx, cancel := context.WithCancel(context.Background())

	signalBus := NewSignalBus(10000)
	nfiPocket := NewPaperPocket("NFI", cfg.PaperTrade)
	bigvPocket := NewPaperPocket("BIGVOLVER", cfg.PaperTrade)
	comparator := NewComparator(nfiPocket, bigvPocket, signalBus)

	engine := &DualEngine{
		config:     cfg.PaperTrade,
		nfiPocket:  nfiPocket,
		bigvPocket: bigvPocket,
		signalBus:  signalBus,
		comparator: comparator,
		ctx:        ctx,
		cancel:     cancel,
	}

	// HTTP 서버 설정 (NFI webhook + API)
	mux := http.NewServeMux()
	webhookHandler := NewNFIWebhookHandler(signalBus)
	mux.HandleFunc("/api/v1/nfi/signals", webhookHandler.HandleWebhook)
	mux.HandleFunc("/api/v1/status", engine.handleStatus)
	mux.HandleFunc("/api/v1/compare", engine.handleCompare)
	mux.HandleFunc("/api/v1/signals", engine.handleSignals)

	addr := fmt.Sprintf(":%d", cfg.WebhookPort)
	engine.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	return engine
}

// SetPriceProvider 가격 제공자 설정 (Freqtrade Adapter 연동)
func (e *DualEngine) SetPriceProvider(fn func(pair string) float64) {
	e.priceProvider = fn
}

// Start 듀얼 엔진 시작
func (e *DualEngine) Start() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return fmt.Errorf("이미 실행 중")
	}

	e.running = true

	// HTTP 서버 시작 (NFI webhook 수신)
	go func() {
		log.Printf("[DualEngine] Webhook 서버 시작 (port %s)", e.httpServer.Addr)
		if err := e.httpServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("[DualEngine] HTTP 서버 오류: %v", err)
		}
	}()

	// 정기 평가 루프
	go e.evaluationLoop()

	log.Printf("[DualEngine] 듀얼 가상매매 엔진 시작 (초기 자본: %.0f %s)",
		e.config.InitialCapital, e.config.StakeCurrency)

	return nil
}

// Stop 듀얼 엔진 중지
func (e *DualEngine) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return nil
	}

	e.cancel()
	e.running = false

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	e.httpServer.Shutdown(ctx)

	return nil
}

// evaluationLoop 정기적으로 가격 업데이트 및 포지션 평가
func (e *DualEngine) evaluationLoop() {
	ticker := time.NewTicker(5 * time.Minute) // 5분봉에 맞춤
	defer ticker.Stop()

	// 즉시 한 번 실행
	e.runEvaluation()

	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			e.runEvaluation()
		}
	}
}

// runEvaluation 전체 평가 사이클
func (e *DualEngine) runEvaluation() {
	if e.priceProvider == nil {
		return
	}

	// 1. 가격 업데이트
	e.updateAllPrices()

	// 2. 시그널 처리 (BigVolver ML 시그널은 외부에서 Publish됨)

	// 3. 포지션 평가 및 비교
	result := e.comparator.Evaluate("5m")
	log.Printf("[DualEngine] %s", result.Summary())
}

// updateAllPrices 모든 오픈 포지션의 가격 업데이트
func (e *DualEngine) updateAllPrices() {
	e.nfiPocket.mu.RLock()
	nfiPairs := make([]string, 0)
	for pair, pos := range e.nfiPocket.Positions {
		if pos.Status == "OPEN" {
			nfiPairs = append(nfiPairs, pair)
		}
	}
	e.nfiPocket.mu.RUnlock()

	e.bigvPocket.mu.RLock()
	bvPairs := make([]string, 0)
	for pair, pos := range e.bigvPocket.Positions {
		if pos.Status == "OPEN" {
			bvPairs = append(bvPairs, pair)
		}
	}
	e.bigvPocket.mu.RUnlock()

	// 중복 제거
	allPairs := make(map[string]bool)
	for _, p := range nfiPairs {
		allPairs[p] = true
	}
	for _, p := range bvPairs {
		allPairs[p] = true
	}

	for pair := range allPairs {
		price := e.priceProvider(pair)
		if price > 0 {
			e.nfiPocket.UpdatePrice(pair, price)
			e.bigvPocket.UpdatePrice(pair, price)
		}
	}
}

// PublishBigVolverSignal BigVolver ML 시그널 발행
func (e *DualEngine) PublishBigVolverSignal(pair, direction string, confidence float64) {
	signal := TradingSignal{
		Timestamp:  time.Now(),
		Source:     "BIGVOLVER_ML",
		Pair:       pair,
		Direction:  direction,
		Confidence: confidence,
		Mode:       "ml_predicted",
		Reason:     fmt.Sprintf("BigVolver ML (conf: %.3f)", confidence),
	}

	e.signalBus.Publish(signal)

	// 시그널 처리
	if direction == "LONG" || direction == "SHORT" {
		price := e.priceProvider(pair)
		if price > 0 {
			err := e.bigvPocket.OpenPosition(pair, direction, price)
			if err != nil {
				log.Printf("[DualEngine] BigV 포지션 오픈 실패 (%s): %v", pair, err)
			} else {
				log.Printf("[DualEngine] BigV %s %s 진입 (가격: %.6f)", direction, pair, price)
			}
		}
	} else if direction == "CLOSE" {
		price := e.priceProvider(pair)
		if price > 0 {
			err := e.bigvPocket.ClosePosition(pair, price, "ml_close")
			if err != nil {
				log.Printf("[DualEngine] BigV 포지션 청산 실패 (%s): %v", pair, err)
			}
		}
	}
}

// ProcessNFISignal NFI 시그널 처리 (signal_bus에서 수신된 시그널 처리)
func (e *DualEngine) ProcessNFISignal(signal TradingSignal) {
	if signal.Source != "NFI" {
		return
	}

	if signal.Direction == "LONG" {
		price := e.priceProvider(signal.Pair)
		if price > 0 {
			err := e.nfiPocket.OpenPosition(signal.Pair, "LONG", price)
			if err != nil {
				log.Printf("[DualEngine] NFI 포지션 오픈 실패 (%s): %v", signal.Pair, err)
			} else {
				log.Printf("[DualEngine] NFI LONG %s 진입 (모드: %s, 가격: %.6f)", signal.Pair, signal.Mode, price)
			}
		}
	} else if signal.Direction == "CLOSE" {
		price := e.priceProvider(signal.Pair)
		if price > 0 {
			err := e.nfiPocket.ClosePosition(signal.Pair, price, "nfi_close")
			if err != nil {
				log.Printf("[DualEngine] NFI 포지션 청산 실패 (%s): %v", signal.Pair, err)
			}
		}
	}
}

// GetStatus 시스템 상태 반환
func (e *DualEngine) GetStatus() map[string]interface{} {
	nfiMetrics := e.nfiPocket.GetMetrics()
	bvMetrics := e.bigvPocket.GetMetrics()
	latest := e.comparator.GetLatestResult()

	return map[string]interface{}{
		"running":           e.running,
		"initial_capital":   e.config.InitialCapital,
		"nfi": map[string]interface{}{
			"equity":        e.nfiPocket.GetEquity(),
			"cash":          e.nfiPocket.AvailableCash,
			"open_positions": e.nfiPocket.GetOpenPositionCount(),
			"total_trades":  nfiMetrics.TotalTrades,
			"win_rate":      nfiMetrics.WinRate,
			"sharpe":        nfiMetrics.SharpeRatio,
			"total_pnl":     nfiMetrics.TotalReturn,
			"max_dd":        nfiMetrics.MaxDrawdown,
		},
		"bigvolver": map[string]interface{}{
			"equity":        e.bigvPocket.GetEquity(),
			"cash":          e.bigvPocket.AvailableCash,
			"open_positions": e.bigvPocket.GetOpenPositionCount(),
			"total_trades":  bvMetrics.TotalTrades,
			"win_rate":      bvMetrics.WinRate,
			"sharpe":        bvMetrics.SharpeRatio,
			"total_pnl":     bvMetrics.TotalReturn,
			"max_dd":        bvMetrics.MaxDrawdown,
		},
		"comparison": latest,
		"signal_agreement": e.signalBus.GetSignalAgreement(24 * time.Hour),
	}
}

// GetEquityHistory 자산 변화 시계열
func (e *DualEngine) GetEquityHistory() []EquitySnapshot {
	return e.comparator.GetEquityHistory()
}

// HTTP 핸들러들

func (e *DualEngine) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(e.GetStatus())
}

func (e *DualEngine) handleCompare(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	result := e.comparator.Evaluate("on_demand")
	json.NewEncoder(w).Encode(result)
}

func (e *DualEngine) handleSignals(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	signals := e.signalBus.GetRecentSignals(50)
	json.NewEncoder(w).Encode(signals)
}
