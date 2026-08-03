# Robin Runbook and Operations Guide

## Overview
This runbook provides emergency operational procedures for the Robin Institutional Trading Stack.

## Service Map
- **Gateway (Go)**: Port 8080. Handles REST and auth.
- **Risk Analytics (Rust)**: Port 9092. Pre-trade risk checks via TCP.
- **Matching Engine (C++)**: Port 9091. Limit Order Book matching via TCP.
- **AI Agent (Python)**: Port 8000. FastAPI service providing WebSocket signals and auto-trading via SHM.
- **CAT Reporter (Go)**: Port 9098. Regulatory reporting service.
- **Frontend (Next.js)**: Port 3000. React dashboard.

## Incident Response (Quick Actions)

### 1. Hard Halt (Kill Switch)
If the trading algorithm goes rogue or a massive risk violation is detected, immediately trip the hardware kill switch or run the software override:
```bash
# Triggers the HardwareKillSwitch inside Rust RiskGate
curl -X POST http://localhost:9096/admin/killswitch
```
*Effect: Rejects ALL new orders across all accounts instantly.*

### 2. Restarting the Stack
If memory corruption or a SHM desync occurs, tear down and rebuild using Docker Compose or Helm (depending on environment):
```bash
# Helm / K8s
helm upgrade --install robin-stack ./deploy/helm/robin-stack

# Local Dev
docker-compose down && docker-compose up -d
```

### 3. Log Inspection
- **Gateway/WebSockets**: Look for `[GATEWAY]` prefix in logs.
- **OMS / Risk Errors**: Inspect `robin-risk-daemon` logs for `RiskError::RegShoRestriction` or `RiskError::FatFinger`.
- **AI Agent Fails**: Check `task-708.log` (FastAPI).

## Compliance Contacts
For Consolidated Audit Trail (CAT) drops or SEC 15c3-5 limit breaches, escalate to the Chief Compliance Officer immediately. CAT logs are stored daily via the `cat-reporter` module.
