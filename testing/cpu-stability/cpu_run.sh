#!/bin/bash
mkdir -p results
LOG="results/cpu_benchmark_all.csv"

echo "time,round,load,mean_latency_ms" > "$LOG"

ROUND=1

while true; do
    echo "=== ROUND $ROUND ==="

    for load in 0 30 60 90; do
        echo "Load: $load%"

        stress-ng --cpu 4 --cpu-load $load --timeout 300s &
        PID=$!
        sleep 10

        VAL=$(python3 cpu_stability_benchmark.py)

        TIME=$(date "+%Y-%m-%d %H:%M:%S")
        echo "$TIME,$ROUND,$load,$VAL" >> "$LOG"

        kill $PID 2>/dev/null
        wait $PID 2>/dev/null
        sleep 10
    done

    ROUND=$((ROUND+1))
    echo "Waiting 1 hour..."
    sleep 3600
done

#chmod +x cpu_run.sh
#nohup ./cpu_run.sh &