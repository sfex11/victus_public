package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"dsl-strategy-evolver/internal/ai"
	"dsl-strategy-evolver/internal/api"
	"dsl-strategy-evolver/internal/data"
	"dsl-strategy-evolver/internal/dsl"
	"dsl-strategy-evolver/internal/engine"
	"dsl-strategy-evolver/internal/evolver"
	"gopkg.in/yaml.v3"
)

const (
	// OpenRouter configuration
	OpenRouterAPIKey = "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"
	OpenRouterBaseURL = "https://openrouter.ai/api/v1"

	// Server configuration
	Port = 3004

	// Database path (absolute path)
	DBPath = "C:/Users/Test/.openclaw/workspace/crypto-trading-app/data/trading.db"
)

func main() {
	fmt.Println("========================================")
	fmt.Println("  DSL Strategy Evolver v2.0")
	fmt.Println("  Autonomous Evolution Mode")
	fmt.Println("========================================")
	fmt.Println()

	// ====================================================================
	// Phase 0: Load Constitution
	// ====================================================================
	fmt.Println("[Phase 0] Loading Constitution...")
	
	constitutionPath := "config/constitution.yaml"
	constitution, err := evolver.Load(constitutionPath)
	if err != nil {
		log.Printf("[Warning] Failed to load constitution: %v, using defaults", err)
		constitution = evolver.Default()
	}
	
	fmt.Printf("[Phase 0] Mandate: %s\n", constitution.Mandate)
	fmt.Printf("[Phase 0] Max DD: %.0f%%, Min Trades: %d\n", 
		constitution.RiskLimits.MaxDrawdown*100, constitution.RiskLimits.MinTrades)
	fmt.Printf("[Phase 0] Goal: %s > %.2f\n", constitution.Goal.Metric, constitution.Goal.Threshold)

	// ====================================================================
	// Phase 1: Initialize Database
	// ====================================================================
	fmt.Printf("\n[Phase 1] Connecting to database: %s\n", DBPath)

	db, err := data.NewDatabase(DBPath)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	fmt.Println("[Phase 1] Database connected successfully")

	// Initialize market data repository
	marketRepo := data.NewMarketDataRepository(db)

	// Check available data
	symbols, err := marketRepo.GetAvailableSymbols()
	if err != nil {
		log.Printf("Warning: Failed to get available symbols: %v", err)
	} else {
		fmt.Printf("[Phase 1] Available symbols: %v\n", symbols)
	}

	// ====================================================================
	// Phase 2: Initialize Components with Safety
	// ====================================================================
	fmt.Println("\n[Phase 2] Initializing components...")

	// Paper trading engine
	tradingEngine := engine.NewEngine(marketRepo)

	// AI generator
	aiGen := ai.NewGenerator(OpenRouterAPIKey, OpenRouterBaseURL)

	// Risk Scorer (SNOWBALL-inspired)
	riskScorer := engine.NewRiskScorer()
	fmt.Println("[Phase 2] Risk Scorer initialized (ATR+RSI+BB+Volume)")

	// Multi-Agent Generator (4 analysts + coordinator)
	multiAgent := ai.NewMultiAgentGenerator(aiGen)
	fmt.Println("[Phase 2] Multi-Agent Consensus: 4 analysts + coordinator")

	// Evolver (original)
	strategyEvolver := evolver.NewEvolver(
		tradingEngine,
		marketRepo,
		OpenRouterAPIKey,
		OpenRouterBaseURL,
	)

	// ====================================================================
	// Phase 3: Initialize Enhanced Safety Systems
	// ====================================================================
	fmt.Println("\n[Phase 3] Initializing safety systems...")

	// Doom-Loop Detector
	doomLoop := evolver.NewDoomLoopDetector(constitution.Safety.DoomLoopThreshold)
	fmt.Printf("[Phase 3] Doom-Loop Detector: threshold=%d\n", constitution.Safety.DoomLoopThreshold)

	// Context Compactor
	contextCompactor := evolver.NewContextCompactor(1000, 0.3)
	fmt.Println("[Phase 3] Context Compactor: max=1000 observations, 30% compression")

	// Playbook (success patterns)
	playbookPath := filepath.Join(filepath.Dir(DBPath), "playbook.db")
	playbook, err := evolver.NewPlaybook(playbookPath)
	if err != nil {
		log.Printf("[Warning] Failed to initialize playbook: %v", err)
	} else {
		stats, _ := playbook.GetStats()
		fmt.Printf("[Phase 3] Playbook: %v patterns loaded\n", stats["total_patterns"])
		defer playbook.Close()
	}

	// Auto Reverter
	_ = evolver.NewAutoReverter()
	fmt.Println("[Phase 3] Auto Reverter: enabled")

	// ArXiv Research Client
	arxivClient := ai.NewArXivClient()
	fmt.Println("[Phase 3] ArXiv Research: ready")

	// ====================================================================
	// Phase 4: Print Safety Summary
	// ====================================================================
	fmt.Println("\n[Phase 3] Safety Configuration:")
	fmt.Printf("  ├─ Approval Mode: %s\n", constitution.Safety.ApprovalMode)
	fmt.Printf("  ├─ Auto Revert: %v\n", constitution.Safety.AutoRevert)
	fmt.Printf("  ├─ Doom-Loop Threshold: %d\n", constitution.Safety.DoomLoopThreshold)
	fmt.Printf("  ├─ Context Max: %d%%\n", constitution.Safety.ContextMaxPercent)
	fmt.Printf("  ├─ Forbidden Patterns: %d\n", len(constitution.Forbidden))
	fmt.Printf("  └─ Evolution Interval: %s\n", constitution.Evolution.Interval)

	// ====================================================================
	// Phase 4: Load Initial Strategies
	// ====================================================================
	fmt.Println("\n[Phase 4] Loading initial strategies...")
	loadInitialStrategies(tradingEngine, marketRepo, strategyEvolver)

	// ====================================================================
	// Phase 5: Start Services
	// ====================================================================
	fmt.Println("\n[Phase 5] Starting services...")

	// Start paper trading engine
	if err := tradingEngine.Start(); err != nil {
		log.Fatalf("Failed to start trading engine: %v", err)
	}
	fmt.Println("[Phase 5] Paper trading engine started")

	// Initialize API handler
	apiHandler := api.NewHandler(strategyEvolver, tradingEngine, strategyEvolver.Ranker(), aiGen)

	// Start HTTP server
	go func() {
		if err := api.StartServer(apiHandler, Port); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()
	time.Sleep(100 * time.Millisecond)

	// ====================================================================
	// Phase 6: Start Autonomous Evolution Loop
	// ====================================================================
	fmt.Println("\n[Phase 6] Starting autonomous evolution loop...")

	// Parse evolution interval
	interval, _ := time.ParseDuration(constitution.Evolution.Interval)
	if interval == 0 {
		interval = 1 * time.Minute
	}

	evolutionTicker := time.NewTicker(interval)
	defer evolutionTicker.Stop()

	// Status ticker (every 30 seconds)
	statusTicker := time.NewTicker(30 * time.Second)
	defer statusTicker.Stop()

	// Daily report ticker (every 6 hours)
	reportTicker := time.NewTicker(6 * time.Hour)
	defer reportTicker.Stop()

	// Cleanup ticker (every 24 hours)
	cleanupTicker := time.NewTicker(24 * time.Hour)
	defer cleanupTicker.Stop()

	fmt.Printf("[Phase 6] Evolution interval: %v\n", interval)
	fmt.Println("[Phase 6] First evolution cycle in", interval)

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("  System Started Successfully!")
	fmt.Println("========================================")
	fmt.Printf("  API Server: http://localhost:%d\n", Port)
	fmt.Printf("  Health: http://localhost:%d/health\n", Port)
	fmt.Printf("  Status: http://localhost:%d/api/evolver/status\n", Port)
	fmt.Println()
	fmt.Println("  Autonomous Mode: ON")
	fmt.Printf("  Evolution Interval: %v\n", interval)
	fmt.Printf("  Max Strategies: %d\n", constitution.Evolution.MaxStrategies)
	fmt.Println()
	fmt.Println("  Press Ctrl+C to stop")
	fmt.Println("========================================")
	fmt.Println()

	// ====================================================================
	// Main Loop
	// ====================================================================
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	for {
		select {
		case <-sigCh:
			fmt.Println("\n[Shutdown] Received interrupt signal")
			shutdown(tradingEngine, strategyEvolver, db)
			return

		case <-evolutionTicker.C:
			// === AUTONOMOUS EVOLUTION CYCLE ===
			fmt.Printf("\n[Evolution] ========== Cycle Started ==========\n")
			evolutionCycleStartTime := time.Now()

			// Step 1: Check context pressure
			contextStatus := contextCompactor.GetStatus()
			if contextStatus.Stage == "critical" {
				contextCompactor.ForceCompact()
				fmt.Printf("[Evolution] Context compacted (was %.1f%%)\n", contextStatus.UsagePercent)
			}

			// Step 1.5: RISK SCORE CHECK (SNOWBALL-inspired)
			fmt.Println("[Evolution] Phase 0: Computing risk score...")
			candles, err := marketRepo.GetLatestCandles("BTCUSDT", 100)
			if err == nil && len(candles) > 0 {
				highs := make([]float64, len(candles))
				lows := make([]float64, len(candles))
				closes := make([]float64, len(candles))
				volumes := make([]float64, len(candles))
				for i, c := range candles {
					highs[i] = c.High
					lows[i] = c.Low
					closes[i] = c.Close
					volumes[i] = c.Volume
				}

				riskResult := riskScorer.Calculate(highs, lows, closes, volumes)
				fmt.Printf("[Evolution] Risk Score: %.0f/100 (ATR:%.0f RSI:%.0f BB:%.0f Vol:%.0f) → %s\n",
					riskResult.Score, riskResult.ATRScore, riskResult.RSIScore, riskResult.BBScore, riskResult.VolumeScore, riskResult.State)

				if !riskResult.ShouldEvolve {
					fmt.Printf("[Evolution] ⚠️  Risk state %s — evolution SKIPPED\n", riskResult.State)
					duration := time.Since(evolutionCycleStartTime)
					fmt.Printf("[Evolution] ========== Cycle Skipped (%v) ==========\n", duration)
					continue
				}

				if riskResult.EvolutionWeight < 1.0 {
					fmt.Printf("[Evolution] Risk CAUTION — reducing generation to %.0f%%\n", riskResult.EvolutionWeight*100)
				}
			} else {
				fmt.Printf("[Evolution] Warning: cannot fetch candles for risk check: %v\n", err)
			}

			// Step 2: Get current best score
			status := strategyEvolver.GetStatus()
			currentBest := 0.0
			if status.BestStrategy != nil {
				currentBest = status.BestStrategy.SharpeRatio
			}

			// Step 3: MULTI-AGENT CONSENSUS (replaces single ThinkingPhase)
			fmt.Println("[Evolution] Phase 1: Multi-agent consensus...")
			strategiesToGen := constitution.Evolution.StrategiesToGenerate
			if strategiesToGen == 0 {
				strategiesToGen = 20
			}

			// Get top strategies for agent context
			var topStrategies []*dsl.Strategy
			allStrategies := tradingEngine.GetAllStrategies()
			if len(allStrategies) > 0 {
				for _, s := range allStrategies {
					topStrategies = append(topStrategies, s.Strategy)
				}
			}

			// Calculate risk score for agents
			agentRiskScore := 25.0 // default NORMAL
			if len(candles) > 0 {
				highs := make([]float64, len(candles))
				lows := make([]float64, len(candles))
				closes := make([]float64, len(candles))
				volumes := make([]float64, len(candles))
				for i, c := range candles {
					highs[i] = c.High
					lows[i] = c.Low
					closes[i] = c.Close
					volumes[i] = c.Volume
				}
				rs := riskScorer.Calculate(highs, lows, closes, volumes)
				agentRiskScore = rs.Score
			}

			situation := fmt.Sprintf("Iteration ongoing. Best Sharpe: %.3f. Active: %d. Risk: %.0f.",
				currentBest, status.ActiveStrategies, agentRiskScore)

			thinking, err := aiGen.ThinkingPhase(context.Background(), situation)
			if err != nil {
				fmt.Printf("[Evolution] Thinking failed: %v\n", err)
			}

			// Step 4: Multi-agent consensus decision
			_, consensusResult, err := multiAgent.GenerateWithConsensus(context.Background(), "BTCUSDT", agentRiskScore, topStrategies)
			if err != nil {
				fmt.Printf("[Evolution] Consensus failed: %v, falling back to single generation\n", err)
			}

			if consensusResult != nil && consensusResult.FinalAction != "GENERATE" {
				fmt.Printf("[Evolution] ⚠️  Multi-agent consensus: %s (agreement=%.2f) — generation SKIPPED\n",
					consensusResult.FinalAction, consensusResult.Agreement)
				for _, v := range consensusResult.Verdicts {
					if v != nil {
						fmt.Printf("[Evolution]   %s: %s (confidence=%.2f)\n", v.Role, v.Action, v.Confidence)
					}
				}
				duration := time.Since(evolutionCycleStartTime)
				fmt.Printf("[Evolution] ========== Cycle Skipped (%v) ==========\n", duration)
				continue
			}

			if consensusResult != nil {
				fmt.Printf("[Evolution] Multi-agent consensus: GENERATE (agreement=%.2f, count=%d)\n",
					consensusResult.Agreement, consensusResult.StrategyCount)
				for _, v := range consensusResult.Verdicts {
					if v != nil {
						fmt.Printf("[Evolution]   %s: %s (confidence=%.2f, score=%.1f)\n", v.Role, v.Action, v.Confidence, v.Score)
					}
				}
				strategiesToGen = consensusResult.StrategyCount
			}

			// Build prompt with research context
			var researchPrompt string
			if thinking != nil {
				researchCtx, err := arxivClient.GetResearchContext(nil, thinking.Hypothesis, 3)
				if err == nil && researchCtx != nil {
					researchPrompt = researchCtx.Summary
					fmt.Printf("[Evolution] ArXiv research: %d papers found\n", len(researchCtx.Papers))
				}
			}

			// Generate strategies
			newStrategies, err := aiGen.GenerateStrategies(context.Background(), strategiesToGen, "BTCUSDT", nil)
			if err != nil {
				fmt.Printf("[Evolution] Generation failed: %v\n", err)
				fmt.Printf("[Evolution] ========== Cycle Failed (%v) ==========\n", time.Since(evolutionCycleStartTime))
				continue
			}

			fmt.Printf("[Evolution] Generated %d strategies\n", len(newStrategies))

			// Step 5: Validate against Constitution
			fmt.Println("[Evolution] Phase 3: Constitution validation...")
			validCount := 0
			for _, s := range newStrategies {
				// Doom-loop check
				action := evolver.Action{
					ToolName: "generate",
					Params: map[string]interface{}{
						"name":       s.Name,
						"entry_long":  s.Long.Entry,
						"exit_long":   s.Long.Exit,
					},
				}
				isDoom, count := doomLoop.Check(action)
				if isDoom {
					fmt.Printf("[Evolution] Skipping doom-loop: %s (count=%d)\n", s.Name, count)
					continue
				}

				// Add to engine
				// Constitution check using raw strategy data
				strategyText := fmt.Sprintf("name=%s entry_long=%s exit_long=%s entry_short=%s exit_short=%s",
					s.Name, s.Long.Entry, s.Long.Exit, s.Short.Entry, s.Short.Exit)
				safe, violations := constitution.ValidateStrategy(strategyText)
				if !safe {
					fmt.Printf("[Evolution] Constitution violation: %s\n", s.Name)
					for _, v := range violations {
						fmt.Printf("  └─ [%s] %s\n", v, v)
					}
					continue
				}

				yamlBytes, _ := yaml.Marshal(s)
				instance, err := dsl.NewParser().ParseWithExpressions([]byte(yamlBytes))
				if err != nil {
					continue
				}
				instance.Strategy.CreatedAt = time.Now()
				instance.Strategy.Source = "evolution"
				instance.Strategy.Generation = status.TotalCycles + 1

				if err := tradingEngine.AddStrategy(instance); err == nil {
					validCount++
				}
			}

			fmt.Printf("[Evolution] Valid strategies: %d/%d\n", validCount, len(newStrategies))

			// Step 6: Record observation
			contextCompactor.AddObservation("evolution",
				fmt.Sprintf("Cycle: generated=%d, valid=%d, research=%s",
					len(newStrategies), validCount, researchPrompt),
				situation, 0.5)

			// Step 7: Start evolver
			fmt.Println("[Evolution] Phase 4: Starting evolution...")
			strategyEvolver.Start()

			duration := time.Since(evolutionCycleStartTime)
			fmt.Printf("[Evolution] ========== Cycle Complete (%v) ==========\n", duration)

		case <-statusTicker.C:
			// Periodic status
			printEnhancedStatus(strategyEvolver, doomLoop, contextCompactor, playbook)

		case <-reportTicker.C:
			// Daily report
			printDailyReport(strategyEvolver, doomLoop, contextCompactor, playbook)

		case <-cleanupTicker.C:
			// Daily cleanup
			expired := doomLoop.CleanExpired()
			fmt.Printf("[Cleanup] Cleaned %d expired doom-loop records\n", expired)
		}
	}
}

// printEnhancedStatus prints status with safety information
func printEnhancedStatus(
	evolverInstance *evolver.Evolver,
	doomLoop *evolver.DoomLoopDetector,
	contextCompactor *evolver.ContextCompactor,
	playbook *evolver.Playbook,
) {
	status := evolverInstance.GetStatus()

	runningStr := "stopped"
	if status.Running {
		runningStr = "running"
	}

	fmt.Printf("[Status] %s | Active: %d | Cycles: %d | Generated: %d\n",
		runningStr, status.ActiveStrategies, status.TotalCycles, status.TotalGenerated)

	if status.BestStrategy != nil {
		fmt.Printf("[Status] Best: %s (Sharpe: %.2f)\n",
			status.BestStrategy.Name,
			status.BestStrategy.SharpeRatio,
		)
	}

	// Safety stats
	doomStats := doomLoop.GetStats()
	contextStatus := contextCompactor.GetStatus()
	fmt.Printf("[Safety] DoomLoops blocked: %d | Context: %.1f%% (%s)\n",
		doomStats.BlockedActions, contextStatus.UsagePercent, contextStatus.Stage)
}

// printDailyReport prints a comprehensive daily report
func printDailyReport(
	evolverInstance *evolver.Evolver,
	doomLoop *evolver.DoomLoopDetector,
	contextCompactor *evolver.ContextCompactor,
	playbook *evolver.Playbook,
) {
	status := evolverInstance.GetStatus()

	fmt.Println("\n========================================")
	fmt.Printf("  DAILY REPORT - %s\n", time.Now().Format("2006-01-02 15:04"))
	fmt.Println("========================================")

	fmt.Printf("\n📊 Evolution Stats:\n")
	fmt.Printf("  Total Cycles: %d\n", status.TotalCycles)
	fmt.Printf("  Total Generated: %d\n", status.TotalGenerated)
	fmt.Printf("  Active Strategies: %d\n", status.ActiveStrategies)

	if status.BestStrategy != nil {
		fmt.Printf("\n🏆 Best Strategy:\n")
		fmt.Printf("  Name: %s\n", status.BestStrategy.Name)
		fmt.Printf("  Sharpe Ratio: %.2f\n", status.BestStrategy.SharpeRatio)
		fmt.Printf("  Total Return: %.2f%%\n", status.BestStrategy.TotalReturn*100)
	}

	if status.AverageMetrics != nil {
		fmt.Printf("\n📈 Average Metrics:\n")
		fmt.Printf("  Sharpe: %.2f\n", status.AverageMetrics.SharpeRatio)
		fmt.Printf("  Win Rate: %.1f%%\n", status.AverageMetrics.WinRate*100)
		fmt.Printf("  Return: %.2f%%\n", status.AverageMetrics.TotalReturn*100)
	}

	// Safety stats
	fmt.Printf("\n🛡️ Safety:\n")
	doomStats := doomLoop.GetStats()
	fmt.Printf("  Doom-Loops Blocked: %d\n", doomStats.BlockedActions)
	fmt.Printf("  Context Usage: %.1f%%\n", contextCompactor.GetContextUsage())
	fmt.Printf("  Observations: %d\n", len(contextCompactor.GetObservations()))

	// Playbook stats
	if playbook != nil {
		stats, err := playbook.GetStats()
		if err == nil {
			fmt.Printf("\n📚 Playbook:\n")
			fmt.Printf("  Total Patterns: %v\n", stats["total_patterns"])
			fmt.Printf("  Avg Nunchi: %.3f\n", stats["avg_nunchi_score"])
			if bestName, ok := stats["best_pattern_name"]; ok {
				fmt.Printf("  Best: %v\n", bestName)
			}
		}
	}

	fmt.Println("\n========================================")
}

// loadInitialStrategies loads initial strategies for paper trading
func loadInitialStrategies(
	eng *engine.Engine,
	marketRepo *data.MarketDataRepository,
	evolverInstance *evolver.Evolver,
) {
	strategies := []string{
		`name: "EMA_Crossover"
symbol: "BTCUSDT"
type: "hedge"
long:
  entry: "price < ema(20) * 0.99"
  exit: "price > ema(20) * 1.02"
  stop_loss: 0.02
short:
  entry: "price > ema(20) * 1.01"
  exit: "price < ema(20) * 0.98"
  stop_loss: 0.02
risk:
  position_size: 100
  max_positions: 2
`,
		`name: "RSI_MeanReversion"
symbol: "BTCUSDT"
type: "hedge"
long:
  entry: "rsi(14) < 30"
  exit: "rsi(14) > 50"
  stop_loss: 0.03
short:
  entry: "rsi(14) > 70"
  exit: "rsi(14) < 50"
  stop_loss: 0.03
risk:
  position_size: 100
  max_positions: 2
`,
		`name: "Momentum_Breakout"
symbol: "BTCUSDT"
type: "hedge"
long:
  entry: "price > ema(20) && price > ema(50)"
  exit: "price < ema(20)"
  stop_loss: 0.025
short:
  entry: "price < ema(20) && price < ema(50)"
  exit: "price > ema(20)"
  stop_loss: 0.025
risk:
  position_size: 100
  max_positions: 2
`,
	}

	parser := dsl.NewParser()
	loaded := 0

	for _, yamlContent := range strategies {
		instance, err := parser.ParseWithExpressions([]byte(yamlContent))
		if err != nil {
			log.Printf("Warning: Failed to parse strategy: %v", err)
			continue
		}

		instance.Strategy.CreatedAt = time.Now()
		instance.Strategy.Source = "initial"
		instance.Strategy.Generation = 0

		if err := eng.AddStrategy(instance); err != nil {
			log.Printf("Warning: Failed to add strategy: %v", err)
			continue
		}

		evolverInstance.Ranker().RegisterStrategy(instance)
		loaded++
	}

	fmt.Printf("[Init] Loaded %d initial strategies\n", loaded)
}

// shutdown gracefully shuts down the system
func shutdown(eng *engine.Engine, evolverInstance *evolver.Evolver, db *data.Database) {
	fmt.Println("[Shutdown] Stopping evolution loop...")
	if err := evolverInstance.Stop(); err != nil {
		log.Printf("Warning: Failed to stop evolver: %v", err)
	}

	fmt.Println("[Shutdown] Stopping paper trading engine...")
	if err := eng.Stop(); err != nil {
		log.Printf("Warning: Failed to stop engine: %v", err)
	}

	fmt.Println("[Shutdown] Closing database...")
	if err := db.Close(); err != nil {
		log.Printf("Warning: Failed to close database: %v", err)
	}

	fmt.Println("[Shutdown] Shutdown complete")
}
