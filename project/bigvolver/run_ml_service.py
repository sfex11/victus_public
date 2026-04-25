import os
os.environ["PYTHONIOENCODING"] = "utf-8"
os.environ["TRAINING_DATA_DIR"] = r"C:\Users\Test\.openclaw\workspace-hex\project\bigvolver\project\bigvolver\internal\data\training_data"
os.environ["MODEL_DIR"] = r"C:\Users\Test\.openclaw\workspace-hex\project\bigvolver\project\bigvolver\models"

import sys
sys.path.insert(0, r"C:\Users\Test\.openclaw\workspace-hex\project\bigvolver\project\bigvolver\ml_service")

from server import app
print(f"[LAUNCHER] TRAINING_DATA_DIR: {os.environ['TRAINING_DATA_DIR']}")
print(f"[LAUNCHER] MODEL_DIR: {os.environ['MODEL_DIR']}")
app.run(host="0.0.0.0", port=5001, debug=False)
