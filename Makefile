.PHONY: dev dev-gateway dev-ai dev-frontend dev-robin-swarm build test e2e docker-up docker-down clean

# ── Local Development ──────────────────────────────────────────────────────────

## Start all services natively (Gateway + AI Agent + Frontend + Swarm)
dev-native:
	@start "Robin Gateway" cmd /k "C:\Robin\start_gateway.bat"
	@timeout /t 2 /nobreak > nul
	@start "Robin AI Agent" cmd /k "cd services\ai-agent && python main.py"
	@timeout /t 2 /nobreak > nul
	@start "Robin Frontend" cmd /k "cd frontend && npm run dev"
	@start "Robin Swarm" cmd /k "docker-compose up robin-swarm -d"
	@echo "[Robin] Services starting. Open http://localhost:3000"

## Start only the Go gateway
dev-gateway:
	cd services\gateway && go run .

## Start only the Python AI agent
dev-ai:
	cd services\ai-agent && python main.py

## Start only the Next.js frontend
dev-frontend:
	cd frontend && npm run dev

## Start Robin Swarm engine
dev-robin-swarm:
	@echo "Starting Robin Swarm engine..."
	docker-compose up robin-swarm -d

# ── Build ──────────────────────────────────────────────────────────────────────

## Build Go gateway binary
build-gateway:
	cd services\gateway && go build -o gateway.exe .

## Build Next.js production bundle
build-frontend:
	cd frontend && npm run build

## Build everything
build: build-gateway build-frontend

# ── Testing ────────────────────────────────────────────────────────────────────

## Run Go unit tests
test-go:
	cd services\gateway && go test ./... -v -timeout 30s

## Run frontend tests (vitest)
test-frontend:
	cd frontend && npm test -- --run

## Run all tests
test: test-go test-frontend

## Run E2E integration test against running services
e2e:
	powershell -ExecutionPolicy Bypass -File scripts\e2e_test.ps1

# ── Docker ─────────────────────────────────────────────────────────────────────

## Start all services via Docker Compose
docker-up:
	docker-compose up --build -d

## Stop all Docker Compose services
docker-down:
	docker-compose down

## Show Docker Compose logs
docker-logs:
	docker-compose logs -f

# ── Utilities ──────────────────────────────────────────────────────────────────

## Remove build artifacts
clean:
	cd services\gateway && del /f gateway.exe 2>nul
	cd frontend && rmdir /s /q .next 2>nul
	@echo "[Robin] Cleaned build artifacts."

## Check gateway health
health:
	curl -s http://localhost:8080/health | python -m json.tool

## Show help
help:
	@echo.
	@echo   Robin Institutional Trading Platform
	@echo   make dev              Start all services (no Docker)
	@echo   make build            Build all binaries
	@echo   make test             Run all tests
	@echo   make e2e              Run E2E integration tests
	@echo   make docker-up        Start via Docker Compose
	@echo   make health           Check gateway health endpoint
	@echo   make clean            Remove build artifacts
	@echo.
