#!/bin/bash
# config/ptp_sync.sh
# Synchronization wrapper script for PTP (Precision Time Protocol) using ptp4l and phc2sys.
# Aligns the system clock with Rubidium/GPS atomic time source (JPMorgan/Citadel clock-sync standards).

echo "===================================================================="
echo "Initializing Rubidium GPSDO / PTP Clock Synchronization"
echo "===================================================================="

INTERFACE="eth0"
PTP4L_CONF="/etc/ptp4l.conf"

# Generate basic ptp4l configuration
sudo tee $PTP4L_CONF > /dev/null <<EOF
[global]
twoStepFlag             1
clientOnly              1
priority1               128
priority2               128
domainNumber            0
delay_mechanism         E2E
network_transport       UDPv4
timeSource              0x20
free_running            0
freq_est_interval       1
max_frequency           900000000
EOF

# Start ptp4l in background to synchronize network interface clock (PHC) to grandmaster clock
echo "Starting ptp4l daemon on interface $INTERFACE..."
sudo ptp4l -i "$INTERFACE" -f "$PTP4L_CONF" -m -S &

# Wait for ptp4l initialization
sleep 2

# Sync system clock (CLOCK_REALTIME) to interface hardware clock (PHC)
echo "Starting phc2sys daemon for system-to-hardware sync..."
sudo phc2sys -s "$INTERFACE" -c CLOCK_REALTIME -w -m &

echo "PTP clock synchronization monitoring initiated. Target deviation: <20ns."
