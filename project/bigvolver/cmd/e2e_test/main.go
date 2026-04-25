package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"dsl-strategy-evolver/internal/data"
	"dsl-strategy-evolver/internal/ml"
)

func main() {
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = filepath.Join("C:\\Users\\Test\\.openclaw\\workspace\\crypto-trading-app\\data", "trading.db")
	}

	outputDir := os.Getenv("OUTPUT_DIR")
	if outputDir == "" {
		outputDir = filepath.Join("C:\\Users\\Test\\.openclaw\\workspace\\bigvolver-e2e", "data")
	}

	log.Printf("[E2E] DB: %s", dbPath)
	log.Printf("[E2E] Output: %s", outputDir)
	os.MkdirAll(outputDir, 0755)

	// 1. Connect to DB
	database, err := data.NewDatabase(dbPath)
	if err != nil {
		log.Fatalf("[E2E] Failed to connect DB: %v", err)
	}
	defer database.Close()

	repo := data.NewMarketDataRepository(database)

	// 2. Check available data
	symbols, err := repo.GetAvailableSymbols()
	if err != nil {
		log.Fatalf("[E2E] Failed to get symbols: %v", err)
	}
	log.Printf("[E2E] Available symbols: %v", symbols)

	for _, symbol := range symbols {
		count, _ := repo.GetCandleCount(symbol)
		log.Printf("[E2E] %s: %d candles", symbol, count)
	}

	// 3. Build feature pipeline
	config := ml.DefaultFeatureConfig()
	pipeline := ml.NewFeaturePipeline(config, repo)

	// 4. Generate features for each symbol
	totalFeatures := 0
	for _, symbol := range symbols {
		log.Printf("[E2E] Computing features for %s...", symbol)

		features, err := pipeline.ComputeFeaturesForSymbol(symbol)
		if err != nil {
			log.Printf("[E2E] ERROR %s: %v", symbol, err)
			continue
		}

		log.Printf("[E2E] %s: %d feature sets generated", symbol, len(features))
		totalFeatures += len(features)

		// 5. Export to JSONL
		outputPath := filepath.Join(outputDir, fmt.Sprintf("training_data_%s.jsonl", symbol))
		err = ml.ExportTrainingData(features, outputPath)
		if err != nil {
			log.Printf("[E2E] ERROR exporting %s: %v", symbol, err)
			continue
		}
		log.Printf("[E2E] Exported to %s", outputPath)

		// 6. Quick stats
		if len(features) > 0 {
			latest := features[len(features)-1]
			log.Printf("[E2E] Latest features for %s (timestamp %d):", symbol, latest.Timestamp)
			for _, name := range []string{"ema_20", "rsi_14", "macd_histogram", "atr_14", "volatility_24h", "momentum_4h", "regime_trending"} {
				if val, ok := latest.Features[name]; ok {
					log.Printf("[E2E]   %s = %.4f", name, val)
				}
			}
			log.Printf("[E2E]   target = %.4f%%", latest.Target)
		}
	}

	// 7. Summary
	log.Printf("[E2E] === SUMMARY ===")
	log.Printf("[E2E] Total feature sets: %d", totalFeatures)
	log.Printf("[E2E] Symbols processed: %d/%d", len(symbols), len(symbols))
	log.Printf("[E2E] Output dir: %s", outputDir)
	log.Printf("[E2E] Next: Start Python ML service and run retrain")

	// Save metadata
	meta := map[string]interface{}{
		"timestamp":    time.Now().Format(time.RFC3339),
		"db_path":      dbPath,
		"symbols":      symbols,
		"total_features": totalFeatures,
	}
	metaPath := filepath.Join(outputDir, "e2e_metadata.json")
	metaJSON, _ := json.MarshalIndent(meta, "", "  ")
	os.WriteFile(metaPath, metaJSON, 0644)
	log.Printf("[E2E] Metadata saved to %s", metaPath)
}
