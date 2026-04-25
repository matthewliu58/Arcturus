#!/bin/bash

# Required dependencies: stress-ng, sysbench
# Install on Ubuntu/Debian: sudo apt-get install stress-ng sysbench
# Install on CentOS/RHEL: sudo yum install stress-ng sysbench

# Configuration
LOG_FILE="$(dirname "$0")/cpu_perf_log.csv"
TEST_LOADS="30 40 50 60 70 80 90"
TEST_DURATION=10
STABLE_WAIT=3
# Every 30 minutes: 30 * 60 = 1800 seconds
LOOP_INTERVAL=1800

# Check dependencies
for cmd in stress-ng sysbench; do
    if ! command -v "$cmd" &> /dev/null; then
        echo "Error: $cmd is not installed. Please install it first:"
        echo "  Ubuntu/Debian: sudo apt-get install stress-ng sysbench"
        echo "  CentOS/RHEL:   sudo yum install stress-ng sysbench"
        exit 1
    fi
done

# Initialize log header
if [ ! -f "$LOG_FILE" ]; then
    echo "timestamp,cpu_load,events_per_sec,avg_latency,score" > "$LOG_FILE"
fi

echo "============================================="
echo " CPU Performance Monitor (Shell Version)"
echo " Frequency: Every 30 minutes per round"
echo " Load levels: 30% 50% 70% 90%"
echo " Log file: $LOG_FILE"
echo " Press Ctrl+C to stop"
echo "============================================="
echo ""

# Main loop
while true; do
    NOW=$(date +"%Y-%m-%d %H:%M:%S")
    echo "===== Test time: $NOW ====="

    for load in $TEST_LOADS; do
        echo "Testing $load% CPU load..."

        # Background stress
        total_time=$((STABLE_WAIT + TEST_DURATION))
        stress-ng --cpu 0 --cpu-load "$load" --timeout "$total_time" >/dev/null 2>&1 &
        stress_pid=$!

        sleep $STABLE_WAIT

        # Run benchmark and extract metrics
        output=$(sysbench cpu --time="$TEST_DURATION" run 2>/dev/null)

        eps=$(echo "$output" | awk '/events per second/ {print $4}')
        avg=$(echo "$output" | awk '/avg:/ {print $2; exit}')

        # Calculate composite score
        score=$(echo "$eps $avg" | awk '{printf "%.2f", $1 / $2}')

        # Write to log
        echo "$NOW,$load%,$eps,$avg,$score" >> "$LOG_FILE"

        # Output current result
        echo "  $load% | EPS: $eps | Latency: $avg ms | Score: $score"

        wait $stress_pid
        sleep 1
    done

    echo ""
    echo "Waiting 30 minutes for next round..."
    echo "---------------------------------------------"
    echo ""

    sleep $LOOP_INTERVAL
done