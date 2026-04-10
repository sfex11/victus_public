# BigVolver V2 — 성공 사례 기반 개선 적용 계획서

**날짜:** 2026-04-10
**작성:** 헥스 (오케스트레이터) + 빅터스 (실행자)
**승인:** 철현 황 회장

---

## 1. 배경

BigVolver Phase 2-3이 완료되었으나, **수익성 있는 전략을 발견하지 못함**. 원인 분석 결과:
- LLM 기반 전략 생성의 품질 한계
- ML/DRL 검증된 기법 미도입
- 실시간 self-adaptive retraining 부재

성공 사례 조사(FinRL-X, FreqAI, Quant-Autoresearch)에서 도출된 공통 패턴을 적용.

---

## 2. 핵심 전환: LLM-First → ML/DRL-First

| 기존 (V1) | 개선 (V2) |
|------------|-----------|
| LLM가 전략 생성 주체 | ML 모델(LightGBM/XGBoost)이 전략 생성 주체 |
| LLM가 DSL 작성 | LLM은 피처 엔지니어링 + 하이퍼파라미터 탐색 보조 |
| 5개 무료 모델 로테이션 | LightGBM + DRL(PPO/SAC) 혼합 |
| 정적 진화 루프 | Self-adaptive retraining (실시간 재훈련) |

**핵심:** LLM을 버리는 게 아니라 **역할을 재배치**. LLM은 피처 발굴, 하이퍼파라미터 탐색, 전략 설명에 활용.

---

## 3. 적용 로드맵

### Phase A: ML 파이프라인 구축 (P0, 1주)

| 순서 | 작업 | 담당 | 산출물 |
|------|------|------|--------|
| A1 | Binance 데이터 → 피처 엔지니어링 파이프라인 | 빅터스 | `internal/ml/features.go` |
| A2 | LightGBM Python 서비스 래핑 (Go↔Python) | 빅터스 | `internal/ml/predictor.go` + `ml_service/` |
| A3 | 피처셋 정의 (기술지표 + 펀딩레이트 + 거래량) | 헥스 | `config/features.yaml` |
| A4 | Walk-Forward 백테스트 연동 | 빅터스 | 기존 Rust 백테스터 활용 |

**피처셋 구성 (초안):**
```yaml
features:
  technical:
    - ema_5, ema_20, ema_50, ema_200
    - rsi_14, rsi_28
    - macd_line, macd_signal, macd_histogram
    - atr_14, bollinger_upper, bollinger_lower
    - adx_14, obv
    - volume_ratio (volume / ema(volume, 20))
  
  market_microstructure:
    - funding_rate (현재, 1h/4h/8h 변화)
    - open_interest (변화율)
    - long_short_ratio
    - top_trader_long_short_ratio
  
  derived:
    - volatility_1h, volatility_4h, volatility_24h
    - momentum_1h, momentum_4h
    - mean_reversion_score
    - regime_label (trending/ranging/volatile)
  
  target:
    - future_return_1h (> 0 = 1, < 0 = 0) — 분류
    - future_return_4h — 회귀
```

### Phase B: Self-Adaptive Retraining (P0, 3일)

| 순서 | 작업 | 담당 | 산출물 |
|------|------|------|--------|
| B1 | 모델 재훈련 스케줄러 (매 6시간) | 빅터스 | `internal/ml/retrainer.go` |
| B2 | 데이터 윈도우 슬라이딩 (최근 30일 학습) | 빅터스 | `internal/ml/data_window.go` |
| B3 | 모델 성능 모니터링 + 자동 롤백 | 빅터스 | 기존 Auto Revert 확장 |
| B4 | Telegram 알림 (재훈련 결과, 성능 변화) | 헥스 | `internal/notify/telegram.go` |

### Phase C: Weight-Centric 파이프라인 (P1, 1주)

| 순서 | 작업 | 담당 | 산출물 |
|------|------|------|--------|
| C1 | Weight 인터페이스 정의 | 헥스 | `internal/pipeline/weight.go` |
| C2 | ML 종목 선정 모듈 | 빅터스 | `internal/pipeline/selector.go` |
| C3 | Risk 오버레이 모듈 | 빅터스 | `internal/pipeline/risk_overlay.go` |
| C4 | 타이밍 조정 모듈 (KAMA 기반) | 빅터스 | `internal/pipeline/timing.go` |

