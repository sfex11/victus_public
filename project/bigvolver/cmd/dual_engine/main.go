package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"dsl-strategy-evolver/internal/data"
	"dsl-strategy-evolver/internal/engine"
)

func main() {
	log.Println("╔══════════════════════════════════════════════╗")
	log.Println("║  ⬡ BigVolver Dual Engine — NFI vs BigVolver  ║")
	log.Println("╚══════════════════════════════════════════════╝")

	// 설정 로드
	_ = os.Args // configPath는 YAML 파서 도입 시 사용

	// 기본 설정 (YAML 파서 없이 하드코딩된 기본값 사용)
	paperConfig := &engine.PaperTradeConfig{
		InitialCapital: 10000.0,
		FeeRate:        0.001,
		Slippage:       0.01,
		MaxPositions:   8,
		StakeCurrency:  "USDT",
		Timeframe:      "5m",
	}

	// 환경변수 오버라이드
	if v := os.Getenv("INITIAL_CAPITAL"); v != "" {
		fmt.Sscanf(v, "%f", &paperConfig.InitialCapital)
	}
	if v := os.Getenv("MAX_POSITIONS"); v != "" {
		fmt.Sscanf(v, "%d", &paperConfig.MaxPositions)
	}

	dualConfig := &engine.DualEngineConfig{
		PaperTrade:   paperConfig,
		WebhookPort:  8080,
		EvalInterval: 5 * time.Minute,
	}
	if v := os.Getenv("WEBHOOK_PORT"); v != "" {
		fmt.Sscanf(v, "%d", &dualConfig.WebhookPort)
	}

	// Freqtrade DB 연결
	freqtradeDBPath := os.Getenv("FREQTRADE_DB_PATH")
	if freqtradeDBPath == "" {
		// 기본 경로
		freqtradeDBPath = "./docker/freqtrade/user_data/data/tradesv3.sqlite"
	}

	freqtradeAdapter, err := data.NewFreqtradeAdapter(freqtradeDBPath)
	if err != nil {
		log.Printf("⚠️  Freqtrade DB 연결 실패 (%s): %v", freqtradeDBPath, err)
		log.Println("    Freqtrade 컨테이너가 시작되면 자동으로 연결을 재시도합니다.")
		freqtradeAdapter = nil
	} else {
		log.Printf("✅ Freqtrade DB 연결 성공: %s", freqtradeDBPath)
		defer freqtradeAdapter.Close()
	}

	// 듀얼 엔진 생성
	dualEngine := engine.NewDualEngine(dualConfig)

	// 가격 제공자 설정
	if freqtradeAdapter != nil {
		dualEngine.SetPriceProvider(func(pair string) float64 {
			// Freqtrade 페어명: BTC/USDT → DB에서는 BTCUSDT로 변환
			dbPair := pair
			price, err := freqtradeAdapter.GetCurrentPrice(dbPair)
			if err != nil {
				return 0
			}
			return price
		})
		log.Println("✅ 가격 제공자 설정 완료 (Freqtrade DB)")
	} else {
		// Freqtrade API로 폴백
		dualEngine.SetPriceProvider(func(pair string) float64 {
			return fetchPriceFromFreqtradeAPI(pair)
		})
		log.Println("✅ 가격 제공자 설정 완료 (Freqtrade API 폴백)")
	}

	// 엔진 시작
	if err := dualEngine.Start(); err != nil {
		log.Fatalf("❌ 엔진 시작 실패: %v", err)
	}

	log.Printf("🌐 Webhook 서버: http://localhost:%d", dualConfig.WebhookPort)
	log.Printf("📊 API 엔드포인트:")
	log.Printf("   GET  http://localhost:%d/api/v1/status", dualConfig.WebhookPort)
	log.Printf("   GET  http://localhost:%d/api/v1/compare", dualConfig.WebhookPort)
	log.Printf("   GET  http://localhost:%d/api/v1/signals", dualConfig.WebhookPort)
	log.Printf("   POST http://localhost:%d/api/v1/nfi/signals (Freqtrade webhook)", dualConfig.WebhookPort)
	log.Println("")
	log.Println("대기 중... (Ctrl+C로 종료)")

	// 종료 시그널 대기
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("종료 중...")
	dualEngine.Stop()
	log.Println("종료 완료")
}

// fetchPriceFromFreqtradeAPI Freqtrade REST API에서 현재가 조회 (폴백)
func fetchPriceFromFreqtradeAPI(pair string) float64 {
	// Freqtrade API: GET /api/v1/pair_candles/{pair}/{timeframe}
	url := fmt.Sprintf("http://localhost:8081/api/v1/pair_candles/%s/5m", pair)
	resp, err := http.Get(url)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return 0
	}

	var candles []struct {
		Close float64 `json:"close"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&candles); err != nil {
		return 0
	}

	if len(candles) == 0 {
		return 0
	}

	return candles[len(candles)-1].Close
}
