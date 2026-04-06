# DSL Strategy Evolver - 업그레이드 계획

## 개요

Quant-Autoresearch 프로젝트에서 영감을 받은 13개 개선 사항을 적용합니다.

**참고 프로젝트:** https://github.com/yllvar/Quant-Autoresearch

---

## 적용 순서

### Phase 2: Go Evolver 개선 (안전성 + 자동화)

| 순서 | 기능 | 파일 | 난이도 | 상태 |
|------|------|------|--------|------|
| 1 | Constitution (제약조건 시스템) | config/constitution.yaml | ⭐⭐ | 대기 |
| 2 | Doom-Loop Detector | internal/evolver/doom_loop.go | ⭐ | 대기 |
| 3 | Constitution 로더 | internal/evolver/constitution.go | ⭐⭐ | 대기 |
| 4 | 자동 Revert 시스템 | internal/evolver/evolver.go | ⭐⭐ | 대기 |
| 5 | Context Compaction | internal/evolver/context.go | ⭐⭐⭐ | 대기 |
| 6 | Multi-Model 분리 | internal/ai/generator.go | ⭐⭐⭐ | 대기 |
| 7 | Playbook (성공 패턴 저장) | internal/evolver/playbook.go | ⭐⭐⭐ | 대기 |
| 8 | ArXiv RAG | internal/ai/research.go | ⭐⭐⭐⭐ | 대기 |

### Phase 3: Rust 백테스터 개선 (신뢰성 + 스코어링)

| 순서 | 기능 | 파일 | 난이도 | 상태 |
|------|------|------|--------|------|
| 9 | Forced Signal Lag | crates/engine/src/backtest.rs | ⭐ | 대기 |
| 10 | Look-Ahead Bias 스캐너 | crates/strategy-dsl/src/scanner.rs | ⭐⭐ | 대기 |
| 11 | Monte Carlo Permutation Test | crates/engine/src/monte_carlo.rs | ⭐⭐ | 대기 |
| 12 | Nunchi Score에 p-value 가중 | crates/engine/src/backtest.rs | ⭐ | 대기 |
| 13 | Walk-Forward Validation | crates/engine/src/walk_forward.rs | ⭐⭐⭐ | 대기 |

---

## 기능별 상세 설명

### 1. Constitution (제약조건 시스템)

**목적:** 불변의 리스크 규칙을 YAML로 정의

**파일:** `config/constitution.yaml`

```yaml
mandate: "Maximize Nunchi Score with strict risk control"

risk_limits:
  max_drawdown: 0.20
  max_leverage: 3.0
  max_position_size: 200
  min_trades: 10

forbidden_patterns:
  - "shift(-"
  - "future_data"
  - "martingale"

allowed_indicators:
  - ema
  - sma
  - rsi
  - macd
  - atr

goal:
  metric: "nunchi_score"
  threshold: 0.5
```

---

### 2. Doom-Loop Detector

**목적:** AI가 같은 전략을 반복 생성하는 것을 감지/차단

**알고리즘:**
```
fingerprint = SHA256(tool_name + sorted_params)
if fingerprints[fingerprint] >= threshold:
    block action
```

---

### 3. 자동 Revert 시스템

**목적:** 성과 하락 시 이전 코드로 자동 복구

**로직:**
```
1. 반복 전 현재 전략 코드 백업
2. 새 전략 생성 → 백테스트 → 페이퍼 트레이딩
3. 성과 > 기존? → 유지
4. 성과 <= 기존? → 백업에서 복구
```

---

### 4. Context Compaction

**목적:** 장기 실행 시 토큰 오버플로우 방지

**알고리즘:**
```
if context_usage > 80%:
    summarize old observations
    delete detailed logs
    keep only: scores, hypotheses, key decisions
```

---

### 5. Multi-Model Thinking/Reasoning 분리

**목적:** 비용 절감 + 품질 향상

