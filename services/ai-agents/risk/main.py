import uvicorn
from fastapi import FastAPI

app = FastAPI(title="Robin risk Agent")

@app.get("/health")
def health():
    return {"status": "online", "agent": "risk"}

if __name__ == "__main__":
    print(f"Starting risk on port 8004...")
    uvicorn.run(app, host="0.0.0.0", port=8004)
