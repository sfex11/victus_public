# PRD - DSL 전략 탐색 시스템 (Strategy Evolver)

## 개요
암호화폐 무기한선물 100개 DSL 전략을 동시 가상매매하며, AI로 지속적으로 좋은 전략을 생성/선발하는 Go 시스템.

## 핵심 기능

### 1. DSL 전략 엔진
- YAML 기반 전략 정의 언어
- 진입/청산/리스크 관리 조건 표현
- 100개 전략 동시 실행

### 2. Paper Trading
- BTC/USDT, ETH/USDT, SOL/USDT 무기한선물
- 롱+숏 동시 포지션 (헤징 모드)
- Go 백엔드와 동일한 DB 사용 (`data/trading.db`)

### 3. AI 전략 생성/평가
- OpenRouter API 사용
- 5개 무료 모델 번갈아 사용:
  - stepfun/step-3.5-flash:free
  - nvidia/nemotron-3-super:free
  - arcee-ai/trinity-large-preview:free
  - z-ai/glm-4.5-air:free
  - qwen/qwen3-coder:free
- 전략 생성 → 백테스팅 → Paper Trading → 평가 → 선발

### 4. 진화 루프
```
1. AI가 새 전략 N개 생성
2. 백테스팅으로 1차 필터링
3. Paper Trading으로 실시간 검증
4. 성과 기반 순위 매기기
5. 하위 전략 제거 → 새 전략으로 교체
6. 반복
```

## 기술 스택
- **언어**: Go 1.21+
- **DB**: SQLite (기존 `data/trading.db` 공유)
- **API**: Binance Futures, OpenRouter
- **DSL**: YAML 파싱 + 표현식 평가

## 데이터 소스
- Go 백엔드(3002)에서 이미 수집 중인 시장 데이터 활용:
  - `market_1h_candles` (1시간봉)
  - `market_funding_rate` (펀딩레이트)

## API 구조
```
POST /api/evolver/start     - 진화 루프 시작
POST /api/evolver/stop      - 진화 루프 중지
GET  /api/evolver/status    - 현재 상태 (전략 수, 성과)
GET  /api/evolver/strategies - 전략 목록 + 순위
POST /api/evolver/generate  - AI로 새 전략 N개 생성
GET  /api/evolver/leaderboard - 상위 전략 목록
```

## 품질 기준 (체크리스트)

### 기능
- [ ] DSL 파서가 YAML을 파싱하여 실행 가능한 전략으로 변환
- [ ] 100개 전략 동시 Paper Trading 실행
- [ ] 롱+숏 동시 포지션 지원
- [ ] OpenRouter API로 전략 생성
- [ ] 5개 모델 로테이션
- [ ] 백테스팅 실행
- [ ] 성과 기반 순위 매기기
- [ ] 하위 전략 자동 교체

### 성능
- [ ] 100개 전략 60초 내 전부 평가 완료
- [ ] 메모리 사용 500MB 이하
- [ ] API 호출 실패 시 재시도

### 통합
- [ ] 기존 Go 백엔드 DB와 공유
- [ ] 기존 시장 데이터 사용
- [ ] 포트 3004에서 실행 (충돌 방지)

## 프로젝트 구조
```
dsl-strategy-evolver/
├── cmd/
│   └── server/
│       └── main.go
├── internal/
│   ├── dsl/           # DSL 파서 + 런타임
│   ├── engine/        # Paper Trading 엔진
│   ├── ai/            # OpenRouter 클라이언트
│   ├── evolver/       # 진화 루프 관리
│   ├── api/           # HTTP 핸들러
│   └── models/        # 데이터 모델
├── strategies/        # 생성된 DSL 전략 저장
├── config/
│   └── config.yaml
└── go.mod
```

## OpenRouter 설정
```yaml
openrouter:
  api_key: "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"
  base_url: "https://openrouter.ai/api/v1"
  models:
    - stepfun/step-3.5-flash:free
    - nvidia/nemotron-3-super:free
    - arcee-ai/trinity-large-preview:free
    - z-ai/glm-4.5-air:free
    - qwen/qwen3-coder:free
  rotation: round-robin
```

## 롱숏 동시 포지션 전략 예시
```yaml
name: "HedgedGrid"
symbol: "BTCUSDT"
type: "hedge"  # 롱+숏 동시

long:
  entry: "price < ema(20) * 0.99"
  exit: "price > ema(20) * 1.02"
  stop_loss: 0.02
  
short:
  entry: "price > ema(20) * 1.01"
  exit: "price < ema(20) * 0.98"
  stop_loss: 0.02

risk:
  position_size: 100  # USDT
  max_positions: 2    # 롱1 + 숏1
```

## 기대 효과
- 자동으로 수익성 높은 전략 발견
- 다양한 AI 모델의 창의성 활용
- 지속적인 전략 개선
