import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt
import numpy as np
import math

# ============================================
# Configurable Parameters
# ============================================
cpuMid2core = 60             # 2-core CPU neutral point (%)
cpuMid4core = 80             # 4-core CPU neutral point (%)
delayGood = 20               # Delay neutral point (ms)

# Alpha sweep ranges (paired by index)
alpha_list_2core = [1.9, 2.0, 2.2]   # 2-core α
alpha_list_4core = [2.7, 3.0, 3.3]   # 4-core α (higher to compensate Mid=80%)

# Beta sweep range (3 values around 0.20)
beta_list = [0.15, 0.20, 0.25]

x_range = (0, 110)                             # extend to show 4-core 100%+
cpu_vals = np.linspace(*x_range, 200)
delay_vals = np.linspace(*x_range, 200)

# ============================================
# Grid: 4(alpha) x 3(beta) = 12 subplots
# ============================================
n_alpha = len(alpha_list_2core)
n_beta  = len(beta_list)
total   = n_alpha * n_beta

n_cols = 3
n_rows = (total + n_cols - 1) // n_cols

fig, axes = plt.subplots(n_rows, n_cols, figsize=(18, 5 * n_rows))
axes_flat = axes.flatten()

for idx in range(total):
    ai = idx // n_beta
    bi = idx % n_beta
    a2 = alpha_list_2core[ai]
    a4 = alpha_list_4core[ai]
    beta = beta_list[bi]

    ax = axes_flat[idx]

    # 2-core CPU (Mid=60%, α2)
    cpu_p_2core = np.exp(a2 * (cpu_vals / cpuMid2core - 1))
    # 4-core CPU (Mid=80%, α4)
    cpu_p_4core = np.exp(a4 * (cpu_vals / cpuMid4core - 1))
    # Delay
    delay_p = np.exp(beta * (delay_vals / delayGood - 1))

    # 2-core CPU: blue, triangle markers every 20%
    ax.plot(cpu_vals, cpu_p_2core, 'b-', linewidth=3.0, label=f'CPU 2c α={a2}')
    ax.plot(cpu_vals[::40], cpu_p_2core[::40], 'b^', markersize=6)
    # 4-core CPU: cyan, square markers every 20%
    ax.plot(cpu_vals, cpu_p_4core, 'c-', linewidth=3.0, label=f'CPU 4c α={a4}')
    ax.plot(cpu_vals[::40], cpu_p_4core[::40], 'cs', markersize=6)
    # Delay: red, circle markers every 20%
    ax.plot(delay_vals, delay_p, 'r--', linewidth=3.0, label=f'Delay β={beta}')
    ax.plot(delay_vals[::40], delay_p[::40], 'ro', markersize=6)

    ax.axhline(y=1.0, color='gray', linestyle=':', alpha=0.5)
    ax.axvline(x=cpuMid2core, color='gray', linestyle=':', alpha=0.2)
    ax.axvline(x=cpuMid4core, color='gray', linestyle=':', alpha=0.2)
    ax.legend(fontsize=15, loc='upper left')
    ax.grid(True, alpha=0.3)
    ax.set_xlim(0, 110)
    ax.set_ylim(0, 6.0)
    ax.set_title(f'2c α={a2}  4c α={a4}  β={beta}', fontsize=10, fontweight='bold')

# Hide unused subplots
for idx in range(total, len(axes_flat)):
    axes_flat[idx].set_visible(False)

fig.suptitle(f'Lyapunov Penalty Overlay: 2c α∈{alpha_list_2core} × 4c α∈{alpha_list_4core} × β∈{beta_list} ({total} combos)\n'
             f'Blue=2-core(Mid=60%)  Cyan=4-core(Mid=80%)  Red--=Delay(good=20ms)',
             fontsize=13, fontweight='bold', y=1.02)
plt.tight_layout()
plt.savefig('penalty_curves.png', dpi=150, bbox_inches='tight')
print("Saved: penalty_curves.png")

# ============================================
# Terminal Numeric Comparison Tables
# ============================================
print(f"\n{'='*100}")
print("CPU Penalty Table (2-core, Mid=60%)")
header = f"{'CPU%':>6}  " + "".join(f"{'α='+str(a):>10}" for a in alpha_list_2core)
print(header)
print(f"{'-'*len(header)}")
for cpu_pct in [20, 40, 60, 80, 100]:
    row = f"{cpu_pct:>5}%  " + "".join(f"{math.exp(a * (cpu_pct / cpuMid2core - 1)):>10.4f}" for a in alpha_list_2core)
    print(row)

print(f"\n{'='*100}")
print("CPU Penalty Table (4-core, Mid=80%)")
header = f"{'CPU%':>6}  " + "".join(f"{'α='+str(a):>10}" for a in alpha_list_4core)
print(header)
print(f"{'-'*len(header)}")
for cpu_pct in [40, 60, 80, 100, 110]:
    row = f"{cpu_pct:>5}%  " + "".join(f"{math.exp(a * (cpu_pct / cpuMid4core - 1)):>10.4f}" for a in alpha_list_4core)
    print(row)

print(f"\n{'='*100}")
print("Delay Penalty Table (good=20ms)")
header = f"{'Delay':>7}  " + "".join(f"{'β='+str(b):>10}" for b in beta_list)
print(header)
print(f"{'-'*len(header)}")
for delay_ms in [10, 20, 40, 60, 80, 100]:
    row = f"{delay_ms:>5}ms  " + "".join(f"{math.exp(b * (delay_ms / delayGood - 1)):>10.4f}" for b in beta_list)
    print(row)

print(f"\n{'='*100}")
print("Combination Match Summary  (2-core CPU% vs Delay ms)")
print(f"  {'α2':>5} {'β':>5} | {'60%<->20ms':>20} | {'80%<->60ms':>20} | {'100%<->80ms':>20}")
print(f"  {'-'*72}")
for a2, beta in [(a2, b) for a2 in alpha_list_2core for b in beta_list]:
    pair_60_20 = f"{math.exp(a2*(60/cpuMid2core-1)):.2f} <-> {math.exp(beta*(20/delayGood-1)):.2f}"
    pair_80_60 = f"{math.exp(a2*(80/cpuMid2core-1)):.2f} <-> {math.exp(beta*(60/delayGood-1)):.2f}"
    pair_100_80 = f"{math.exp(a2*(100/cpuMid2core-1)):.2f} <-> {math.exp(beta*(80/delayGood-1)):.2f}"
    print(f"  {a2:>5.1f} {beta:>5.2f} | {pair_60_20:>20} | {pair_80_60:>20} | {pair_100_80:>20}")

print(f"\n{'='*100}")
print("Combination Match Summary  (4-core CPU% vs Delay ms)")
print(f"  {'α4':>5} {'β':>5} | {'80%<->20ms':>20} | {'100%<->60ms':>20} | {'110%<->80ms':>20}")
print(f"  {'-'*72}")
for a4, beta in [(a4, b) for a4 in alpha_list_4core for b in beta_list]:
    pair_80_20  = f"{math.exp(a4*(80/cpuMid4core-1)):.2f} <-> {math.exp(beta*(20/delayGood-1)):.2f}"
    pair_100_60 = f"{math.exp(a4*(100/cpuMid4core-1)):.2f} <-> {math.exp(beta*(60/delayGood-1)):.2f}"
    pair_110_80 = f"{math.exp(a4*(110/cpuMid4core-1)):.2f} <-> {math.exp(beta*(80/delayGood-1)):.2f}"
    print(f"  {a4:>5.1f} {beta:>5.2f} | {pair_80_20:>20} | {pair_100_60:>20} | {pair_110_80:>20}")
