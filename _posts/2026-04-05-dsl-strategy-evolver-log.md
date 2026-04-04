---
layout: post
title: "매매전략 자동 개발 시스템 — DSL Strategy Evolver 작업일지"
subtitle: "100개 DSL 전략을 AI가 자율 진화시키는 시스템을 구축한 기록"
date: 2026-04-05
series: "빅터스 연구소"
tags: [프로젝트, 암호화폐, Go, AI, 백테스팅]
---

![Strategy Evolution Loop](https://img.shields.io/badge/Phase-1~3_완료-brightgreen) ![Phase 4](https://img.shields.io/badge/Phase-4-통합_테스트-yellow) ![Go](https://img.shields.io/badge/Language-Go_1.21+-00ADD8)

## 이 프로젝트가 하는 일

DSL Strategy Evolver는 **100개의 암호화폐 매매 전략을 동시에 Paper Trading하며, AI가 지속적으로 새로운 전략을 생성·검증·교체하는 자율 진화 시스템**입니다.

단순히 "전략을 짜서 돌리는" 것이 아닙니다. 시스템 스스로 학습하고, 실패한 전략은 버리고, 더 나은 전략으로 교체하는 루프를 자동으로 돌립니다.

**핵심 아이디어 한 줄:** *자연선택처럼, 시장에서 살아남은 전략만 다음 세대로 전달된다.*

---

## 시스템 아키텍처

```
┌─────────────────────────────────────────────────┐
│              HTTP API Layer (Port 3004)           │
│  /start  /stop  /status  /generate  /leaderboard  │
└─────────────────────┬───────────────────────────┘
                      ▼
┌─────────────────────────────────────────────────┐
│             Evolver Orchestrator                 │
│  Evolution Loop │ Strategy Manager │ Rank Calc    │
└──────┬──────────────┬──────────────────┬────────┘
       ▼              ▼                  ▼
  AI Generator   Paper Trading      Backtester
  (OpenRouter)    Engine (실시간)    (과거 데이터)
       │              │                  │
       ▼              ▼                  ▼
  5 Free Models  Position Mgr       Data Provider
  Round-Robin    Long + Short       SQLite DB
```

> 📐 **설계 참고:** [Quant-Autoresearch](https://github.com/yllvar/Quant-Autoresearch) 프로젝트에서 아이디어를 얻어, 우리 환경(Go + SQLite + Binance)에 맞게 재설계했습니다.

---

## 진화 루프 — 어떻게 동작하는가

```
1. GENERATE  → AI가 N개 새 전략 생성
2. BACKTEST  → 과거 데이터로 1차 필터링
3. TRADE     → Paper Trading으로 실시간 검증
4. EVALUATE  → 성과 기반 순위 계산
5. SELECT    → 하위 20% 제거, 상위 유지
6. REPEAT    → 주기마다 반복
```

**핵심 포인트:** "진화"라는 단어가 의미 있으려면 *선택 압력*이 필요합니다. 우리 시스템은 Sharpe Ratio, Win Rate, Max Drawdown, Profit Factor를 종합 점수(Nunchi Score)로 계산해 하위 전략을 도태시킵니다.

---

## DSL — YAML로 전략을 정의하는 언어

전략은 YAML 파일로 작성합니다. 예시:

```yaml
name: "SimpleEMA"
symbol: "BTCUSDT"
type: "hedge"
long:
  entry: "price < ema(20)"
  exit: "price > ema(20)"
  stop_loss: 0.02
short:
  entry: "price > ema(20)"
  exit: "price < ema(20)"
  stop_loss: 0.02
risk:
  position_size: 100
  max_positions: 2
```

**지원하는 표현식:**
- 수학: `+`, `-`, `*`, `/`, `%`
- 비교: `<`, `>`, `<=`, `>=`, `==`, `!=`
- 논리: `&&`, `||`, `!`
- 내장 함수: `ema(period)`, `sma(period)`, `rsi(period)`, `max()`, `min()`
- 변수: `price`, `volume`, `funding_rate`

💡 **교훈:** DSL 설계에서 가장 중요한 것은 *"충분히 유연하면서도 충분히 제한적이어야 한다"*는 것입니다. 완전한 프로그래밍 언어를 만들면 유지보수가 불가능해지고, 너무 단순하면 의미 있는 전략을 표현할 수 없습니다.

---

## 기술 스택

| 구성요소 | 기술 |
|----------|------|
| 언어 | **Go 1.21+** |
| DB | SQLite (`data/trading.db` 공유) |
| API | Binance Futures, OpenRouter |
| 터널 | Rustunnel (자체 제작) |
| 배포 | Oracle Cloud Free Tier VPS |

**AI 모델 풀 (무료 로테이션):**
1. `stepfun/step-3.5-flash:free`
2. `nvidia/nemotron-3-super:free`
3. `arcee-ai/trinity-large-preview:free`
4. `z-ai/glm-4.5-air:free`
5. `qwen/qwen3-coder:free`

---

## 프로젝트 구조 (13개 모듈)

```
dsl-strategy-evolver/
├── cmd/server/           # 진입점
├── internal/
│   ├── ai/               # AI 생성 + Multi-Agent Consensus
│   │   ├── generator.go      # OpenRouter 다중 모델 전략 생성
│   │   ├── consensus.go      # 다중 에이전트 합의 시스템
│   │   └── research.go       # ArXiv 논문 기반 RAG
│   ├── dsl/              # DSL 파서
│   │   ├── parser.go         # YAML → Strategy 변환
│   │   ├── models.go         # 전략 타입 정의
│   │   └── indicators.go     # 기술적 지표 (EMA, RSI, SMA)
│   ├── engine/           # Paper Trading 엔진
│   │   ├── engine.go         # 100전략 동시 실행
│   │   ├── position.go       # 롱+숏 포지션 관리
│   │   ├── risk.go           # 리스크 관리
│   │   ├── regime.go         # 시장 국면 판별
│   │   └── validation.go     # 전략 유효성 검증
│   ├── evolver/          # 진화 오케스트레이터
│   │   ├── evolver.go        # 메인 진화 루프
│   │   ├── constitution.go   # 제약조건 시스템 (YAML)
│   │   ├── doom_loop.go      # 둠루프 감지기
│   │   ├── playbook.go       # 성공 패턴 학습 (GORM)
│   │   ├── auto_revert.go    # 자동 롤백 시스템
│   │   └── context.go        # 컨텍스트 압축
│   ├── backtest/         # 백테스터
│   │   └── backtest.go       # 과거 데이터 검증
│   ├── rank/             # 순위 계산
│   │   └── ranker.go         # Nunchi Score 산출
│   ├── data/             # 데이터 계층
│   │   ├── db.go              # SQLite + GORM
│   │   └── market.go          # 시장 데이터 조회
│   └── api/              # HTTP API
│       └── handler.go         # REST 엔드포인트
├── strategies/           # DSL 전략 YAML
└── config/               # 설정 (Constitution 등)
```

---

## Phase별 진행 기록

### Phase 1: 문서화 (완료 ✅)

PRD → DESIGN → TEAM-PRD까지 체계적으로 문서화했습니다.

> 📖 **핵심 교훈:** 코딩을 시작하기 전에 아키텍처를 충분히 설계하는 것이 장기적으로 더 빠릅니다. 우리는 3개의 설계 문서(PRD, DESIGN, TEAM-PRD)를 먼저 완성했고, 그 덕분에 3명 팀원 분업이 수월했습니다.

### Phase 2: Go Evolver 개선 — 13개 기능 구현 (완료 ✅)

| # | 기능 | 파일 | 설명 |
|---|------|------|------|
| 1 | **Constitution** | `constitution.yaml` | 불변의 리스크 규칙 (최대 DD 20% 등) |
| 2 | **Doom-Loop Detector** | `doom_loop.go` | 동일 전략 반복 생성 감지 |
| 3 | **Constitution 로더** | `constitution.go` | YAML 제약조건 런타임 로딩 |
| 4 | **Auto-Revert** | `auto_revert.go` | 성능 저하 시 자동 롤백 |
| 5 | **Context Compaction** | `context.go` | AI 프롬프트 길이 압축 |
| 6 | **Multi-Model 분리** | `generator.go` | 5개 모델 Round-Robin |
| 7 | **Playbook** | `playbook.go` | 성공 패턴 DB 저장·재사용 |
| 8 | **ArXiv RAG** | `research.go` | 논문 기반 전략 인사이트 |
| 9 | **DSL Parser** | `dsl/` | YAML → 실행 가능한 전략 |
| 10 | **Paper Trading Engine** | `engine/` | 100전략 실시간 실행 |
| 11 | **Backtester** | `backtest/` | 과거 데이터 검증 |
| 12 | **Rank Calculator** | `ranker.go` | Nunchi Score 산출 |
| 13 | **HTTP API** | `handler.go` | REST 엔드포인트 |

💡 **응용:** Constitution(헌법) 패턴은 AI 시스템에 "절대 건드리지 말아야 할 규칙"을 강제하는 방법입니다. 이 패턴은 자율주행, 의료 AI 등 안전이 중요한 모든 AI 시스템에 적용 가능합니다.

### Phase 3: Rust 백테스터 개선 (완료 ✅)

Rust 기반 백테스터에 5개 기능 추가:
- **Forced Signal Lag** — 실제 거래 지연 시뮬레이션 (미래 참조 방지)
- **Look-Ahead Bias Scanner** — 과적합 탐지
- **Monte Carlo Permutation Test** — 랜덤 대비 통계적 유의성
- **Nunchi Score p-value 가중** — 점수의 통계적 신뢰도 반영
- **Walk-Forward Validation** — 시간 순서 보존 교차검증

> 🎯 **핵심:** 백테스트에서 가장 위험한 것은 **Look-Ahead Bias(미래 참조 편향)**입니다. 과거 데이터로 "완벽한 전략"을 만드는 것은 쉽지만, 실전에서는 동작하지 않습니다. Walk-Forward Validation으로 이 문제를 완화합니다.

### Phase 4: 통합 테스트 (진행 중 ⏳)

#### 해결한 이슈 3건

**이슈 1: Playbook CGO → GORM 전환**
- 문제: `playbook.go`가 CGO 기반 `go-sqlite3` 사용 → Windows에서 빌드 실패
- 해결: `github.com/glebarez/sqlite` (순수 Go) + GORM ORM으로 전환
- `PatternDB` 모델 추가, `playbook.go` 전면 리팩토링

**이슈 2: 시장 데이터 테이블 누락**
- 문제: `market_1h_candles`, `market_funding_rate` 테이블이 DB에 없음
- 해결: `data/db.go`에 GORM 모델 추가 + `AutoMigrate`로 자동 생성

**이슈 3: 전략 중복 로드**
- 문제: `loadInitialStrategies`가 기존 DB 전략과 충돌
- 상태: 경고만 발생, 크리티컬하지 않음

#### 빌드 & 실행 결과
- `go build ./...` ✅ 에러 0
- `go build -o evolver.exe ./cmd/server/` ✅
- Phase 0~6 모두 정상 통과
- API 서버 `:3004` 정상 구동
- 초기 전략 2개 로드 (EMA_Crossover, RSI_MeanReversion)

---

## 인프라 — 외부 접근 구성

Evolver를 인터넷에서 모니터링할 수 있도록 자체 제작 터널인 [Rustunnel](https://github.com/nicobailon/rustunnel)을 사용합니다.

```
Evolver(:3004) → rustunnel → Oracle VPS(:4040→:8443 TLS) → nginx(:8080 HTTP)
```

| 서비스 | 위치 | 포트 |
|--------|------|------|
| Evolver | 로컬 Windows | `:3004` |
| Rustunnel Client | 로컬 Windows | → `:4040` |
| Rustunnel Server | Oracle VPS | `:4040` (Control), `:8443` (TLS) |
| nginx | Oracle VPS | `:8080` |

> 🔗 외부 접근: `http://129.154.63.231:8080/api/evolver/status`

---

## 남은 작업

| 항목 | 우선순위 | 비고 |
|------|----------|------|
| 도메인 설정 + Let's Encrypt TLS | 높음 | HTTP → HTTPS |
| 일일 리포트 자동화 | 높음 | Cron + 텔레그램 알림 |
| Windows 방화벽 스크립트 | 중간 | 관리자 권한 자동 실행 |
| Walk-Forward Validation 강화 | 중간 | Rust 백테스터 |
| 멀티 심볼 확장 (BTC, ETH, SOL) | 낮음 | 현재 BTC 중심 |

---

## 배운 것들

### 🔬 핵심 교훈

1. **CGO 없이 SQLite 쓰기:** Windows 환경에서 CGO는 골칫거리입니다. 순수 Go 구현체(`glebarez/sqlite`)를 쓰면 빌드가 훨씬 안정적입니다.

2. **AI 모델 로테이션:** 무료 모델 5개를 Round-Robin으로 돌리면 하나의 모델에 의존하지 않고, Rate Limit도 분산됩니다. *단점은 각 모델의 품질 편차가 크다는 것.*

3. **헌법(Constitution) 패턴:** AI 시스템이 자율적으로 돌아갈 때, "절대 넘지 말아야 할 선"을 코드가 아닌 설정 파일로 관리하면 유지보수가 쉽습니다.

### 🚀 응용 가능 분야

- **자율주행:** 안전 규칙을 Constitution으로 정의, AI가 위반하면 즉시 차단
- **게임 AI:** 전략 진화 루프로 NPC 행동 패턴 자동 개선
- **포트폴리오 관리:** 암호화폐뿐 아니라 주식, ETF에도 동일 아키텍처 적용 가능

---

> 100개 전략을 동시에 돌리고, AI가 죽은 전략은 버리고 산 전략은 더 진화시키는 시스템 — DSL Strategy Evolver는 단순한 트레이딩 봇이 아니라 **전략의 자연선택을 시뮬레이션하는 연구 플랫폼**입니다.

## 관련 링크

- [PRD 문서](https://github.com/sfex11/victus_public/blob/main/PRD.md)
- [설계 문서 (DESIGN.md)](https://github.com/sfex11/victus_public/blob/main/DESIGN.md)
- [팀 PRD (TEAM-PRD.md)](https://github.com/sfex11/victus_public/blob/main/TEAM-PRD.md)
- [업그레이드 계획 (UPGRADE_PLAN.md)](https://github.com/sfex11/victus_public/blob/main/UPGRADE_PLAN.md)
- [영감을 준 프로젝트: Quant-Autoresearch](https://github.com/yllvar/Quant-Autoresearch)
- [Rustunnel](https://github.com/nicobailon/rustunnel)

---

*이 글은 2026년 3월 25일~28일까지의 작업 기록을 바탕으로 작성되었습니다.*
