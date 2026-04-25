"""Dual System Monitor - NFI + BigVolver 상시 모니터링 스크립트

1분마다 Freqtrade 상태를 조회하고 NFI 시그널이 발생하면 기록
주기적으로 대시보드 갱신
"""
import sys
import os
import json
import time
from datetime import datetime, timezone, timedelta
from pathlib import Path

sys.stdout.reconfigure(encoding='utf-8', errors='replace')
sys.path.insert(0, os.path.dirname(__file__))

from freqtrade_bridge import FreqtradeBridge

KST = timezone(timedelta(hours=9))
LOG_DIR = Path(__file__).parent.parent / "logs"
LOG_DIR.mkdir(exist_ok=True)


def log(message):
    ts = datetime.now(KST).strftime("%Y-%m-%d %H:%M:%S")
    line = f"[{ts}] {message}"
    print(line)
    with open(LOG_DIR / "dual_monitor.log", "a", encoding="utf-8") as f:
        f.write(line + "\n")


def main():
    log("=" * 50)
    log("Dual System Monitor Started")
    log("=" * 50)

    bridge = FreqtradeBridge()
    prev_trades = set()
    cycle = 0

    while True:
        cycle += 1
        try:
            if not bridge.health_check():
                log("[WARN] Freqtrade API unreachable, waiting...")
                time.sleep(30)
                continue

            # 현재 거래 조회
            trades = bridge.get_nfi_signals()
            active_pairs = set()

            for t in trades:
                if t["is_open"]:
                    active_pairs.add(t["pair"])
                    # 새로운 진입 감지
                    if t["pair"] not in prev_trades:
                        log(f"[SIGNAL] NFI NEW: {t['pair']} ({t['mode']}) stake={t['stake_amount']}")

            # 청산 감지
            for pair in prev_trades:
                if pair not in active_pairs:
                    log(f"[SIGNAL] NFI CLOSE: {pair}")

            prev_trades = active_pairs

            # 10분마다 상태 리포트
            if cycle % 10 == 0:
                balance = bridge.get_balance()
                profit = bridge.get_profit()
                btc_price = bridge.get_current_price("BTC/USDT")

                closed_profit = profit.get("profit_closed_coin", 0)
                open_trades = profit.get("open_trades", 0)
                total = len(trades)

                log(f"[STATUS] BTC=${btc_price:,.2f} | Active={total} | Closed PnL={closed_profit:.4f} USDT | Open trades={open_trades}")

                # 상태를 JSON으로 저장 (대시보드용)
                state = {
                    "timestamp": datetime.now(KST).isoformat(),
                    "btc_price": btc_price,
                    "nfi_active_trades": total,
                    "nfi_closed_pnl": closed_profit,
                    "balance": balance,
                    "profit": profit,
                    "signals": trades,
                }
                state_file = LOG_DIR / "latest_state.json"
                state_file.write_text(json.dumps(state, ensure_ascii=False, indent=2), encoding="utf-8")

            time.sleep(60)

        except KeyboardInterrupt:
            log("Monitor stopped by user")
            break
        except Exception as e:
            log(f"[ERROR] {e}")
            time.sleep(60)


if __name__ == "__main__":
    main()
