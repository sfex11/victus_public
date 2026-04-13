package engine

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// TradingSignal은 NFI와 BigVolver ML의 표준화된 시그널 포맷
type TradingSignal struct {
	Timestamp  time.Time `json:"timestamp"`
	Source     string    `json:"source"`    // "NFI" or "BIGVOLVER_ML"
	Pair       string    `json:"pair"`
	Direction  string    `json:"direction"` // "LONG", "SHORT", "CLOSE"
	Confidence float64   `json:"confidence"`
	Mode       string    `json:"mode"`      // NFI: normal/pump/quick, BigV: ml_predicted
	Reason     string    `json:"reason"`
}

// SignalBus는 두 시스템의 시그널을 중계하고 PaperTrade로 전달
type SignalBus struct {
	mu          sync.RWMutex
	signals     []TradingSignal
	subscribers []chan TradingSignal
	history     []TradingSignal // 전체 시그널 이력 (비교용)
	maxHistory  int
}

// NewSignalBus 새로운 시그널 버스 생성
func NewSignalBus(maxHistory int) *SignalBus {
	if maxHistory <= 0 {
		maxHistory = 10000
	}
	return &SignalBus{
		signals:     make([]TradingSignal, 0),
		subscribers: make([]chan TradingSignal, 0),
		history:     make([]TradingSignal, 0),
		maxHistory:  maxHistory,
	}
}

// Publish 시그널을 발행하고 모든 구독자에게 전달
func (sb *SignalBus) Publish(signal TradingSignal) {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	sb.signals = append(sb.signals, signal)
	sb.history = append(sb.history, signal)

	// 히스토리 크기 제한
	if len(sb.history) > sb.maxHistory {
		sb.history = sb.history[len(sb.history)-sb.maxHistory:]
	}

	// 최근 시그널만 유지
	if len(sb.signals) > 1000 {
		sb.signals = sb.signals[len(sb.signals)-1000:]
	}

	// 비동기로 구독자에게 전달
	for _, ch := range sb.subscribers {
		select {
		case ch <- signal:
		default:
			// 채널이 가득 차면 스킵 (블로킹 방지)
		}
	}
}

// Subscribe 시그널 구독
func (sb *SignalBus) Subscribe() chan TradingSignal {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	ch := make(chan TradingSignal, 100)
	sb.subscribers = append(sb.subscribers, ch)
	return ch
}

// GetRecentSignals 최근 N개 시그널 반환
func (sb *SignalBus) GetRecentSignals(n int) []TradingSignal {
	sb.mu.RLock()
	defer sb.mu.RUnlock()

	if n > len(sb.signals) {
		n = len(sb.signals)
	}
	return append([]TradingSignal(nil), sb.signals[len(sb.signals)-n:]...)
}

// GetHistory 전체 시그널 이력 반환 (비교 분석용)
func (sb *SignalBus) GetHistory() []TradingSignal {
	sb.mu.RLock()
	defer sb.mu.RUnlock()

	return append([]TradingSignal(nil), sb.history...)
}

// GetSignalsByPair 특정 페어의 시그널 반환
func (sb *SignalBus) GetSignalsByPair(pair string) []TradingSignal {
	sb.mu.RLock()
	defer sb.mu.RUnlock()

	var result []TradingSignal
	for _, s := range sb.history {
		if s.Pair == pair {
			result = append(result, s)
		}
	}
	return result
}

// GetSignalAgreement 두 시스템의 시그널 일치도 계산 (0.0 ~ 1.0)
func (sb *SignalBus) GetSignalAgreement(window time.Duration) float64 {
	sb.mu.RLock()
	defer sb.mu.RUnlock()

	cutoff := time.Now().Add(-window)
	agreements := 0
	disagreements := 0
	total := 0

	// NFI 시그널을 기준으로 BigVolver 시그널과 비교
	nfiSignals := make(map[string]TradingSignal) // pair -> latest signal in window
	bvSignals := make(map[string]TradingSignal)

	for _, s := range sb.history {
		if s.Timestamp.Before(cutoff) {
			continue
		}
		if s.Source == "NFI" {
			nfiSignals[s.Pair] = s
		} else if s.Source == "BIGVOLVER_ML" {
			bvSignals[s.Pair] = s
		}
	}

	// 공통 페어에 대해 방향 일치 여부 확인
	for pair, nfiSig := range nfiSignals {
		bvSig, exists := bvSignals[pair]
		if !exists {
			continue
		}
		total++
		if nfiSig.Direction == bvSig.Direction {
			agreements++
		} else {
			disagreements++
		}
	}

	if total == 0 {
		return 0.0
	}
	return float64(agreements) / float64(total)
}

// NFIWebhookHandler Freqtrade에서 NFI 시그널을 수신하는 HTTP 핸들러
type NFIWebhookHandler struct {
	bus *SignalBus
}

// NewNFIWebhookHandler 생성
func NewNFIWebhookHandler(bus *SignalBus) *NFIWebhookHandler {
	return &NFIWebhookHandler{bus: bus}
}

// HandleWebhook Freqtrade webhook POST 요청 처리
// Freqtrade config에 다음 설정 필요:
// webhooks:
//   url: http://localhost:8080/api/v1/nfi/signals
func (h *NFIWebhookHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		Type      string  `json:"type"`       // "entry" or "exit"
		Pair      string  `json:"pair"`
		TradeID   int     `json:"trade_id"`
		StakeAmount float64 `json:"stake_amount"`
		EnterTag  string  `json:"enter_tag"`  // NFI 모드 태그
		ExitReason string `json:"exit_reason"`
		OpenRate  float64 `json:"open_rate"`
		CloseRate float64 `json:"close_rate"`
		Profit    float64 `json:"profit"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// 시그널 변환
	signal := TradingSignal{
		Timestamp:  time.Now(),
		Source:     "NFI",
		Pair:       payload.Pair,
		Confidence: 1.0, // NFI는 rule-based이므로 항상 1.0
		Mode:       payload.EnterTag,
	}

	if payload.Type == "entry" {
		signal.Direction = "LONG" // NFI는 spot 전략, LONG만 해당
		signal.Reason = fmt.Sprintf("NFI entry (tag: %s, rate: %.6f)", payload.EnterTag, payload.OpenRate)
	} else if payload.Type == "exit" {
		signal.Direction = "CLOSE"
		signal.Reason = fmt.Sprintf("NFI exit (reason: %s, profit: %.4f)", payload.ExitReason, payload.Profit)
	}

	h.bus.Publish(signal)
	log.Printf("[SignalBus] NFI 시그널 수신: %s %s %s", signal.Pair, signal.Direction, signal.Mode)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
