import uvicorn
from fastapi import FastAPI

app = FastAPI(title="Robin alpha-research Agent")

@app.get("/health")
def health():
    return {"status": "online", "agent": "alpha-research"}

if __name__ == "__main__":
    print(f"Starting alpha-research on port 8002...")
    uvicorn.run(app, host="0.0.0.0", port=8002)
