import re
import sys
import numpy as np
from collections import Counter
from itertools import combinations

# =============================
# 1. Load log
# =============================
if len(sys.argv) > 1:
    log_file = sys.argv[1]
else:
    log_file = "cost266_carousel_greed_test.log"

# Path pattern for Carousel Greed algorithm
path_pattern = re.compile(
    r'level=DEBUG msg=Path pre=TEST index=\d+ hops="(?P<path>[^"]+)" rtt=\d+ rawRtt=(?P<rtt>\d+)'
)
edge_pattern = re.compile(
    r'level=DEBUG msg="Edge created" source=(?P<src>\S+) target=(?P<tgt>\S+) rawRTT=\d+ cpuUtil=(?P<cpu>\d+)'
)

selected_paths = []
edge_cpu = {}

with open(log_file) as f:
    for line in f:
        # selected path
        m = path_pattern.search(line)
        if m:
            selected_paths.append({
                "rawRTT": int(m.group("rtt")),
                "path": m.group("path").split(" -> ")
            })
        # edge info
        e = edge_pattern.search(line)
        if e:
            key = tuple(sorted([e.group("src"), e.group("tgt")]))  # undirected edge
            edge_cpu[key] = int(e.group("cpu"))

# =============================
# 2. Latency
# =============================
rtts = [p["rawRTT"] for p in selected_paths]
print("===== Latency =====")
print(f"Mean RTT  : {np.mean(rtts):.2f} ms")
print(f"Median RTT: {np.median(rtts):.2f} ms")
print(f"P90 RTT   : {np.percentile(rtts, 90):.2f} ms\n")

# =============================
# 3. Edge usage
# =============================
edge_usage = Counter()
for p in selected_paths:
    path = p["path"]
    for i in range(len(path)-1):
        e = tuple(sorted([path[i], path[i+1]]))  # undirected edge
        edge_usage[e] += 1

# =============================
# 4. Hot-edge Risk (HAR)
# =============================
# Exponential penalty for CPU > 60%, more penalty for repeated edges
HAR_list = []
for p in selected_paths:
    path = p["path"]
    har = 1.0
    for i in range(len(path)-1):
        e = tuple(sorted([path[i], path[i+1]]))
        cpu = edge_cpu.get(e, 0)
        usage_count = edge_usage[e]
        if cpu > 60:
            har *= np.exp((cpu - 60)/10) * (1 + (usage_count-1)/5)  # exponential penalty + repetition
        else:
            har *= 1 + (usage_count-1)/10  # small penalty for repetition
    HAR_list.append(har)

HAR_array = np.array(HAR_list)
print("===== Hot-edge Risk (HAR) =====")
for i, val in enumerate(HAR_list[:10], 1):
    print(f"Path {i} HAR : {val:.3f}")
print(f"Overall HAR mean  : {HAR_array.mean():.3f}")
print(f"Overall HAR median: {np.median(HAR_array):.3f}")
print(f"Overall HAR P90   : {np.percentile(HAR_array,90):.3f}\n")

# =============================
# 5. Path Structure
# =============================
prefix_k = 3
prefix_sim_list = []
for p1, p2 in combinations(selected_paths, 2):
    prefix_sim_list.append(len(set(p1["path"][:prefix_k]) & set(p2["path"][:prefix_k])) / prefix_k)
print("===== Path Structure =====")
print(f"Prefix-{prefix_k} similarity (backbone sharing): {np.mean(prefix_sim_list):.3f}\n")

# =============================
# 6. Top used edges
# =============================
used_edges_with_cpu = [(e, count, edge_cpu.get(e, 0)) for e, count in edge_usage.items()]
top_edges = sorted(used_edges_with_cpu, key=lambda x: x[1], reverse=True)[:5]

print("===== Top used edges =====")
for e, count, cpu in top_edges:
    print(f"{e[0]}<->{e[1]} | usage={count} | CPU={cpu}%")