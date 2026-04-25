#!/bin/bash

LOG="results/cpu_benchmark_final.csv"
mkdir -p results

echo "time,round,load,mean_latency_ms,std_latency_ms" > $LOG

ROUND=1

while true; do
    echo "===== ROUND $ROUND ====="

    for load in 0 30 60 90; do
        echo "Testing load: $load%"

        if [ $load -eq 0 ]; then
            sleep 22 &
            PID=$!
        else
            stress-ng --cpu 2 --cpu-load $load --timeout 22s &
            PID=$!
        fi

        sleep 12

        # Run test
        RES=$(python3 cpu_stability_benchmark.py)
        MEAN=$(echo $RES | cut -d',' -f1)
        STD=$(echo $RES | cut -d',' -f2)

        TIME=$(date '+%Y-%m-%d %H:%M:%S')
        echo "$TIME,$ROUND,$load,$MEAN,$STD" >> $LOG

        kill $PID 2>/dev/null
        wait $PID 2>/dev/null
        sleep 5
    done

    ROUND=$((ROUND+1))
    sleep 1800
done