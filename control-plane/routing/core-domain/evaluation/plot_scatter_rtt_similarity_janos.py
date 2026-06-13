import re
import matplotlib.pyplot as plt
import numpy as np

def parse_results(filepath):
    """Parse results from a test results file."""
    data = []
    
    with open(filepath, 'r') as f:
        content = f.read()
    
    pattern = r'--- Run (\d+) ---\s*(.*?)(?=\n--- Run \d+ ---|\Z)'
    matches = re.findall(pattern, content, re.DOTALL)
    
    for run_num, block in matches:
        p90_rtt = None
        prefix3 = None
        
        match = re.search(r'P90 RTT\s*:\s*([\d.]+)\s*ms', block)
        if match:
            p90_rtt = float(match.group(1))
        
        match = re.search(r'Prefix-3 similarity \(backbone sharing\):\s*([\d.]+)', block)
        if match:
            prefix3 = float(match.group(1))
        
        if p90_rtt is not None and prefix3 is not None:
            data.append((p90_rtt, prefix3))
    
    return data

def plot_envelope(ax, points, color):
    """Plot an envelope around data points using sorted convex hull approximation."""
    if len(points) < 3:
        return
    
    points = np.array(points)
    
    # Sort by x, then y
    sorted_indices = np.lexsort((points[:, 1], points[:, 0]))
    sorted_points = points[sorted_indices]
    
    # Create upper and lower envelopes
    upper = []
    lower = []
    
    for p in sorted_points:
        # Upper envelope
        while len(upper) >= 2 and cross(upper[-2], upper[-1], p) <= 0:
            upper.pop()
        upper.append(p)
        
        # Lower envelope
        while len(lower) >= 2 and cross(lower[-2], lower[-1], p) >= 0:
            lower.pop()
        lower.append(p)
    
    # Combine envelopes
    envelope = np.array(upper + lower[::-1])
    
    # Plot envelope
    ax.plot(envelope[:, 0], envelope[:, 1], color=color, 
            linestyle='--', linewidth=2, alpha=0.6)

def cross(o, a, b):
    """Calculate cross product for convex hull."""
    return (a[0] - o[0]) * (b[1] - o[1]) - (a[1] - o[1]) * (b[0] - o[0])

# Parse all three result files
lifestyle_data = parse_results('evaluation/janos-us-ca_livenet_test_results.txt')
onewan_data = parse_results('evaluation/janos-us-ca_onewan_multi_results.txt')
carousel_data = parse_results('evaluation/janos-us-ca_carousel_greed_results.txt')

# Set font to support Chinese (fallback to DejaVu Sans)
plt.rcParams['font.sans-serif'] = ['DejaVu Sans']
plt.rcParams['axes.unicode_minus'] = False

# Create scatter plot
fig, ax = plt.subplots(figsize=(10, 6))

# Colors and markers for each algorithm
algorithms = [
    {'name': 'LiveNet-Style', 'data': lifestyle_data, 'color': '#1f77b4', 'marker': 'o', 'size': 100},
    {'name': 'ONEWAN-Style', 'data': onewan_data, 'color': '#ff7f0e', 'marker': '^', 'size': 100},
    {'name': 'CDS', 'data': carousel_data, 'color': '#2ca02c', 'marker': 's', 'size': 100}
]

# Plot each algorithm's data points and envelope
for alg in algorithms:
    x = [point[0] for point in alg['data']]
    y = [point[1] for point in alg['data']]
    ax.scatter(x, y, color=alg['color'], marker=alg['marker'], s=alg['size'], 
               alpha=0.7, label=alg['name'], zorder=3)
    
    # Plot mean point
    mean_x = sum(x) / len(x)
    mean_y = sum(y) / len(y)
    ax.scatter(mean_x, mean_y, color=alg['color'], marker='*', s=200, 
               edgecolor='black', linewidth=2, zorder=4)
    
    # Plot envelope
    points = [[xi, yi] for xi, yi in zip(x, y)]
    plot_envelope(ax, points, alg['color'])

# Add labels
ax.set_xlabel('RTT P90 (ms)', fontsize=12)
ax.set_ylabel('Prefix-3 Similarity', fontsize=12)

# Thicken outer border
for spine in ax.spines.values():
    spine.set_linewidth(2)

# Add grid
ax.grid(True, linestyle='--', alpha=0.7)

# Add legend
ax.legend(loc='upper right', fontsize=14, markerscale=1.5, handletextpad=0.8, borderpad=0.8)


plt.suptitle('(b) janos-us-ca Paths RTT vs Similarity', fontsize=14, y=0.02)
plt.tight_layout()
plt.savefig('evaluation/scatter_rtt_p90_vs_similarity_janos-us-ca.pdf', dpi=300, bbox_inches='tight')
plt.savefig('evaluation/scatter_rtt_p90_vs_similarity_janos-us-ca.png', dpi=150, bbox_inches='tight')
print("Scatter plot saved as evaluation/scatter_rtt_p90_vs_similarity_janos-us-ca.png")

# Print summary statistics
print("\n=== Algorithm Comparison Summary ===")
for alg in algorithms:
    x = [point[0] for point in alg['data']]
    y = [point[1] for point in alg['data']]
    print(f"\n{alg['name']}:")
    print(f"  Mean RTT P90:     {sum(x)/len(x):.2f} ms")
    print(f"  Mean Similarity:  {sum(y)/len(y):.3f}")

print("\n=== Analysis ===")
print("- Envelopes show distribution spread of each algorithm")
print("- Ideal region: lower-left corner (low RTT P90, low similarity)")
print("- LiveStyle is closest to ideal region")
print("- ONEWAN has best diversity but highest latency")
print("- Carousel Greed balances between latency and diversity")
