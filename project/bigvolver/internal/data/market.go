package data

import (
	"database/sql"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// MarketDataRepository handles market data queries
type MarketDataRepository struct {
	db *Database
}

// NewMarketDataRepository creates a new market data repository
func NewMarketDataRepository(db *Database) *MarketDataRepository {
	return &MarketDataRepository{db: db}
}

// Candle represents a market candle
type Candle struct {
	Symbol    string
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	Timestamp int64
}

// FundingRate represents funding rate data
type FundingRate struct {
	Symbol    string
	Rate      float64
	Timestamp int64
}

// GetDB returns the underlying SQL DB for raw queries
func (r *MarketDataRepository) getSQLDB() (*sql.DB, error) {
	return r.db.GetDB().DB()
}

// GetLatestCandles retrieves the latest N candles for a symbol
func (r *MarketDataRepository) GetLatestCandles(symbol string, limit int) ([]*Candle, error) {
	query := `
		SELECT symbol, open, high, low, close, volume, timestamp
		FROM market_1h_candles
		WHERE symbol = ?
		ORDER BY timestamp DESC
		LIMIT ?
	`

	sqlDB, err := r.getSQLDB()
	if err != nil {
		return nil, err
	}

	rows, err := sqlDB.Query(query, symbol, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query candles: %w", err)
	}
	defer rows.Close()

	var candles []*Candle
	for rows.Next() {
		candle := &Candle{}
		err := rows.Scan(
			&candle.Symbol,
			&candle.Open,
			&candle.High,
			&candle.Low,
			&candle.Close,
			&candle.Volume,
			&candle.Timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan candle: %w", err)
		}
		candles = append(candles, candle)
	}

	// Reverse to have oldest first
	for i, j := 0, len(candles)-1; i < j; i, j = i+1, j-1 {
		candles[i], candles[j] = candles[j], candles[i]
	}

	return candles, nil
}

// GetCandlesInRange retrieves candles for a time range
func (r *MarketDataRepository) GetCandlesInRange(symbol string, startTime, endTime int64) ([]*Candle, error) {
	query := `
		SELECT symbol, open, high, low, close, volume, timestamp
		FROM market_1h_candles
		WHERE symbol = ? AND timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp ASC
	`

	sqlDB, err := r.getSQLDB()
	if err != nil {
		return nil, err
	}

	rows, err := sqlDB.Query(query, symbol, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("failed to query candles: %w", err)
	}
	defer rows.Close()

	var candles []*Candle
	for rows.Next() {
		candle := &Candle{}
		err := rows.Scan(
			&candle.Symbol,
			&candle.Open,
			&candle.High,
			&candle.Low,
			&candle.Close,
			&candle.Volume,
			&candle.Timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan candle: %w", err)
		}
		candles = append(candles, candle)
	}

	return candles, nil
}

// GetLatestCandle retrieves the most recent candle for a symbol
func (r *MarketDataRepository) GetLatestCandle(symbol string) (*Candle, error) {
	query := `
		SELECT symbol, open, high, low, close, volume, timestamp
		FROM market_1h_candles
		WHERE symbol = ?
		ORDER BY timestamp DESC
		LIMIT 1
	`

	sqlDB, err := r.getSQLDB()
	if err != nil {
		return nil, err
	}

	candle := &Candle{}
	err = sqlDB.QueryRow(query, symbol).Scan(
		&candle.Symbol,
		&candle.Open,
		&candle.High,
		&candle.Low,
		&candle.Close,
		&candle.Volume,
		&candle.Timestamp,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no candle found for symbol %s", symbol)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query candle: %w", err)
	}

	return candle, nil
}

// GetLatestFundingRate retrieves the latest funding rate for a symbol
func (r *MarketDataRepository) GetLatestFundingRate(symbol string) (*FundingRate, error) {
	query := `
		SELECT symbol, funding_rate, funding_time
		FROM market_funding_rate
		WHERE symbol = ?
		ORDER BY funding_time DESC
		LIMIT 1
	`

	sqlDB, err := r.getSQLDB()
	if err != nil {
		return nil, err
	}

	rate := &FundingRate{}
	err = sqlDB.QueryRow(query, symbol).Scan(
		&rate.Symbol,
		&rate.Rate,
		&rate.Timestamp,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no funding rate found for symbol %s", symbol)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query funding rate: %w", err)
	}

	return rate, nil
}

