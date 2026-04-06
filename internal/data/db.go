package data

import (
	"fmt"
	"sync"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// Database handles database connections and queries
type Database struct {
	db   *gorm.DB
	path string
	mu   sync.RWMutex
}

// NewDatabase creates a new database connection
func NewDatabase(dbPath string) (*Database, error) {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Get underlying SQL DB for connection pool settings
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying DB: %w", err)
	}

	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	// Enable WAL mode
	if err := db.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	// Test connection
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	database := &Database{
		db:   db,
		path: dbPath,
	}

	// Initialize tables
	if err := database.initTables(); err != nil {
		return nil, fmt.Errorf("failed to initialize tables: %w", err)
	}

	return database, nil
}

// Strategy model
type Strategy struct {
	ID         string `gorm:"primaryKey"`
	Name       string `gorm:"not null"`
	YamlContent string `gorm:"not null"`
	CreatedAt  int64  `gorm:"not null"`
	Source     string
	ParentID   string
	Generation int    `gorm:"default:0"`
	Status     string `gorm:"default:'active'"`
}

// TableName sets custom table name
func (Strategy) TableName() string {
	return "evolver_strategies"
}

// PaperPosition model
type PaperPosition struct {
	ID             string  `gorm:"primaryKey"`
	StrategyID     string  `gorm:"not null;index"`
	Symbol         string  `gorm:"not null;index"`
	Side           string  `gorm:"not null"`
	SizeUSDT       float64 `gorm:"not null"`
	EntryPrice     float64 `gorm:"not null"`
	ExitPrice      *float64
	EntryTime      int64   `gorm:"not null"`
	ExitTime       *int64
	StopLoss       *float64
	TakeProfit     *float64
	UnrealizedPnL  float64 `gorm:"default:0"`
	RealizedPnL    float64 `gorm:"default:0"`
	Status         string  `gorm:"not null;default:'OPEN';index"`
}

func (PaperPosition) TableName() string {
	return "evolver_positions"
}

// PerformanceMetric model
type PerformanceMetric struct {
	ID           string  `gorm:"primaryKey"`
	StrategyID   string  `gorm:"not null;index"`
	Timestamp    int64   `gorm:"not null;index"`
	TotalReturn  float64 `gorm:"not null"`
	SharpeRatio  float64 `gorm:"not null"`
	WinRate      float64 `gorm:"not null"`
	MaxDrawdown  float64 `gorm:"not null"`
	ProfitFactor float64 `gorm:"not null"`
	TotalTrades  int     `gorm:"not null"`
}

func (PerformanceMetric) TableName() string {
	return "evolver_metrics"
}

// EvolutionHistory model
type EvolutionHistory struct {
	ID                 string  `gorm:"primaryKey"`
	CycleNumber        int     `gorm:"unique;not null"`
	Timestamp          int64   `gorm:"not null"`
	StrategiesGenerated int    `gorm:"not null"`
	StrategiesAdded    int     `gorm:"not null"`
	StrategiesRemoved  int     `gorm:"not null"`
	BestStrategyID     *string
	AvgSharpeRatio     *float64
}

func (EvolutionHistory) TableName() string {
	return "evolver_history"
}

// AIGenerationLog model
type AIGenerationLog struct {
	ID              string `gorm:"primaryKey"`
	Timestamp       int64  `gorm:"not null"`
	ModelUsed       string `gorm:"not null"`
	PromptHash      string `gorm:"not null"`
	StrategiesCount int    `gorm:"not null"`
	SuccessCount    int    `gorm:"not null"`
	ErrorMessage    *string
}

func (AIGenerationLog) TableName() string {
	return "evolver_ai_log"
}

// MarketCandle represents a 1-hour market candle
type MarketCandle struct {
	Symbol    string  `gorm:"primaryKey;not null"`
	Timestamp int64   `gorm:"primaryKey;not null"`
	Open      float64 `gorm:"not null"`
	High      float64 `gorm:"not null"`
	Low       float64 `gorm:"not null"`
	Close     float64 `gorm:"not null"`
	Volume    float64 `gorm:"not null"`
}

func (MarketCandle) TableName() string {
	return "market_1h_candles"
}

// MarketFundingRate represents funding rate data
type MarketFundingRate struct {
	Symbol      string  `gorm:"primaryKey;not null"`
	FundingTime int64   `gorm:"primaryKey;not null"`
	FundingRate float64 `gorm:"not null"`
}

func (MarketFundingRate) TableName() string {
	return "market_funding_rate"
}

// initTables creates necessary tables if they don't exist
func (d *Database) initTables() error {
	return d.db.AutoMigrate(
		&Strategy{},
		&PaperPosition{},
		&PerformanceMetric{},
		&EvolutionHistory{},
		&AIGenerationLog{},
		&MarketCandle{},
		&MarketFundingRate{},
	)
}

// Close closes the database connection
func (d *Database) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	sqlDB, err := d.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// GetDB returns the underlying database connection
func (d *Database) GetDB() *gorm.DB {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.db
}

// CreateStrategy creates a new strategy
func (d *Database) CreateStrategy(strategy *Strategy) error {
	return d.db.Create(strategy).Error
}

// GetStrategies returns all active strategies
func (d *Database) GetStrategies(limit int) ([]Strategy, error) {
	var strategies []Strategy
	err := d.db.Where("status = ?", "active").Limit(limit).Find(&strategies).Error
	return strategies, err
}

// CreatePosition creates a new position
func (d *Database) CreatePosition(position *PaperPosition) error {
	return d.db.Create(position).Error
}

// UpdatePosition updates a position
func (d *Database) UpdatePosition(position *PaperPosition) error {
	return d.db.Save(position).Error
}

// GetOpenPositions returns all open positions for a strategy
func (d *Database) GetOpenPositions(strategyID string) ([]PaperPosition, error) {
	var positions []PaperPosition
	err := d.db.Where("strategy_id = ? AND status = ?", strategyID, "OPEN").Find(&positions).Error
	return positions, err
}

// CreateMetric creates a new performance metric
func (d *Database) CreateMetric(metric *PerformanceMetric) error {
	return d.db.Create(metric).Error
}

// GetLatestMetrics returns the latest metrics for a strategy
func (d *Database) GetLatestMetrics(strategyID string) (*PerformanceMetric, error) {
	var metric PerformanceMetric
	err := d.db.Where("strategy_id = ?", strategyID).Order("timestamp DESC").First(&metric).Error
	if err != nil {
		return nil, err
	}
	return &metric, nil
}
