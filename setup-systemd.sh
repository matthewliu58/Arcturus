#!/usr/bin/env bash
set -e

# Script to set up Arcturus services as systemd services

echo "==> Arcturus Systemd Service Setup"
echo "=================================="

# ===== Configuration =====
PROJECT_ROOT="$(pwd)"
CONTROL_PLANE_DIR="$PROJECT_ROOT/control-plane"
DATA_PLANE_DIR="$PROJECT_ROOT/data-plane"
DATA_PROXY_DIR="$PROJECT_ROOT/data-proxy"

# ===== Helper functions =====

echo_step() {
    echo "\n==> $1"
}

create_service_file() {
    local service_name=$1
    local service_description=$2
    local working_dir=$3
    local exec_start=$4
    
    local service_file="/etc/systemd/system/arcturus-$service_name.service"
    
    echo_step "Creating systemd service file for $service_name"
    
    cat << EOF | sudo tee "$service_file"
[Unit]
Description=$service_description
After=network.target

[Service]
Type=simple
WorkingDirectory=$working_dir
ExecStart=$exec_start
Restart=always
RestartSec=5
StandardOutput=syslog
StandardError=syslog
SyslogIdentifier=arcturus-$service_name

[Install]
WantedBy=multi-user.target
EOF
    
    echo "Created $service_file"
}

# ===== Main script =====

# Check if we're in the correct directory
if [ ! -d "$CONTROL_PLANE_DIR" ] || [ ! -d "$DATA_PLANE_DIR" ] || [ ! -d "$DATA_PROXY_DIR" ]; then
    echo "Error: This script must be run from the Arcturus project root directory"
    exit 1
fi

# Build services first
echo_step "Building services"

cd "$CONTROL_PLANE_DIR"
go build -o "control-plane" .
if [ $? -ne 0 ]; then
    echo "Error: Failed to build control-plane"
    exit 1
fi
cd ..

cd "$DATA_PLANE_DIR"
go build -o "data-plane" .
if [ $? -ne 0 ]; then
    echo "Error: Failed to build data-plane"
    exit 1
fi
cd ..

cd "$DATA_PROXY_DIR"
go build -o "data-proxy" .
if [ $? -ne 0 ]; then
    echo "Error: Failed to build data-proxy"
    exit 1
fi
cd ..

# Create systemd service files
create_service_file "control-plane" "Arcturus Control Plane" "$CONTROL_PLANE_DIR" "$CONTROL_PLANE_DIR/control-plane"
create_service_file "data-plane" "Arcturus Data Plane" "$DATA_PLANE_DIR" "$DATA_PLANE_DIR/data-plane"
create_service_file "data-proxy" "Arcturus Data Proxy" "$DATA_PROXY_DIR" "$DATA_PROXY_DIR/data-proxy"

# Reload systemd configuration
echo_step "Reloading systemd configuration"
sudo systemctl daemon-reload

# Enable services
echo_step "Enabling services"
sudo systemctl enable arcturus-control-plane arcturus-data-plane arcturus-data-proxy

# Start services
echo_step "Starting services"
sudo systemctl start arcturus-control-plane arcturus-data-plane arcturus-data-proxy

# Check service status
echo_step "Checking service status"
sudo systemctl status arcturus-control-plane arcturus-data-plane arcturus-data-proxy

# ===== Summary =====
echo "\n=================================="
echo "Arcturus Systemd Service Setup Complete!"
echo "=================================="
echo "Services have been registered as systemd services:"
echo "1. arcturus-control-plane"
echo "2. arcturus-data-plane"
echo "3. arcturus-data-proxy"
echo "\nKey features:"
echo "- Auto-start on system boot"
echo "- Auto-restart on failure"
echo "- Centralized logging via syslog"
echo "- Unified management via systemctl"
echo "\nManagement commands:"
echo "  sudo systemctl start|stop|restart arcturus-<service>"
echo "  sudo systemctl status arcturus-<service>"
echo "  sudo journalctl -u arcturus-<service>"  # View logs
 echo "=================================="
