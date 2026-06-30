import uvicorn
from fastapi import FastAPI

app = FastAPI(title="Robin portfolio Agent")

@app.get("/health")
def health():
    return {"status": "online", "agent": "portfolio"}

if __name__ == "__main__":
    print(f"Starting portfolio on port 8003...")
    uvicorn.run(app, host="0.0.0.0", port=8003)
