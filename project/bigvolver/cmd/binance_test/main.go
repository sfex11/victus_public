package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	apiKey    = "6FT9COyvVIDCyWWIBFlQivcoBcCFEDyCFdauXm2SQW4zSsGnMGLIIpCC4yDZOFH4"
	secretKey = "diid5757"
	baseURL   = "https://api.binance.com"
)

// Binance API 요청에 HMAC-SHA256 서명 생성
func sign(query string) string {
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(query))
	return hex.EncodeToString(mac.Sum(nil))
}

// 공개 엔드포인트 테스트 (서명 불필요)
func testPublicEndpoints() {
	fmt.Println("=== 공개 엔드포인트 테스트 ===")

	// 1. 서버 시간
	testEndpoint("/api/v3/time", "서버 시간")

	// 2. BTC/USDT 가격
	testEndpoint("/api/v3/ticker/price?symbol=BTCUSDT", "BTC 가격")

	// 3. USDT 잔고 페어 24시간 정보
	testEndpoint("/api/v3/ticker/24hr?symbol=BTCUSDT", "BTC 24시간 정보")
}

// 인증 엔드포인트 테스트 (서명 필요)
func testPrivateEndpoints() {
	fmt.Println("\n=== 인증 엔드포인트 테스트 ===")

	timestamp := fmt.Sprintf("%d", time.Now().UnixMilli())

	// 1. 계정 정보
	testSigned("/api/v3/account", "계정 정보", timestamp)

	// 2. USDT 잔고
	testSigned("/api/v3/balance", "USDT 잔고", timestamp)

	// 3. 거래 내역 (최근 5건)
	testSigned("/api/v3/myTrades?symbol=BTCUSDT&limit=5", "BTC 거래 내역", timestamp)
}

func testPublicEndpoints... // 아래에 정의
