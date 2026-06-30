import uvicorn
from fastapi import FastAPI

app = FastAPI(title="Robin execution Agent")

@app.get("/health")
def health():
    return {"status": "online", "agent": "execution"}

if __name__ == "__main__":
    print(f"Starting execution on port 8005...")
    uvicorn.run(app, host="0.0.0.0", port=8005)
