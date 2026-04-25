"""Freqtrade REST API 브릿지 — DualEngine에서 Freqtrade 데이터/시그널을 읽기 위한 어댑터

BigVolver DualEngine이 Freqtrade API와 통신하여:
1. 캔들 데이터 조회 (pair_candles)
2. NFI 거래 시그널 수신 (trades)
3. 현재 포지션 상태 조회
"""

import requests
import json
from datetime import datetime, timezone, timedelta

KST = timezone(timedelta(hours=9))


class FreqtradeBridge:
    """Freqtrade REST API 브릿지"""

    def __init__(self, base_url="http://localhost:8081", username="bigvolver", password="bigvolver_dual_2026"):
        self.base_url = base_url.rstrip("/")
        self.auth = (username, password)
        self.session = requests.Session()
        self.session.auth = self.auth

    def health_check(self):
        """Freqtrade API 상태 확인"""
        try:
            r = self.session.get(f"{self.base_url}/api/v1/ping", timeout=5)
            return r.status_code == 200
        except:
            return False

    def show_config(self):
        """현재 설정 조회"""
        try:
            r = self.session.get(f"{self.base_url}/api/v1/show_config", timeout=5)
            r.raise_for_status()
            return r.json()
        except Exception as e:
            return {"error": str(e)}

    def get_pair_candles(self, pair, timeframe="5m", limit=100):
        try:
            r = self.session.get(
                f"{self.base_url}/api/v1/pair_candles",
                params={"pair": pair, "timeframe": timeframe, "limit": limit},
                timeout=10
            )
            if r.status_code == 200:
                data = r.json()
                if isinstance(data, dict):
                    # Freqtrade는 columns + data_array 형식 반환
                    columns = data.get("columns", [])
                    candles_raw = data.get("data", [])
                    if columns and candles_raw:
                        result = []
                        for row in candles_raw:
                            candle = dict(zip(columns, row))
                            result.append(candle)
                        return result
                    # 대안: data 필드
                    return data.get("data", data.get("candles", []))
                return data if isinstance(data, list) else []
            return []
        except Exception as e:
            print(f"[Bridge] candle fetch failed ({pair}): {e}")
            return []

    def get_whitelist(self):
        """현재 활성화된 페어 목록"""
        try:
            r = self.session.get(f"{self.base_url}/api/v1/whitelist", timeout=5)
            r.raise_for_status()
            data = r.json()
            return data.get("whitelist", [])
        except Exception as e:
            return []

    def get_current_price(self, pair):
        candles = self.get_pair_candles(pair, "5m", 1)
        if candles and len(candles) > 0:
            last = candles[-1]
            if isinstance(last, dict):
                # Freqtrade는 컬럼명이 date/open/high/low/close/volume
                close_val = last.get("close", 0)
                if close_val:
                    return float(close_val)
        # Fallback: 공개 API
        try:
            symbol = pair.replace("/", "")
            r = requests.get(f"https://api.binance.com/api/v3/ticker/price?symbol={symbol}", timeout=5)
            if r.status_code == 200:
                return float(r.json().get("price", 0))
        except:
            pass
        return 0.0

    def get_all_current_prices(self):
        """모든 화이트리스트 페어의 현재가"""
        whitelist = self.get_whitelist()
        prices = {}
        for pair in whitelist:
            price = self.get_current_price(pair)
            if price > 0:
                prices[pair] = price
        return prices

    def get_trades(self, limit=20):
        """NFI의 현재 진행 중인 거래 조회"""
        try:
            r = self.session.get(
                f"{self.base_url}/api/v1/status",
                timeout=5
            )
            r.raise_for_status()
            return r.json()
        except Exception as e:
            print(f"[Bridge] 거래 상태 조회 실패: {e}")
            return []

    def get_profit(self):
        """현재 총 수익"""
        try:
            r = self.session.get(f"{self.base_url}/api/v1/profit", timeout=5)
            r.raise_for_status()
            return r.json()
        except Exception as e:
            return {"error": str(e)}

    def get_balance(self):
        """현재 잔고"""
        try:
            r = self.session.get(f"{self.base_url}/api/v1/balance", timeout=5)
            r.raise_for_status()
            return r.json()
        except Exception as e:
            return {"error": str(e)}

    def get_nfi_signals(self):
        """NFI의 진입/청산 시그널을 추출
        
        Returns:
            list: [{"pair": str, "direction": str, "mode": str, "profit": float}, ...]
        """
        trades = self.get_trades()
        if not trades:
            return []

        signals = []
        for trade in trades:
            pair = trade.get("pair", "")
            # Freqtrade는 spot만 지원하므로 LONG만
            direction = "LONG"
            mode = trade.get("enter_tag", "unknown")
            profit_pct = trade.get("profit_pct", 0)
            profit_abs = trade.get("profit_abs", 0)
            is_open = trade.get("is_open", False)

            signals.append({
                "pair": pair,
                "direction": direction,
                "mode": mode,
                "profit_pct": profit_pct,
                "profit_abs": profit_abs,
                "is_open": is_open,
                "open_date": trade.get("open_date", ""),
                "stake_amount": trade.get("stake_amount", 0),
                "trade_id": trade.get("trade_id", 0),
            })

        return signals

    def export_state_for_bigvolver(self):
        """BigVolver DualEngine에 전달할 전체 상태 내보내기
        
        Returns:
            dict: NFI 현재 상태 (시그널 + 가격 + 잔고)
        """
        state = {
            "timestamp": datetime.now(KST).isoformat(),
            "health": self.health_check(),
            "trades": self.get_nfi_signals(),
            "balance": self.get_balance(),
            "profit": self.get_profit(),
            "whitelist": self.get_whitelist(),
            "prices": {},
        }

        # 현재 가격 조회 (시그널이 있는 페어만)
        for trade in state["trades"]:
            pair = trade["pair"]
            if pair not in state["prices"]:
                state["prices"][pair] = self.get_current_price(pair)

        return state


if __name__ == "__main__":
    import sys
    sys.stdout.reconfigure(encoding='utf-8', errors='replace')
    bridge = FreqtradeBridge()
    print("=== Freqtrade Bridge Test ===")

    if not bridge.health_check():
        print("[FAIL] Freqtrade API에 연결할 수 없습니다")
        exit(1)

    print("[OK] Freqtrade API 연결 성공")

    # 화이트리스트
    wl = bridge.get_whitelist()
    print(f"[OK] Whitelist: {len(wl)} pairs")
    if wl:
        print(f"     Top 10: {wl[:10]}")

    # 현재 거래
    trades = bridge.get_nfi_signals()
    print(f"[OK] Trades: {len(trades)}")
    for t in trades[:5]:
        print(f"     {t['pair']} ({t['mode']}) PnL: {t['profit_pct']:.2f}%")

    # 잔고
    balance = bridge.get_balance()
    print(f"[OK] Balance: {json.dumps(balance, ensure_ascii=False)[:200]}")

    # 수익
    profit = bridge.get_profit()
    print(f"[OK] Profit: {json.dumps(profit, ensure_ascii=False)[:200]}")

    # BTC 가격
    btc_price = bridge.get_current_price("BTC/USDT")
    print(f"[OK] BTC/USDT: ${btc_price:,.2f}")
