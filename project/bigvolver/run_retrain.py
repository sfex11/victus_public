import urllib.request
import json

# Health check first
try:
    req = urllib.request.Request("http://localhost:5001/health")
    resp = urllib.request.urlopen(req, timeout=5)
    health = json.loads(resp.read())
    print(f"Health: {json.dumps(health, indent=2)}")
except Exception as e:
    print(f"Health check failed: {e}")
    exit(1)

# Retrain
url = "http://localhost:5001/retrain"
payload = json.dumps({"symbol": "BTCUSDT", "min_samples": 100}).encode()
req = urllib.request.Request(url, data=payload, headers={"Content-Type": "application/json"})

try:
    resp = urllib.request.urlopen(req, timeout=120)
    result = json.loads(resp.read())
    print(json.dumps(result, indent=2))
except Exception as e:
    body = e.read().decode() if hasattr(e, "read") else str(e)
    print(f"Error: {body}")
