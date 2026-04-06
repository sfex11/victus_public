package evolver

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Playbook stores successful strategy patterns for reuse
// Inspired by Quant-Autoresearch's playbook system
type Playbook struct {
	db   *gorm.DB
	path string
	mu   sync.RWMutex
}

// PatternDB is the GORM model for patterns
type PatternDB struct {
	ID             string         `gorm:"primaryKey"`
	Name           string         `gorm:"not null"`
	Hypothesis     string         `gorm:"type:text"`
	StrategyYAML   string         `gorm:"type:text"`
	EntryCondition string         `gorm:"type:text"`
	ExitCondition  string         `gorm:"type:text"`
	StopLoss       float64
	TakeProfit     float64
	Metrics        string         `gorm:"type:text"` // JSON
	NunchiScore    float64
	SharpeRatio    float64
	WinRate        float64
	MaxDrawdown    float64
	TotalTrades    int
	MarketRegime   string
	Symbol         string
	Tags           string         `gorm:"type:text"` // JSON
	CreatedAt      time.Time
	LastUsed       time.Time
	UseCount       int            `gorm:"default:0"`
	Metadata       string         `gorm:"type:text"` // JSON
}

func (PatternDB) TableName() string {
	return "patterns"
}

// Pattern represents a successful strategy pattern
type Pattern struct {
	ID              string                 `json:"id"`
	Name            string                 `json:"name"`
	Hypothesis      string                 `json:"hypothesis"`
	StrategyYAML    string                 `json:"strategy_yaml"`
	EntryCondition  string                 `json:"entry_condition"`
	ExitCondition   string                 `json:"exit_condition"`
	StopLoss        float64                `json:"stop_loss"`
	TakeProfit      float64                `json:"take_profit"`
	Metrics         map[string]float64     `json:"metrics"`
	NunchiScore     float64                `json:"nunchi_score"`
	SharpeRatio     float64                `json:"sharpe_ratio"`
	WinRate         float64                `json:"win_rate"`
	MaxDrawdown     float64                `json:"max_drawdown"`
	TotalTrades     int                    `json:"total_trades"`
	MarketRegime    string                 `json:"market_regime"` // trending, ranging, volatile
	Symbol          string                 `json:"symbol"`
	Tags            []string               `json:"tags"`
	CreatedAt       time.Time              `json:"created_at"`
	LastUsed        time.Time              `json:"last_used"`
	UseCount        int                    `json:"use_count"`
	Metadata        map[string]interface{} `json:"metadata"`
}

// NewPlaybook creates a new playbook
func NewPlaybook(dbPath string) (*Playbook, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create playbook directory: %w", err)
	}

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open playbook database: %w", err)
	}

	if err := db.AutoMigrate(&PatternDB{}); err != nil {
		return nil, fmt.Errorf("failed to init schema: %w", err)
	}

	p := &Playbook{
		db:   db,
		path: dbPath,
	}

	return p, nil
}

// StorePattern stores a new pattern
func (p *Playbook) StorePattern(pattern Pattern) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	metricsJSON, _ := json.Marshal(pattern.Metrics)
	tagsJSON, _ := json.Marshal(pattern.Tags)
	metadataJSON, _ := json.Marshal(pattern.Metadata)

	record := PatternDB{
		ID:             pattern.ID,
		Name:           pattern.Name,
		Hypothesis:     pattern.Hypothesis,
		StrategyYAML:   pattern.StrategyYAML,
		EntryCondition: pattern.EntryCondition,
		ExitCondition:  pattern.ExitCondition,
		StopLoss:       pattern.StopLoss,
		TakeProfit:     pattern.TakeProfit,
		Metrics:        string(metricsJSON),
		NunchiScore:    pattern.NunchiScore,
		SharpeRatio:    pattern.SharpeRatio,
		WinRate:        pattern.WinRate,
		MaxDrawdown:    pattern.MaxDrawdown,
		TotalTrades:    pattern.TotalTrades,
		MarketRegime:   pattern.MarketRegime,
		Symbol:         pattern.Symbol,
		Tags:           string(tagsJSON),
		CreatedAt:      pattern.CreatedAt,
		LastUsed:       pattern.LastUsed,
		UseCount:       pattern.UseCount,
		Metadata:       string(metadataJSON),
	}

	return p.db.Save(&record).Error
}

