# Team PRD - DSL 전략 탐색 시스템

Create an agent team with 3 teammates.

## 공통 규칙
- 각 팀원은 자신 담당 파일만 수정
- Go 1.21+ 사용
- 모듈 간 인터페이스는 DESIGN.md 참조
- 작업 디렉토리: 현재 디렉토리
- 최종 산출물: 실행 가능한 Go 서버 (포트 3004)

---

## Teammate A: DSL Parser + Data Layer

### 담당 파일
```
internal/dsl/
├── parser.go        # YAML 파싱
├── validator.go     # 전략 검증
├── expression.go    # 표현식 평가 엔진
├── builtins.go      # 내장 함수 (ema, rsi, sma)
└── types.go         # 타입 정의

internal/data/
├── db.go            # SQLite 연결
├── candles.go       # 캔들 데이터 조회
├── funding.go       # 펀딩레이트 조회
└── repository.go    # 전략 저장소
```

### 구현 요구사항
1. YAML DSL 파싱 → Strategy 구조체 변환
2. 표현식 평가: `price < ema(20) * 0.99`
3. 내장 함수: ema(), sma(), rsi(), max(), min()
4. 헤징 모드 지원 (long/short 동시)
5. 기존 DB (`data/trading.db`) 연동
6. 시장 데이터 조회 (market_1h_candles, market_funding_rate)

### 체크리스트
- [ ] YAML 파싱 동작
- [ ] 표현식 평가 동작 (price < ema(20))
- [ ] 내장 함수 5개 구현
- [ ] DB 연결 성공
- [ ] 캔들 데이터 조회 가능

---

## Teammate B: Paper Trading Engine + Backtester

### 담당 파일
```
internal/engine/
├── engine.go        # 메인 엔진
├── strategy.go      # 전략 인스턴스 관리
├── position.go      # 포지션 관리 (Long+Short)
├── risk.go          # 리스크 관리
└── metrics.go       # 성과 메트릭

internal/backtest/
├── backtester.go    # 백테스팅 실행
├── simulator.go     # 거래 시뮬레이션
└── report.go        # 결과 리포트
```

### 구현 요구사항
1. 100개 전략 동시 실행
2. 롱+숏 동시 포지션 지원
3. 1초마다 전략 평가
4. 진입/청산 조건 자동 실행
5. 손절/익증 자동 처리
6. 백테스팅: 과거 데이터로 전략 검증
7. Sharpe, WinRate, MaxDrawdown, ProfitFactor 계산

### 체크리스트
- [ ] 100개 전략 동시 실행
- [ ] 롱+숏 포지션 생성
- [ ] 진입/청산 조건 평가
- [ ] 손절/익절 자동 처리
- [ ] 백테스팅 실행
- [ ] 메트릭 계산 (Sharpe, WinRate)

---

## Teammate C: AI Generator + Evolver + API

### 담당 파일
```
internal/ai/
├── generator.go     # OpenRouter 연동
├── model_pool.go    # 5개 모델 로테이션
├── prompts.go       # 프롬프트 템플릿
└── parser.go        # AI 응답 파싱

internal/evolver/
├── evolver.go       # 진화 루프 제어
├── cycle.go         # 사이클 실행
└── history.go       # 진화 이력

internal/rank/
├── ranker.go        # 순위 계산
└── score.go         # 점수 산출

internal/api/
├── handler.go       # HTTP 핸들러
├── routes.go        # 라우팅
└── server.go        # 서버 설정

cmd/server/
└── main.go          # 진입점
```

### 구현 요구사항
1. OpenRouter API 연동
2. 5개 무료 모델 라운드로빈 로테이션:
   - stepfun/step-3.5-flash:free
   - nvidia/nemotron-3-super:free
   - arcee-ai/trinity-large-preview:free
   - z-ai/glm-4.5-air:free
   - qwen/qwen3-coder:free
3. AI로 전략 생성 (YAML 출력)
4. 진화 루프: 생성 → 백테스트 → Paper Trading → 선발 → 교체
5. 성과 기반 순위 계산
6. HTTP API (6개 엔드포인트)
7. 포트 3004에서 실행

### API 엔드포인트
- POST /api/evolver/start
- POST /api/evolver/stop
- GET /api/evolver/status
- GET /api/evolver/strategies
- POST /api/evolver/generate
- GET /api/evolver/leaderboard

### OpenRouter 설정
```go
APIKey:  "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"
BaseURL: "https://openrouter.ai/api/v1"
```

### 체크리스트
- [ ] OpenRouter API 호출 성공
- [ ] 5개 모델 로테이션
- [ ] AI 응답 → YAML 전략 변환
- [ ] 진화 루프 실행
- [ ] 6개 API 엔드포인트 동작
- [ ] 포트 3004에서 서버 실행

---

## 최종 통합

Team Lead가 모든 모듈을 통합하여 실행 가능한 서버로 완성:
1. main.go에서 모든 컴포넌트 연결
2. go.mod 생성
3. 의존성 해결
4. 빌드 테스트
5. 기본 실행 확인
