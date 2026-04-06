package dsl

import (
	"sync"
	"time"
)

// Side represents position side
type PositionSide string

const (
	LongPosition  PositionSide = "LONG"
	ShortPosition PositionSide = "SHORT"
)

// StrategyType represents strategy type
type StrategyType string

const (
	HedgeType StrategyType = "hedge"
	LongType  StrategyType = "long"
	ShortType StrategyType = "short"
)

// Strategy represents a trading strategy
type Strategy struct {
	ID        string       `yaml:"-" json:"id"`
	Name      string       `yaml:"name" json:"name"`
	Symbol    string       `yaml:"symbol" json:"symbol"`
	Type      StrategyType `yaml:"type" json:"type"`
	Long      *SideConfig  `yaml:"long,omitempty" json:"long,omitempty"`
	Short     *SideConfig  `yaml:"short,omitempty" json:"short,omitempty"`
	Risk      *RiskConfig  `yaml:"risk" json:"risk"`
	CreatedAt time.Time    `yaml:"-" json:"created_at"`
	Source    string       `yaml:"-" json:"source"`    // 'ai_generated' | 'user_uploaded'
	ParentID  string       `yaml:"-" json:"parent_id"` // AI generation parent
	Generation int         `yaml:"-" json:"generation"` // Evolution generation
}

// SideConfig defines entry/exit conditions for a side
type SideConfig struct {
	Entry    string  `yaml:"entry" json:"entry"`
	Exit     string  `yaml:"exit" json:"exit"`
	StopLoss float64 `yaml:"stop_loss" json:"stop_loss"`
}

// RiskConfig defines risk management parameters
type RiskConfig struct {
	PositionSize  float64 `yaml:"position_size" json:"position_size"`
	MaxPositions  int     `yaml:"max_positions" json:"max_positions"`
	MaxDrawdown   float64 `yaml:"max_drawdown,omitempty" json:"max_drawdown"`
}

// EvaluationContext provides data for expression evaluation
type EvaluationContext struct {
	Mu          sync.RWMutex
	Price       float64
	Volume      float64
	Timestamp   int64
	FundingRate float64
	Indicators  map[string]float64 // ema, sma, rsi, etc.
	Candles     []*Candle
}

// Candle represents market data
type Candle struct {
	Symbol    string
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	Timestamp int64
}

// Position represents a trading position
type Position struct {
	ID            string
	StrategyID    string
	Symbol        string
	Side          PositionSide
	SizeUSDT      float64
	EntryPrice    float64
	CurrentPrice  float64
	UnrealizedPnL float64
	RealizedPnL   float64
	StopLoss      float64
	TakeProfit    float64
	EntryTime     int64
	ExitTime      int64
	ExitPrice     float64
	Status        string // 'OPEN' | 'CLOSED'
}

// PerformanceMetrics tracks strategy performance
type PerformanceMetrics struct {
	StrategyID       string
	TotalReturn      float64
	DailyReturn      float64
	WeeklyReturn     float64
	SharpeRatio      float64
	SortinoRatio     float64
	MaxDrawdown      float64
	WinRate          float64
	ProfitFactor     float64
	AvgTradeDuration float64
	TotalTrades      int
	LastUpdated      time.Time
}

// StrategyInstance represents a runtime strategy instance
type StrategyInstance struct {
	ID             string
	Strategy       *Strategy
	Positions      map[string]*Position // symbol -> Position
	LongEntry      *Expression
	LongExit       *Expression
	ShortEntry     *Expression
	ShortExit      *Expression
	Context        *EvaluationContext
	LastEvalTime   time.Time
	Metrics        *PerformanceMetrics
	TradeHistory   []*Trade
	State          string // 'active' | 'paused' | 'removed'
}

// Trade represents a completed trade
type Trade struct {
	ID         string
	StrategyID string
	Symbol     string
	Side       PositionSide
	EntryPrice float64
	ExitPrice  float64
	SizeUSDT   float64
	PnL        float64
	Duration   float64 // seconds
	EntryTime  int64
	ExitTime   int64
}