**Weight-Centric 흐름:**
```
Market Data → ML Selector (종목 선정) → Allocator (비중 할당)
            → Timing (진입 타이밍) → Risk Overlay (리스크 조정)
            → 최종 Weight Vector → 실행
```

### Phase D: DRL 통합 (P2, 2주)

| 순서 | 작업 | 담당 | 산출물 |
|------|------|------|--------|
| D1 | FinRL-X PPO/SAC 에이전트 포팅 | 빅터스 | `internal/drl/agent.py` |
| D2 | Gym 환경 구축 (Binance Futures) | 빅터스 | `internal/drl/env.py` |
| D3 | DRL↔Go 브릿지 | 빅터스 | gRPC/REST 인터페이스 |
| D4 | DRL vs ML 벤치마크 | 헥스 | 성능 비교 보고서 |

### Phase E: 실험 추적 & 모니터링 (P1, 3일)

| 순서 | 작업 | 담당 | 산출물 |
|------|------|------|--------|
| E1 | W&B 또는 MLflow 통합 | 빅터스 | `internal/telemetry/` |
| E2 | 일일 성과 리포트 자동 생성 | 헥스 | Telegram 전송 |
| E3 | 전략 진화 히스토리 대시보드 | 헥스 | HTML 리포트 |

---

## 4. 기술 아키텍처 변경

### 기존
```
AI Generator (LLM) → DSL Parser → Paper Trading → Rank → Evolve
```

### 개선
```
┌─────────────────────────────────────────────────────────────┐
│                    BigVolver V2 Pipeline                     │
│                                                             │
│  ┌─────────────┐   ┌─────────────┐   ┌─────────────────┐  │
│  │ ML Selector │   │ ML/DRL      │   │ Risk Overlay    │  │
│  │ (LightGBM)  │→→→│ Allocator   │→→→│ (Constitution)  │  │
│  └──────┬──────┘   └──────┬──────┘   └────────┬────────┘  │
│         │                 │                    │            │
│  ┌──────┴──────┐   ┌──────┴──────┐   ┌────────┴────────┐  │
│  │ LLM Assist │   │ Timing      │   │ Telemetry (W&B) │  │
│  │ (피처/탐색) │   │ (KAMA)      │   │                 │  │
│  └─────────────┘   └─────────────┘   └─────────────────┘  │
│                                                             │
│  Self-Adaptive Retraining Loop (6h 주기)                    │
│  Binance Data → Feature Eng → Train → Validate → Deploy    │
└─────────────────────────────────────────────────────────────┘
```

---

## 5. LLM의 새로운 역할

LLM은 전략 생성에서 **보조 역할**로 전환:

| 기존 역할 | 새로운 역할 |
|-----------|------------|
| 전략 DSL 직접 생성 | 피처 엔지니어링 아이디어 제안 |
| 전략 평가 | 하이퍼파라미터 탐색 공간 정의 |
| 진화 방향 결정 | 시장 국면 해석 (뉴스/이벤트) |
| — | 생성된 전략의 인간 친화적 설명 |
| — | ArXiv 논문 → 피처 아이디어 변환 |

---

## 6. 성공 지표

| 메트릭 | V1 목표 | V2 목표 | 근거 |
|--------|---------|---------|------|
| Sharpe Ratio | > 1.0 | > 1.5 | FinRL-X: 1.96 달성 |
| Max Drawdown | < 20% | < 15% | FinRL-X: 12.22% |
| Win Rate | > 45% | > 55% | FinRL-X: 64.89% |
| 연간 수익률 | — | > 30% | FinRL-X: 62.16% |
| 재훈련 주기 | 수동 | 6시간 자동 | FreqAI: 실시간 |

---

## 7. 리스크

| 리스크 | 대응 |
|--------|------|
| Go↔Python 브릿지 성능 | gRPC + 공유메모리, 필요시 전체 Python 마이그레이션 검토 |
| LightGBM 과적합 | Walk-Forward + Monte Carlo 기존 검증 활용 |
| Binance API 제한 | 캐싱 + WebSocket 스트리밍 |
| 모델 drift (시장 변화) | Self-adaptive retraining으로 자동 대응 |

---

*BigVolver V2 — 검증된 기법으로 전략의 수익성을 증명한다.*