// GetAvailableSymbols retrieves all available symbols
func (r *MarketDataRepository) GetAvailableSymbols() ([]string, error) {
	query := `
		SELECT DISTINCT symbol
		FROM market_1h_candles
		ORDER BY symbol
	`

	sqlDB, err := r.getSQLDB()
	if err != nil {
		return nil, err
	}

	rows, err := sqlDB.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query symbols: %w", err)
	}
	defer rows.Close()

	var symbols []string
	for rows.Next() {
		var symbol string
		if err := rows.Scan(&symbol); err != nil {
			return nil, fmt.Errorf("failed to scan symbol: %w", err)
		}
		symbols = append(symbols, symbol)
	}

	return symbols, nil
}

// GetHistoricalDataForBacktest retrieves historical data for backtesting
func (r *MarketDataRepository) GetHistoricalDataForBacktest(symbol string, days int) ([]*Candle, error) {
	startTime := time.Now().AddDate(0, 0, -days).Unix()

	query := `
		SELECT symbol, open, high, low, close, volume, timestamp
		FROM market_1h_candles
		WHERE symbol = ? AND timestamp >= ?
		ORDER BY timestamp ASC
	`

	sqlDB, err := r.getSQLDB()
	if err != nil {
		return nil, err
	}

	rows, err := sqlDB.Query(query, symbol, startTime)
	if err != nil {
		return nil, fmt.Errorf("failed to query historical candles: %w", err)
	}
	defer rows.Close()

	var candles []*Candle
	for rows.Next() {
		candle := &Candle{}
		err := rows.Scan(
			&candle.Symbol,
			&candle.Open,
			&candle.High,
			&candle.Low,
			&candle.Close,
			&candle.Volume,
			&candle.Timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan candle: %w", err)
		}
		candles = append(candles, candle)
	}

	return candles, nil
}

// GetCandleCount returns the number of candles for a symbol
func (r *MarketDataRepository) GetCandleCount(symbol string) (int, error) {
	var count int
	sqlDB, err := r.getSQLDB()
	if err != nil {
		return 0, err
	}

	err = sqlDB.QueryRow(
		"SELECT COUNT(*) FROM market_1h_candles WHERE symbol = ?",
		symbol,
	).Scan(&count)

	if err != nil {
		return 0, fmt.Errorf("failed to count candles: %w", err)
	}

	return count, nil
}

// GetDataStartDate returns the earliest date for which we have data
func (r *MarketDataRepository) GetDataStartDate(symbol string) (time.Time, error) {
	var timestamp int64
	sqlDB, err := r.getSQLDB()
	if err != nil {
		return time.Time{}, err
	}

	err = sqlDB.QueryRow(
		"SELECT MIN(timestamp) FROM market_1h_candles WHERE symbol = ?",
		symbol,
	).Scan(&timestamp)

	if err != nil {
		return time.Time{}, fmt.Errorf("failed to get start date: %w", err)
	}

	return time.Unix(timestamp, 0), nil
}

// GetDataEndDate returns the latest date for which we have data
func (r *MarketDataRepository) GetDataEndDate(symbol string) (time.Time, error) {
	var timestamp int64
	sqlDB, err := r.getSQLDB()
	if err != nil {
		return time.Time{}, err
	}

	err = sqlDB.QueryRow(
		"SELECT MAX(timestamp) FROM market_1h_candles WHERE symbol = ?",
		symbol,
	).Scan(&timestamp)

	if err != nil {
		return time.Time{}, fmt.Errorf("failed to get end date: %w", err)
	}

	return time.Unix(timestamp, 0), nil
}

// GetDB returns the gorm DB (for other packages)
func (d *Database) GetGormDB() *gorm.DB {
	return d.db
}
