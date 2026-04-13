package data

import (
	"database/sql"
	"fmt"
	"time"
)

// FreqtradeAdapter는 Freqtrade의 SQLite DB에서 캔들 데이터를 읽어 BigVolver 포맷으로 변환
// Freqtrade는 candles 테이블에 OHLCV 데이터를 저장 (v2.x 기준)
type FreqtradeAdapter struct {
	dbPath string
	db     *sql.DB
}

// NewFreqtradeAdapter 생성
func NewFreqtradeAdapter(dbPath string) (*FreqtradeAdapter, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("Freqtrade DB 열기 실패: %w", err)
	}

	// 연결 테스트
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("Freqtrade DB 연결 실패: %w", err)
	}

	return &FreqtradeAdapter{dbPath: dbPath, db: db}, nil
}

// Close DB 연결 종료
func (f *FreqtradeAdapter) Close() error {
	if f.db != nil {
		return f.db.Close()
	}
	return nil
}

// GetLatestCandles Freqtrade DB에서 최근 N개 캔들 조회
// Freqtrade v2.x: 테이블명은 `fts_candle_1m` 또는 `candles` (버전에 따라 다름)
// 공통적으로 pair_id를 조인하여 페어명을 가져옴
func (f *FreqtradeAdapter) GetLatestCandles(pair string, timeframe string, limit int) ([]*Candle, error) {
	// Freqtrade는 pair_id 기반으로 데이터를 저장
	// 캔들 테이블 구조: id, pair_id, timeframe, date (timestamp), open, high, low, close, volume

	// pair_id 조회
	pairID, err := f.getPairID(pair)
	if err != nil {
		return nil, err
	}

	// 캔들 조회 - Freqtrade 테이블명 자동 감지
	query := fmt.Sprintf(`
		SELECT date, open, high, low, close, volume
		FROM candles
		WHERE pair_id = ? AND timeframe = ?
		ORDER BY date DESC
		LIMIT ?
	`)

	rows, err := f.db.Query(query, pairID, timeframe, limit)
	if err != nil {
		// 대안 테이블 시도 (Freqtrade 구버전)
		return f.getLatestCandlesLegacy(pair, timeframe, limit)
	}
	defer rows.Close()

	var candles []*Candle
	for rows.Next() {
		var ts int64
		candle := &Candle{Symbol: pair}
		if err := rows.Scan(&ts, &candle.Open, &candle.High, &candle.Low, &candle.Close, &candle.Volume); err != nil {
			return nil, fmt.Errorf("캔들 스캔 실패: %w", err)
		}
		candle.Timestamp = ts
		candles = append(candles, candle)
	}

	// 오래된 것부터 정렬
	for i, j := 0, len(candles)-1; i < j; i, j = i+1, j-1 {
		candles[i], candles[j] = candles[j], candles[i]
	}

	return candles, nil
}

// getLatestCandlesLegacy 구버전 Freqtrade 호환
func (f *FreqtradeAdapter) getLatestCandlesLegacy(pair string, timeframe string, limit int) ([]*Candle, error) {
	// 구버전 Freqtrade는 별도 캔들 테이블 없이 trades에서 파생
	// 또는 tradedata 테이블 사용
	query := `
		SELECT open_time, open, high, low, close, volume
		FROM tradedata
		WHERE pair = ? AND timeframe = ?
		ORDER BY open_time DESC
		LIMIT ?
	`

	rows, err := f.db.Query(query, pair, timeframe, limit)
	if err != nil {
		return nil, fmt.Errorf("구버전 캔들 조회 실패: %w", err)
	}
	defer rows.Close()

	var candles []*Candle
	for rows.Next() {
		candle := &Candle{Symbol: pair}
		if err := rows.Scan(&candle.Timestamp, &candle.Open, &candle.High, &candle.Low, &candle.Close, &candle.Volume); err != nil {
			return nil, fmt.Errorf("캔들 스캔 실패: %w", err)
		}
		candles = append(candles, candle)
	}

	for i, j := 0, len(candles)-1; i < j; i, j = i+1, j-1 {
		candles[i], candles[j] = candles[j], candles[i]
	}

	return candles, nil
}

// getPairID 페어명으로 pair_id 조회
func (f *FreqtradeAdapter) getPairID(pair string) (int, error) {
	var pairID int
	err := f.db.QueryRow("SELECT id FROM pairs WHERE pair = ?", pair).Scan(&pairID)
	if err == sql.ErrNoRows {
		// 다른 컬럼명 시도
		err = f.db.QueryRow("SELECT id FROM trades WHERE pair = ? LIMIT 1", pair).Scan(&pairID)
	}
	if err != nil {
		return 0, fmt.Errorf("pair_id 조회 실패 (%s): %w", pair, err)
	}
	return pairID, nil
}

// GetAvailablePairs 사용 가능한 페어 목록 조회
func (f *FreqtradeAdapter) GetAvailablePairs() ([]string, error) {
	query := "SELECT DISTINCT pair FROM pairs ORDER BY pair"
	rows, err := f.db.Query(query)
	if err != nil {
		// 대안 쿼리
		query = "SELECT DISTINCT pair FROM trades ORDER BY pair"
		rows, err = f.db.Query(query)
	}
	if err != nil {
		return nil, fmt.Errorf("페어 목록 조회 실패: %w", err)
	}
	defer rows.Close()

	var pairs []string
	for rows.Next() {
		var pair string
		if err := rows.Scan(&pair); err != nil {
			continue
		}
		pairs = append(pairs, pair)
	}

	return pairs, nil
}

// GetCandlesInRange 시간 범위로 캔들 조회 (백테스트용)
func (f *FreqtradeAdapter) GetCandlesInRange(pair string, timeframe string, start, end time.Time) ([]*Candle, error) {
	pairID, err := f.getPairID(pair)
	if err != nil {
		return nil, err
	}

	startTs := start.Unix()
	endTs := end.Unix()

	query := `
		SELECT date, open, high, low, close, volume
		FROM candles
		WHERE pair_id = ? AND timeframe = ? AND date >= ? AND date <= ?
		ORDER BY date ASC
	`

	rows, err := f.db.Query(query, pairID, timeframe, startTs, endTs)
	if err != nil {
		return nil, fmt.Errorf("범위 캔들 조회 실패: %w", err)
	}
	defer rows.Close()

	var candles []*Candle
	for rows.Next() {
		candle := &Candle{Symbol: pair}
		if err := rows.Scan(&candle.Timestamp, &candle.Open, &candle.High, &candle.Low, &candle.Close, &candle.Volume); err != nil {
			return nil, fmt.Errorf("캔들 스캔 실패: %w", err)
		}
		candles = append(candles, candle)
	}

	return candles, nil
}

// GetCurrentPrice 최근 종가 (현재가) 반환
func (f *FreqtradeAdapter) GetCurrentPrice(pair string) (float64, error) {
	candles, err := f.GetLatestCandles(pair, "5m", 1)
	if err != nil {
		return 0, err
	}
	if len(candles) == 0 {
		return 0, fmt.Errorf("데이터 없음: %s", pair)
	}
	return candles[len(candles)-1].Close, nil
}

// GetAllCurrentPrices 모든 페어의 현재가 반환
func (f *FreqtradeAdapter) GetAllCurrentPrices(timeframe string) (map[string]float64, error) {
	pairs, err := f.GetAvailablePairs()
	if err != nil {
		return nil, err
	}

	prices := make(map[string]float64)
	for _, pair := range pairs {
		price, err := f.GetCurrentPrice(pair)
		if err != nil {
			continue
		}
		prices[pair] = price
	}

	return prices, nil
}
