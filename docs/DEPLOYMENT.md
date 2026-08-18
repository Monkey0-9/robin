# Robin Quantitative Trading Platform — Production Deployment Guide
**Document ID:** SPEC-DEP-202608-01  
**Classification:** SRE & Operations Deployment Guide  

---

## 1. Production Docker Orchestration

Launch the full 12-service production stack with real-time CPU priority, memory locks, and IPC hugepages:

```bash
# 1. Build and verify all images
docker compose -f infra/docker-compose.prod.yml build

# 2. Start stack in detached mode
docker compose -f infra/docker-compose.prod.yml up -d

# 3. Verify health status of all 12 services
docker compose -f infra/docker-compose.prod.yml ps
```

---

## 2. Kernel & Systemd Production Prerequisites

```bash
# 1. Configure 2MB Hugepages for DPDK & C++ NUMA memory pools
sudo sysctl -w vm.nr_hugepages=2048

# 2. Unlock memlock limits for zero-page-fault guarantees
echo "* - memlock unlimited" | sudo tee -a /etc/security/limits.conf
echo "* - rtprio 99" | sudo tee -a /etc/security/limits.conf

# 3. Install systemd services
sudo cp infra/systemd/*.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now robin-gateway robin-risk robin-match
```
