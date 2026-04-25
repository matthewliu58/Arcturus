#!/bin/bash

# Log file path
LOG="results/ping_monitor.csv"

# Create results directory if it does not exist
mkdir -p results

# Write CSV header
echo "time,ip,status,avg_rtt_ms" > "$LOG"

# List of target IP addresses to monitor
IP_LIST=(
    "1.1.1.1"
    "8.8.8.8"
    "your_frankfurt_ip"
    "your_tokyo_ip"
)

# Main loop: run checks periodically
while true; do
    echo "===== Starting check round: $(date) ====="

    # Loop through each IP
    for ip in "${IP_LIST[@]}"; do
        # Ping each IP 100 times with 2-second timeout
        res=$(ping -c 100 -W 2 "$ip" 2>/dev/null)

        # Determine if the IP is up or down
        if echo "$res" | grep -q "100% packet loss"; then
            status="DOWN"
            avg="NA"
        else
            status="UP"
            avg=$(echo "$res" | awk -F'/' '/mmax/ {print $5}')
        fi

        # Append result to log file
        echo "$(date '+%Y-%m-%d %H:%M:%S'),$ip,$status,$avg" >> "$LOG"
        echo "$ip => $status | avg: $avg ms"
    done

    # Wait 30 minutes (1800 seconds) before next round
    # Change to 3600 for 1-hour interval
    echo ""
    echo "Waiting 30 minutes for next check round..."
    echo ""
    sleep 1800
done