import time
import statistics
import hashlib

def cpu_task():
    x = 0
    # 合理计算量，运行时间 20~30ms
    for i in range(250000):
        x += (i * i) % 97
    hashlib.sha256(str(x).encode()).hexdigest()
    return x

def run_once():
    start = time.perf_counter()
    cpu_task()
    end = time.perf_counter()
    return (end - start) * 1000

def run_experiment(trials=50):
    # Warm-up
    for _ in range(5):
        run_once()

    results = []
    for _ in range(trials):
        results.append(run_once())

    mean_val = statistics.mean(results)
    std_val = statistics.stdev(results)
    print(f"{mean_val},{std_val}")

if __name__ == "__main__":
    run_experiment()