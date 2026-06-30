import uvicorn
from fastapi import FastAPI

app = FastAPI(title="Robin market-data Agent")

@app.get("/health")
def health():
    return {"status": "online", "agent": "market-data"}

if __name__ == "__main__":
    print(f"Starting market-data on port 8001...")
    uvicorn.run(app, host="0.0.0.0", port=8001)
