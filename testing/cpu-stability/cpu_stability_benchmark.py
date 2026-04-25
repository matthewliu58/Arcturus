# """
# File: cpu_stability_benchmark.py
# Purpose:
#     Measure CPU processing stability under fixed workload
#     (used in Arcturus evaluation)
# """

import time
import statistics
import hashlib

def cpu_task(n=200000):
    x = 0
    for i in range(n):
        x += (i * i) % 97
    hashlib.sha256(str(x).encode()).hexdigest()
    return x

def run_once():
    start = time.perf_counter()
    cpu_task()
    end = time.perf_counter()
    return (end - start) * 1000  # ms

def run_experiment(trials=100):

    for _ in range(10):
        run_once()

    results = []
    for _ in range(trials):
        results.append(run_once())

    mean_val = statistics.mean(results)
    print(mean_val)

if __name__ == "__main__":
    run_experiment()