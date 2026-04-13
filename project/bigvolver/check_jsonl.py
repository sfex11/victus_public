from pathlib import Path
import json

p = Path(r"C:\Users\Test\.openclaw\workspace-hex\project\bigvolver\project\bigvolver\internal\data\training_data\training_data_BTCUSDT.jsonl")
lines = p.read_text().strip().split("\n")

print(f"Total lines: {len(lines)}")

# Check how many have target
with_target = 0
without_target = 0
for i, line in enumerate(lines):
    rec = json.loads(line)
    if "target" in rec:
        with_target += 1
    else:
        without_target += 1
        if without_target <= 3:
            print(f"  No target at line {i}: keys={list(rec.keys())}")

print(f"With target: {with_target}")
print(f"Without target: {without_target}")