**구조:**
```
Phase 1 (Thinking): 빠른 모델로 가설 생성
         ↓
Phase 2 (Reasoning): 강한 모델로 전략 작성
```

---

### 6. Playbook (성공 패턴 저장)

**목적:** 성과 좋은 전략의 핵심 패턴 재사용

**저장 내용:**
- 전략 핵심 아이디어
- 진입/청산 조건 패턴
- 성과 메트릭
- 시장 레짐 (트렌딩/레인징)

---

### 7. ArXiv RAG

**목적:** 학술 논문 기반 전략 생성

**워크플로우:**
```
1. AI가 가설 생성
2. ArXiv에서 관련 논문 검색 (bm25s)
3. 논문 내용을 프롬프트에 주입
4. 검증된 이론 기반 전략 생성
```

---

### 8. Forced Signal Lag

**목적:** Look-ahead bias 100% 차단

**구현:**
```rust
// 모든 신호를 1바 뒤로 이동
signals = signals.shift(1);
```

---

### 9. Look-Ahead Bias 스캐너

**목적:** DSL 표현식에서 미래 데이터 접근 감지

**감지 패턴:**
- `shift(-1)`, `shift(-N)`
- `future`, `next`, `ahead`
- 인덱스 역방향 접근

---

### 10. Monte Carlo Permutation Test

**목적:** 전략의 샤프 비율이 우연인지 검증

**알고리즘:**
```
1. 실제 샤프 비율 계산
2. 수익률 순서를 무작위로 섞어서 가짜 샤프 계산 (1000회)
3. p-value = (가짜 샤프 >= 실제 샤프) / 1000
4. p-value < 0.05 → 통계적 유의성 있음
```

---

### 11. Nunchi Score에 p-value 가중

**목적:** 통계적 유의성이 낮은 전략에 패널티

**수정:**
```
score = sharpe * trade_factor - dd_penalty - turnover_penalty

if p_value > 0.05:
    score *= 0.5  # 통계적 유의성 없으면 50% 패널티
```

---

### 12. Walk-Forward Validation

**목적:** 과적합 방지, Out-of-Sample 검증

**구조:**
```
Data: |----Window 1----|----Window 2----|----Window 3----|...
      | Train | Test  | Train | Test  | Train | Test  |
        70%    30%      70%    30%      70%    30%
```

**최종 점수:** OOS 기간 평균 성과

---

## 예상 효과

| 지표 | 현재 | 개선 후 |
|------|------|---------|
| 백테스트 신뢰성 | 중간 | 높음 (Walk-Forward + Monte Carlo) |
| 진화 효율성 | 중간 | 높음 (Doom-Loop + Playbook) |
| API 비용 | 기준 | -30% (Multi-Model + Doom-Loop) |
| 장기 실행 | 제한적 | 무제한 (Context Compaction) |
| 리스크 관리 | 수동 | 자동 (Constitution) |

---

## 진행 상황

- [x] Phase 1: 문서화
- [x] Phase 2: Go Evolver 개선
  - [x] Constitution (제약조건 시스템)
  - [x] Doom-Loop Detector
  - [x] Context Compactor
  - [x] Playbook (성공 패턴 저장)
  - [x] ArXiv RAG
  - [x] Multi-Model Thinking/Reasoning
  - [x] 자동 Revert 시스템
- [x] Phase 3: Rust 백테스터 개선
  - [x] Look-Ahead Bias 스캐너
  - [x] Monte Carlo Permutation Test
  - [x] Nunchi Score에 p-value 가중
  - [x] Walk-Forward Validation
  - [x] Forced Signal Lag 설정
- [ ] Phase 4: 통합 테스트 (다음 단계)

---

## 참고 자료

- Quant-Autoresearch: https://github.com/yllvar/Quant-Autoresearch
- FinRL: https://github.com/AI4Finance-Foundation/FinRL
- Freqtrade: https://github.com/freqtrade/freqtrade
