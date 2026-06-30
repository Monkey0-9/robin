# Changelog

All notable changes to the Robin Platform will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.1.0] - 2026-06-30

### Added
- Comprehensive `CHANGELOG.md` file.
- `CONTRIBUTING.md` file detailing guidelines for team collaboration.
- Connection pooling documentation.
- Sample configuration file (`config/robin_config.example.yaml`) for quick developer setup.
- Health check endpoints (`/ready`, `/live`) across Gateway and AI Agent services for better orchestration and Kubernetes support.
- Graceful shutdown lifecycle hooks in the AI Agent (FastAPI).

### Changed
- Configured rate limiter tuning applied to the `/order` and `/config` endpoints in the Gateway to prevent overload during traffic spikes.
