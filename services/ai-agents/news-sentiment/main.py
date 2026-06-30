import uvicorn
from fastapi import FastAPI

app = FastAPI(title="Robin news-sentiment Agent")

@app.get("/health")
def health():
    return {"status": "online", "agent": "news-sentiment"}

if __name__ == "__main__":
    print(f"Starting news-sentiment on port 8006...")
    uvicorn.run(app, host="0.0.0.0", port=8006)
