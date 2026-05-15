#!/bin/bash

# Required dependencies: stress-ng, sysbench
# Ubuntu/Debian:
#   sudo apt-get install stress-ng sysbench
# CentOS/RHEL:
#   sudo yum install stress-ng sysbench

# =========================================================
# Configuration
# =========================================================

LOG_FILE="$(dirname "$0")/cpu_perf_log.csv"

# CPU load levels to test
TEST_LOADS="30 40 50 60 70 80 90"

# Benchmark duration (seconds)
TEST_DURATION=15

# Wait for stress to stabilize (seconds)
STABLE_WAIT=5

# Run every 30 minutes: 30 * 60 = 600 seconds
LOOP_INTERVAL=1800

# Use all available vCPUs
CPU_THREADS=$(nproc)

# =========================================================
# Dependency Check
# =========================================================

for cmd in stress-ng sysbench; do
    if ! command -v "$cmd" &> /dev/null; then
        echo "Error: $cmd is not installed."
        echo "Ubuntu/Debian: sudo apt-get install stress-ng sysbench"
        echo "CentOS/RHEL:   sudo yum install stress-ng sysbench"
        exit 1
    fi
done

# =========================================================
# Initialize CSV
# =========================================================

if [ ! -f "$LOG_FILE" ]; then
    echo "timestamp,cpu_load,threads,events_per_sec,avg_latency_ms,score" > "$LOG_FILE"
fi

# =========================================================
# Start
# =========================================================

echo "=================================================="
echo " CPU Performance Monitor"
echo "=================================================="
echo "Detected vCPUs      : $CPU_THREADS"
echo "Test loads          : $TEST_LOADS"
echo "Benchmark duration  : ${TEST_DURATION}s"
echo "Run interval        : Every 10 minutes"
echo "Log file            : $LOG_FILE"
echo "Press Ctrl+C to stop"
echo "=================================================="
echo ""

# =========================================================
# Main Loop
# =========================================================

while true; do
    NOW=$(date +"%Y-%m-%d %H:%M:%S")

    echo "=================================================="
    echo "Test round started at: $NOW"
    echo "=================================================="

    for load in $TEST_LOADS; do
        echo ""
        echo "Testing CPU load: ${load}%"

        # Total stress time = warmup + benchmark
        TOTAL_TIME=$((STABLE_WAIT + TEST_DURATION))

        # Stress all CPUs
        stress-ng \
            --cpu "$CPU_THREADS" \
            --cpu-load "$load" \
            --timeout "${TOTAL_TIME}s" \
            >/dev/null 2>&1 &

        STRESS_PID=$!

        # Wait for load stabilization
        sleep "$STABLE_WAIT"

        # Run sysbench using all threads
        OUTPUT=$(sysbench cpu \
            --threads="$CPU_THREADS" \
            --time="$TEST_DURATION" \
            run 2>/dev/null)

        # Extract metrics
        EPS=$(echo "$OUTPUT" | awk '/events per second/ {print $4}')
        AVG=$(echo "$OUTPUT" | awk '/avg:/ {print $2; exit}')

        # Safety check
        if [ -z "$EPS" ] || [ -z "$AVG" ]; then
            echo "Failed to collect metrics for ${load}% load"
            wait "$STRESS_PID"
            continue
        fi

        # Composite score (higher is better)
        SCORE=$(echo "$EPS $AVG" | awk '{printf "%.2f", $1 / $2}')

        # Write CSV
        echo "$NOW,${load}%,$CPU_THREADS,$EPS,$AVG,$SCORE" >> "$LOG_FILE"

        # Console output
        echo "Threads            : $CPU_THREADS"
        echo "Events/sec         : $EPS"
        echo "Avg latency (ms)   : $AVG"
        echo "Composite score    : $SCORE"

        # Wait for stress to finish
        wait "$STRESS_PID"

        # Small gap between tests
        sleep 2
    done

    echo ""
    echo "Round completed. Waiting 10 minutes for next round..."
    echo ""

    sleep "$LOOP_INTERVAL"
done