import os
os.environ["PYTHONIOENCODING"] = "utf-8"

from data_collector import BinanceCollector, DEFAULT_SYMBOLS, DEFAULT_DAYS
import argparse
import json

parser = argparse.ArgumentParser(description="BigVolver Data Collector")
parser.add_argument("--symbols", nargs="+", default=DEFAULT_SYMBOLS, help="Trading symbols")
parser.add_argument("--days", type=int, default=DEFAULT_DAYS, help="Historical days to backfill")
parser.add_argument("--db", type=str, default="bigvolver.db", help="SQLite database path")
parser.add_argument("--stats", action="store_true", help="Print collection stats")
args = parser.parse_args()

collector = BinanceCollector(args.db)

try:
    if args.stats:
        stats = collector.get_stats()
        print(json.dumps(stats, indent=2, ensure_ascii=False))
    else:
        print(f"[Collector] Backfilling {args.symbols} for {args.days} days...")
        for symbol in args.symbols:
            print(f"\n  {symbol} -- klines:")
            collector.backfill_klines(symbol, args.days)

            print(f"  {symbol} -- funding rates:")
            collector.backfill_funding_rates(symbol, args.days)

        print(f"\n  Collection Summary:")
        stats = collector.get_stats()
        for sym, info in stats.get("candles", {}).items():
            print(f"  {sym}: {info['count']} candles ({info['start']} ~ {info['end']})")
        for sym, info in stats.get("funding_rates", {}).items():
            print(f"  {sym}: {info['count']} funding rates")
finally:
    collector.close()
