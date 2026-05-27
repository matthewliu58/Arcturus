#!/usr/bin/env bash
set -e

# Network and kernel optimization script for SkyAccel acceleration project

echo "==> SkyAccel Network Optimization Script"
echo "======================================"

# ===== Network performance optimization =====
echo "\n==> Optimizing network kernel parameters"

# Check if SkyAccel network configuration already exists
if ! grep -q "# Network performance optimization for SkyAccel acceleration project" /etc/sysctl.conf; then
    cat << EOF | sudo tee -a /etc/sysctl.conf

# Network performance optimization for SkyAccel acceleration project
net.core.netdev_max_backlog = 65536
net.core.somaxconn = 65536
net.core.rmem_max = 16777216
net.core.wmem_max = 16777216
net.ipv4.tcp_max_syn_backlog = 65536
net.ipv4.tcp_slow_start_after_idle = 0
net.ipv4.tcp_tw_reuse = 1
net.ipv4.tcp_fin_timeout = 15
net.ipv4.tcp_keepalive_time = 300
net.ipv4.tcp_keepalive_probes = 3
net.ipv4.tcp_keepalive_intvl = 15
net.ipv4.tcp_fastopen = 3
net.ipv4.tcp_no_metrics_save = 1
net.ipv4.tcp_synack_retries = 2
net.ipv4.tcp_abort_on_overflow = 1
net.ipv4.ip_local_port_range = 1024 65535
EOF
fi

# Apply sysctl settings
sudo sysctl -p

# ===== BBR congestion control =====
echo "\n==> Enabling BBR congestion control"

sudo modprobe tcp_bbr
if ! grep -q "# Enable BBR congestion control" /etc/sysctl.conf; then
    cat << EOF | sudo tee -a /etc/sysctl.conf

# Enable BBR congestion control
net.ipv4.tcp_congestion_control = bbr
net.ipv4.tcp_notsent_lowat = 16384
EOF
fi

sudo sysctl -p

# ===== System resource optimization =====
echo "\n==> Optimizing system resources"

# Increase file descriptor limits
if ! grep -q "# Increase file descriptor limits for network applications" /etc/security/limits.conf; then
    cat << EOF | sudo tee -a /etc/security/limits.conf

# Increase file descriptor limits for network applications
* soft nofile 65536
* hard nofile 65536
EOF
fi

# Optimize memory management
if ! grep -q "# Memory management optimization" /etc/sysctl.conf; then
    cat << EOF | sudo tee -a /etc/sysctl.conf

# Memory management optimization
vm.swappiness = 10
vm.max_map_count = 262144
EOF
fi

sudo sysctl -p

# ===== Hardware acceleration =====
echo "\n==> Enabling hardware acceleration"

# Check for and enable hardware acceleration
sudo apt install -y ethtool

# Enable hardware checksum offloading
for nic in $(ls /sys/class/net/ | grep -v lo); do
    echo "Optimizing network interface: $nic"
    # Enable checksum offloading (ignore errors)
    sudo ethtool -K $nic rx on tx on tso on gso on gro on lro off 2>/dev/null || true
    # Set ring buffer size (use conservative values)
    sudo ethtool -G $nic rx 2048 tx 2048 2>/dev/null || true
    # Disable interrupt coalescing (ignore errors)
    sudo ethtool -A $nic rx off tx off 2>/dev/null || true
    # Set coalescing parameters (ignore errors)
    sudo ethtool --coalesce $nic rx-usecs 100 rx-frames 100 tx-usecs 100 tx-frames 100 2>/dev/null || true
    # Enable adaptive coalescing (ignore errors)
    sudo ethtool -C $nic adaptive-rx on adaptive-tx on 2>/dev/null || true
    # Only set speed if supported
    if sudo ethtool $nic | grep -q "Speed: 1000Mb/s"; then
        sudo ethtool -s $nic speed 1000 duplex full autoneg off 2>/dev/null || true
    fi
    echo "Optimization applied to $nic"
done

# ===== Compilation optimization =====
echo "\n==> Setting up compilation optimizations"

BASHRC="$HOME/.bashrc"

# Add compilation flags to bashrc
if ! grep -q "GOFLAGS" "$BASHRC"; then
cat << EOF >> "$BASHRC"

# Go compilation optimizations for SkyAccel
export GOFLAGS="-ldflags=-s -ldflags=-w"
export CGO_CFLAGS="-O3 -march=native"
export CGO_CXXFLAGS="-O3 -march=native"
export CGO_LDFLAGS="-O3"
EOF
fi

# ===== Network stack optimization =====
echo "\n==> Optimizing network stack"

# Enable jumbo frames (if supported)
for nic in $(ls /sys/class/net/ | grep -v lo); do
    if sudo ethtool -g $nic | grep -q "Jumbo":; then
        echo "Enabling jumbo frames on $nic"
        sudo ip link set $nic mtu 9000 2>/dev/null || true
    fi
done

# ===== Verification =====
echo "\n==> Verifying optimizations"

# Check BBR is enabled
echo "BBR status: $(cat /proc/sys/net/ipv4/tcp_congestion_control)"

# Check file descriptor limits
echo "File descriptor limits:"
ulimit -n

# Check network parameters
echo "\nKey network parameters:"
sysctl net.core.somaxconn net.ipv4.tcp_max_syn_backlog net.ipv4.tcp_fastopen

# ===== Cleanup =====
echo "\n==> Cleanup"

# Remove temporary files
sudo rm -f /tmp/*.tmp

# ===== Summary =====
echo "\n======================================"
echo "SkyAccel Network Optimization Complete!"
echo "======================================"
echo "The following optimizations have been applied:"
echo "1. Network kernel parameters tuned for high performance"
echo "2. BBR congestion control enabled"
echo "3. System resources optimized (file descriptors, memory)"
echo "4. Hardware acceleration enabled"
echo "5. Compilation optimizations configured"
echo "6. Network stack optimized"
echo "\nPlease reboot the system for all changes to take full effect."
echo "======================================"