// patternDBToPattern converts a DB record to Pattern
func patternDBToPattern(r PatternDB) Pattern {
	p := Pattern{
		ID:             r.ID,
		Name:           r.Name,
		Hypothesis:     r.Hypothesis,
		StrategyYAML:   r.StrategyYAML,
		EntryCondition: r.EntryCondition,
		ExitCondition:  r.ExitCondition,
		StopLoss:       r.StopLoss,
		TakeProfit:     r.TakeProfit,
		NunchiScore:    r.NunchiScore,
		SharpeRatio:    r.SharpeRatio,
		WinRate:        r.WinRate,
		MaxDrawdown:    r.MaxDrawdown,
		TotalTrades:    r.TotalTrades,
		MarketRegime:   r.MarketRegime,
		Symbol:         r.Symbol,
		CreatedAt:      r.CreatedAt,
		LastUsed:       r.LastUsed,
		UseCount:       r.UseCount,
		Metrics:        make(map[string]float64),
		Tags:           []string{},
		Metadata:       make(map[string]interface{}),
	}
	json.Unmarshal([]byte(r.Metrics), &p.Metrics)
	json.Unmarshal([]byte(r.Tags), &p.Tags)
	json.Unmarshal([]byte(r.Metadata), &p.Metadata)
	return p
}

// GetTopPatterns returns top patterns by score
func (p *Playbook) GetTopPatterns(limit int) ([]Pattern, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var records []PatternDB
	err := p.db.Order("nunchi_score DESC").Limit(limit).Find(&records).Error
	if err != nil {
		return nil, err
	}

	patterns := make([]Pattern, 0, len(records))
	for _, r := range records {
		patterns = append(patterns, patternDBToPattern(r))
	}
	return patterns, nil
}

// GetPatternsByRegime returns patterns for a specific market regime
func (p *Playbook) GetPatternsByRegime(regime string, limit int) ([]Pattern, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var records []PatternDB
	err := p.db.Where("market_regime = ?", regime).Order("nunchi_score DESC").Limit(limit).Find(&records).Error
	if err != nil {
		return nil, err
	}

	patterns := make([]Pattern, 0, len(records))
	for _, r := range records {
		patterns = append(patterns, patternDBToPattern(r))
	}
	return patterns, nil
}

// GetPatternsBySymbol returns patterns for a specific symbol
func (p *Playbook) GetPatternsBySymbol(symbol string, limit int) ([]Pattern, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var records []PatternDB
	err := p.db.Where("symbol = ?", symbol).Order("nunchi_score DESC").Limit(limit).Find(&records).Error
	if err != nil {
		return nil, err
	}

	patterns := make([]Pattern, 0, len(records))
	for _, r := range records {
		patterns = append(patterns, patternDBToPattern(r))
	}
	return patterns, nil
}

// SearchPatterns searches patterns by hypothesis or entry/exit conditions
func (p *Playbook) SearchPatterns(query string, limit int) ([]Pattern, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	searchTerm := "%" + query + "%"
	var records []PatternDB
	err := p.db.Where("hypothesis LIKE ? OR entry_condition LIKE ? OR exit_condition LIKE ?",
		searchTerm, searchTerm, searchTerm).Order("nunchi_score DESC").Limit(limit).Find(&records).Error
	if err != nil {
		return nil, err
	}

	patterns := make([]Pattern, 0, len(records))
	for _, r := range records {
		patterns = append(patterns, patternDBToPattern(r))
	}
	return patterns, nil
}

// MarkUsed marks a pattern as used
func (p *Playbook) MarkUsed(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.db.Model(&PatternDB{}).Where("id = ?", id).
		Updates(map[string]interface{}{
			"last_used": time.Now(),
			"use_count": gorm.Expr("use_count + 1"),
		}).Error
}

// DeletePattern deletes a pattern
func (p *Playbook) DeletePattern(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.db.Where("id = ?", id).Delete(&PatternDB{}).Error
}

// GetStats returns playbook statistics
func (p *Playbook) GetStats() (map[string]interface{}, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := make(map[string]interface{})

	var total int64
	p.db.Model(&PatternDB{}).Count(&total)
	stats["total_patterns"] = total

	var avgNunchi, avgSharpe float64
	p.db.Model(&PatternDB{}).Select("COALESCE(AVG(nunchi_score), 0)").Scan(&avgNunchi)
	p.db.Model(&PatternDB{}).Select("COALESCE(AVG(sharpe_ratio), 0)").Scan(&avgSharpe)
	stats["avg_nunchi_score"] = avgNunchi
	stats["avg_sharpe_ratio"] = avgSharpe

	var best PatternDB
	result := p.db.Order("nunchi_score DESC").First(&best)
	if result.Error == nil {
		stats["best_pattern_name"] = best.Name
		stats["best_nunchi_score"] = best.NunchiScore
	}

	// Regime distribution
	type RegimeCount struct {
		MarketRegime string
		Count        int64
	}
	var regimes []RegimeCount
	p.db.Model(&PatternDB{}).Select("market_regime, COUNT(*) as count").Group("market_regime").Scan(&regimes)
	regimeMap := make(map[string]int)
	for _, r := range regimes {
		regimeMap[r.MarketRegime] = int(r.Count)
	}
	stats["regime_distribution"] = regimeMap

	return stats, nil
}

// GetObservations is kept for compatibility
func (p *Playbook) GetObservations() []interface{} {
	return nil
}

// Close closes the database connection
func (p *Playbook) Close() error {
	sqlDB, err := p.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
