# BigVolver Manifesto

## 핵심 가치 (One Line)

> **"전략이 스스로 진화한다 — 인간은 방향만 정하고, AI가 나머지를 설계·검증·도태한다."**

---

## 미션

암호화폐 무기한선물 시장에서, **AI가 만든 매매전략이 스스로 학습하고 진화하여 지속적으로 수익을 창출**하는 자율 시스템을 구축한다.

### 핵심 가치 3원칙

| 원칙 | 의미 |
|------|------|
| **자율 진화** | AI가 전략을 생성·백테스트·검증·도태하는 전체 사이클을 자동 수행 |
| **견고한 안전** | 3계층 방어 (Constitution → Doom Loop → Auto Revert)로 시스템 붕괴 방지 |
| **투명한 합의** | 4롤 멀티 에이전트가 독립적으로 평가하고 투표로 결정 — 블랙박스 없음 |

---

## 5대 서브시스템

### 🧠 1. Evolution Engine — 진화 루프
전략의 생명주기를 관리하는 심장부.

```
생성(AI) → 백테스트(과거데이터) → Paper Trading(실시간) → 평가(Rank) → 도태(하위 교체) → 반복
```

- 주기별 자동 실행 (기본 1시간)
- 100개 전략 동시 Paper Trading
- 하위 20% 자동 교체 → 상위 전략 유지
- 세대 추적 (generation numbering)

### 🔧 2. Strategy Toolkit — 전략 파서 + 엔진
전략을 정의하고 실행하는 DSL 시스템.

- **YAML DSL**: 사람도 읽을 수 있고 AI도 생성할 수 있는 전략 정의 언어
- **표현식 엔진**: `price < ema(20)` 같은 조건을 컴파일·실행
- **8종 지표**: EMA, SMA, RSI, MACD, ATR, Bollinger, ADX, OBV
- **롱+숏 동시 포지션**: 헤징 모드 지원
- **롱숏 동시 포지션**: 롱+숏 동시 보유

### 🛡️ 3. Constitution — 3계층 안전 시스템
전략 진화를 안전하게 통제하는 불변 규칙.

```
Layer 1: Constitution (불변 제약)
  → martingale 금지, look-ahead 방지, SL/TP 범위 강제
  
Layer 2: Doom Loop Detector (반복 감지)
  → 동일 전략 fingerprint 해시로 반복 생성 차단
  
Layer 3: Auto Reverter (자동 복귀)
  → 성과 하락 시 이전 최고 전략으로 자동 롤백
```

- 리스크 한도: Max Drawdown 20%, Max Leverage 3x, Position Size 200 USDT
- Nunchi Score: 종합 성과 지표 (Sharpe + WinRate + Drawdown + ProfitFactor)
- Approval Mode: Auto / Semi / Manual

### 🤝 4. Multi-Agent Consensus — 4롤 합의 시스템
전략 생성 전 4명의 전문가가 독립 평가 후 투표.

| 롤 | 가중치 | 역할 |
|-----|--------|------|
| Technical Analyst | 1x | 차트 패턴, 지표 신호 품질 |
| Sentiment Analyst | 1x | 거래량 이상, 공포/탐욕 사이클 |
| Risk Manager | 2x | 자본 보호 우선 (STOP/PAUSE에 2x 보팅) |
| Macro Strategist | 1x | 트렌드 방향, 마켓 사이클 |

- 3/4 이상 합의 → GENERATE
- 리스크매니저 반대 → 방어적 행동 우선
- 불확실성 높음 (confidence < 0.4) → 자동 PAUSE

### 📚 5. Playbook & Memory — 지식 기반
성공 패턴을 학습하고 재사용하는 장기 기억.

- **Playbook**: 검증된 전략 패턴을 시장 국면별(regime)로 저장
- **ArXiv RAG**: q-fin 논문 검색 → 전략 생성 프롬프트에 학술 근거 주입
- **Context Compactor**: 장기 실행 시 관찰 기록을 중요도 기반 압축
- **Thinking/Reasoning 2단계**: 빠른 모델(가설) → 강한 모델(전략 설계)

---

## AI 폴백 체인

무료 API 전부 장애여도 동작을 보장하는 3단계 폴백:

```
OpenRouter (5무료모델 로테이션)
  ↓ 429 / 장애
Anthropic REST API (claude-sonnet-4)
  ↓ 장애
Claude CLI (claude -p)
```

---

## 기술 스택

| 항목 | 선택 |
|------|------|
| 언어 | Go 1.21+ |
| DB | SQLite (GORM) |
| API | Binance Futures + OpenRouter + Anthropic |
| DSL | YAML + 커스텀 표현식 엔진 |
| 통신 | HTTP API (Port 3004) |
| 아키텍처 | 모듈형 (internal/ 패키지 분리) |

---

## 성공 기준 (Constitution Goal)

| 메트릭 | 임계값 |
|--------|--------|
| Sharpe Ratio | > 1.0 |
| Max Drawdown | < 20% |
| Win Rate | > 45% |
| Total Trades | >= 10 |
| Nunchi Score | > 0.5 |

---

## 향후 로드맵

- **Phase 2**: 실전 트레이딩 연동 (Binance 실계좌), 웹 대시보드
- **Phase 3**: 강화학습 기반 최적화, 감정 분석 (뉴스/소셜), 포트폴리오 관리

---

*BigVolver — 빅터스에서 탄생한 자율 진화 매매전략 시스템*
