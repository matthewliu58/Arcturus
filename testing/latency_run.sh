#!/bin/bash

# Log file path (in the same directory as the script)
LOG="$(dirname "$0")/ping_monitor.csv"

# Write CSV header
echo "time,ip,status,avg_rtt_ms" > "$LOG"

# List of target IP addresses to monitor
IP_LIST=(
    "34.48.113.182"
    "198.199.67.247"
    "47.90.140.166"
    "35.234.103.240"
    "134.122.75.45"
    "47.245.143.155"
    "34.94.91.108"
    "64.227.92.19"
    "47.89.252.104"
    "35.200.105.130"
    "157.230.41.239"
    "47.238.70.228"
    "34.39.96.45"
    "46.101.47.75"
    "8.208.34.84"
)

# Main loop: run checks periodically
while true; do
    echo "===== Starting check round: $(date) ====="

    # Loop through each IP
    for ip in "${IP_LIST[@]}"; do
        # Ping each IP 100 times with 2-second timeout
        res=$(ping -c 10 -W 2 "$ip" 2>/dev/null)
        
        # Debug: show raw ping output
        echo "--- Debug output for $ip ---"
        echo "$res"
        echo "--------------------------------"

        # Determine if the IP is up or down
        if echo "$res" | grep -q "0 packets received"; then
            status="DOWN"
            avg="NA"
        else
            status="UP"
            avg=$(echo "$res" | awk '/^rtt/{split($4,a,"/"); print a[2]}')
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