import numpy as np
import matplotlib.pyplot as plt

# Set font for English labels
plt.rcParams['font.sans-serif'] = ['DejaVu Sans', 'Arial']
plt.rcParams['axes.unicode_minus'] = False

# x-axis: normalized value [0, 1]
x = np.linspace(0, 1, 100)

# Two curves
y_cpu = x ** 2.0    # cpuPower = 2.0
y_lat = x ** 1.5    # latPower = 1.5

# Create figure
plt.figure(figsize=(10, 6))
plt.plot(x, y_cpu, 'b-', linewidth=2, label='CPU (power=2.0)')
plt.plot(x, y_lat, 'r-', linewidth=2, label='Latency (power=1.5)')
plt.plot(x, x, 'g--', linewidth=1, alpha=0.5, label='Linear (power=1.0)')

# Add reference lines
plt.axhline(y=0.5, color='gray', linestyle=':', alpha=0.5)
plt.axvline(x=0.5, color='gray', linestyle=':', alpha=0.5)

# Mark key points
plt.scatter([0.5], [0.5**2.0], color='blue', s=80, zorder=5)
plt.scatter([0.5], [0.5**1.5], color='red', s=80, zorder=5)
plt.annotate(f'CPU: {0.5**2.0:.3f}', xy=(0.5, 0.5**2.0), xytext=(0.55, 0.5**2.0),
             fontsize=10, color='blue')
plt.annotate(f'Lat: {0.5**1.5:.3f}', xy=(0.5, 0.5**1.5), xytext=(0.55, 0.5**1.5-0.08),
             fontsize=10, color='red')

# Figure decoration
plt.xlabel('Normalized Value (ratio)', fontsize=12)
plt.ylabel('Risk', fontsize=12)
plt.title('Risk Function Comparison: Power Curves', fontsize=14, fontweight='bold')
plt.legend(fontsize=11, loc='upper left')
plt.grid(True, alpha=0.3)
plt.xlim(0, 1)
plt.ylim(0, 1)

# Add description text
textstr = '\n'.join([
    'CPU (power=2.0): Steeper curve, risk grows faster at high values',
    'Latency (power=1.5): Flatter curve, growth between linear and quadratic',
    'Linear (power=1.0): Linear growth, as reference'
])
plt.text(0.02, 0.98, textstr, transform=plt.gca().transAxes,
         fontsize=10, verticalalignment='top',
         bbox=dict(boxstyle='round', facecolor='wheat', alpha=0.3))

plt.tight_layout()
plt.savefig('risk_curves.png', dpi=300, bbox_inches='tight')
print("Risk curves plot saved as risk_curves.png")
plt.show()