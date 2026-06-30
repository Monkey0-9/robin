import uvicorn
from fastapi import FastAPI

app = FastAPI(title="Robin strategy-generator Agent")

@app.get("/health")
def health():
    return {"status": "online", "agent": "strategy-generator"}

if __name__ == "__main__":
    print(f"Starting strategy-generator on port 8007...")
    uvicorn.run(app, host="0.0.0.0", port=8007)
