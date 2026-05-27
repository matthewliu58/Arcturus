#!/usr/bin/env bash
set -e

# Script to set up SkyAccel services as systemd services

echo "==> SkyAccel Systemd Service Setup"
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
    
    local service_file="/etc/systemd/system/SkyAccel-$service_name.service"
    
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
SyslogIdentifier=SkyAccel-$service_name

[Install]
WantedBy=multi-user.target
EOF
    
    echo "Created $service_file"
}

# ===== Main script =====

# Check if we're in the correct directory
if [ ! -d "$CONTROL_PLANE_DIR" ] || [ ! -d "$DATA_PLANE_DIR" ] || [ ! -d "$DATA_PROXY_DIR" ]; then
    echo "Error: This script must be run from the SkyAccel project root directory"
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
create_service_file "control-plane" "SkyAccel Control Plane" "$CONTROL_PLANE_DIR" "$CONTROL_PLANE_DIR/control-plane"
create_service_file "data-plane" "SkyAccel Data Plane" "$DATA_PLANE_DIR" "$DATA_PLANE_DIR/data-plane"
create_service_file "data-proxy" "SkyAccel Data Proxy" "$DATA_PROXY_DIR" "$DATA_PROXY_DIR/data-proxy"

# Reload systemd configuration
echo_step "Reloading systemd configuration"
sudo systemctl daemon-reload

# Enable services
echo_step "Enabling services"
sudo systemctl enable SkyAccel-control-plane SkyAccel-data-plane SkyAccel-data-proxy

# Start services
echo_step "Starting services"
sudo systemctl start SkyAccel-control-plane SkyAccel-data-plane SkyAccel-data-proxy

# Check service status
echo_step "Checking service status"
sudo systemctl status SkyAccel-control-plane SkyAccel-data-plane SkyAccel-data-proxy

# ===== Summary =====
echo "\n=================================="
echo "SkyAccel Systemd Service Setup Complete!"
echo "=================================="
echo "Services have been registered as systemd services:"
echo "1. SkyAccel-control-plane"
echo "2. SkyAccel-data-plane"
echo "3. SkyAccel-data-proxy"
echo "\nKey features:"
echo "- Auto-start on system boot"
echo "- Auto-restart on failure"
echo "- Centralized logging via syslog"
echo "- Unified management via systemctl"
echo "\nManagement commands:"
echo "  sudo systemctl start|stop|restart SkyAccel-<service>"
echo "  sudo systemctl status SkyAccel-<service>"
echo "  sudo journalctl -u SkyAccel-<service>"  # View logs
 echo "=================================="
