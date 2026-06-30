# Contributing to Robin Platform

First off, thank you for considering contributing to the Robin Platform! It's people like you that make this platform robust, secure, and fast.

## 1. Where do I go from here?

If you've noticed a bug or have a feature request, please make sure you create an issue or check existing issues before starting work.

## 2. Setting up your environment

### Prerequisites
- Go 1.21+
- Python 3.10+
- Node.js (for frontend)
- Docker & Docker Compose (for KDB+ / PostgreSQL dependencies)
- Access to an FPGA development board (Optional, for hardware-accelerated matching engine work)

### Running the Dev Environment
We have provided sample configuration files and `env.example`.
Copy `env.example` to `.env` and `config/robin_config.example.yaml` to `config/robin_config.yaml` to get started.

Run the entire stack via Docker:
```bash
docker-compose up -d
```
Or use the provided `start_all.bat` / `Makefile` scripts for local non-containerized testing.

## 3. Making Changes

- **Code Style**: 
  - Go: Run `go fmt` and `golangci-lint`.
  - Python: Use `black`, `isort`, and `flake8` for formatting and linting.
- **Testing**:
  - Run all tests before submitting a PR.
  - Gateway: `cd services/gateway && go test -v ./...`
  - Ensure all health checks (`/live`, `/ready`) pass.
- **Commit Messages**: Write clear and descriptive commit messages following the Conventional Commits format.

## 4. Submitting a Pull Request
1. Fork the repo and create your branch from `main`.
2. Ensure the test suite passes.
3. Update the `CHANGELOG.md` with your changes.
4. Issue that pull request!
