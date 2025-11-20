#!/bin/bash
# ============================================================================
# Arcturus Traefik Gateway + Client Probe Deployment Script
# ============================================================================
# This script automatically deploys:
# 1. Traefik Gateway with weighted redirector plugin
# 2. Client Probe module for latency measurement
# ============================================================================

set -e  # Exit on error

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

log_step() {
    echo -e "\n${BLUE}==== $1 ====${NC}\n"
}

# Check if running as root
check_root() {
    if [ "$(id -u)" -ne 0 ]; then
        log_error "This script must be run as root. Please use sudo."
    fi
}

# Check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Parse TOML configuration file
parse_toml() {
    local file="$1"
    local section="$2"
    local key="$3"
    
    if [ ! -f "$file" ]; then
        log_error "Configuration file not found: $file"
    fi
    
    # Extract value from TOML file
    # Handle both [section] and [section.subsection] formats
    local value=$(awk -F= -v section="[$section]" -v key="$key" '
        # Skip comments
        /^[[:space:]]*#/ { next }
        # Match section header
        $0 == section { in_section=1; next }
        # Exit section on new section header
        /^\[/ { in_section=0 }
        # Extract key-value in current section
        in_section && $1 ~ "^[[:space:]]*"key"[[:space:]]*$" {
            # Remove leading/trailing whitespace
            gsub(/^[ \t]*|[ \t]*$/, "", $2)
            # Remove inline comments (everything after #)
            sub(/#.*$/, "", $2)
            # Remove trailing whitespace again after comment removal
            gsub(/[ \t]*$/, "", $2)
            # Remove quotes
            gsub(/^"|"$/, "", $2)
            print $2
            exit
        }
    ' "$file")
    
    echo "$value"
}

# Clean up existing deployments
cleanup_existing_services() {
    log_step "Cleaning up existing Arcturus services"
    
    # Only clean up traefik-related services, don't touch forwarding
    local services=("traefik" "traefik-client-probe")
    
    for service in "${services[@]}"; do
        if systemctl list-units --full --all | grep -q "$service.service"; then
            log_info "Stopping and disabling $service..."
            systemctl stop "$service" 2>/dev/null || true
            systemctl disable "$service" 2>/dev/null || true
            rm -f "/etc/systemd/system/$service.service" 2>/dev/null || true
        fi
    done
    
    # Reload systemd daemon
    systemctl daemon-reload
    
    # Remove old traefik binaries only
    log_info "Removing old binaries..."
    rm -f /usr/local/bin/traefik 2>/dev/null || true
    rm -f /usr/local/bin/traefik-client-probe 2>/dev/null || true
    
    # Remove old configuration directories (but keep logs)
    log_info "Removing old configurations..."
    rm -rf /etc/traefik 2>/dev/null || true
    
    # Wait for system to fully release resources
    log_info "Waiting for system to release resources..."
    sleep 3
    
    log_info "✅ Cleanup completed. Logs preserved."
}

# Detect OS
detect_os() {
    if [ -f /etc/os-release ]; then
        . /etc/os-release
        OS_FAMILY=$ID_LIKE
        OS_NAME=$ID
        OS_VERSION=$VERSION_ID
    else
        log_error "Cannot detect OS. /etc/os-release not found."
    fi
    log_info "Detected OS: $OS_NAME $OS_VERSION (Family: $OS_FAMILY)"
}

# Install Go (needed for probing_client probing_report)
install_go() {
    local go_version="$1"
    local go_arch="$2"
    
    log_step "Installing Go Environment"
    
    if command_exists go; then
        local current_version=$(go version | awk '{print $3}')
        log_info "Go is already installed: $current_version"
        return 0
    fi
    
    log_info "Downloading Go $go_version..."
    local go_tarball="go${go_version}.${go_arch}.tar.gz"
    local go_url="https://go.dev/dl/${go_tarball}"
    
    # Remove any existing corrupted file
    rm -f "/tmp/${go_tarball}"
    
    # Download with retries (quiet mode)
    wget -q --tries=3 --timeout=60 "$go_url" -O "/tmp/${go_tarball}" || log_error "Failed to download Go"
    
    # Verify the downloaded file is a valid gzip
    if ! file "/tmp/${go_tarball}" | grep -q "gzip compressed"; then
        log_error "Downloaded Go file is corrupted. Please check your network connection."
    fi
    
    log_info "Installing Go to /usr/local/go..."
    sudo rm -rf /usr/local/go
    sudo tar -C /usr/local -xzf "/tmp/${go_tarball}"
    rm "/tmp/${go_tarball}"
    
    # Add to PATH
    if ! grep -q "/usr/local/go/bin" /etc/profile; then
        echo 'export PATH=$PATH:/usr/local/go/bin' | sudo tee -a /etc/profile
    fi
    
    if ! grep -q "GOPATH" /etc/profile; then
        echo 'export GOPATH=$HOME/go' | sudo tee -a /etc/profile
        echo 'export PATH=$PATH:$GOPATH/bin' | sudo tee -a /etc/profile
    fi
    
    export PATH=$PATH:/usr/local/go/bin
    export GOPATH=$HOME/go
    
    log_info "Go installed successfully: $(go version)"
}

# Download and install Traefik
install_traefik() {
    local version="$1"
    
    log_step "Installing Traefik"
    
    if [ -f "/usr/local/bin/traefik" ]; then
        log_info "Traefik is already installed"
        return 0
    fi
    
    local arch=$(uname -m)
    local os=$(uname -s | tr '[:upper:]' '[:lower:]')
    
    # Convert architecture names
    if [ "$arch" == "x86_64" ]; then
        arch="amd64"
    elif [ "$arch" == "aarch64" ]; then
        arch="arm64"
    fi
    
    local download_url="https://github.com/traefik/traefik/releases/download/${version}/traefik_${version}_${os}_${arch}.tar.gz"
    
    log_info "Downloading Traefik ${version}..."
    wget -q "$download_url" -O "/tmp/traefik.tar.gz" || log_error "Failed to download Traefik"
    
    log_info "Installing Traefik..."
    tar -xzf "/tmp/traefik.tar.gz" -C /tmp
    sudo mv /tmp/traefik /usr/local/bin/
    sudo chmod +x /usr/local/bin/traefik
    rm /tmp/traefik.tar.gz
    
    log_info "Traefik installed successfully: $(traefik version --short 2>/dev/null || echo 'installed')"
}

# Install Traefik plugin
install_traefik_plugin() {
    local project_root="$1"
    local plugin_name="$2"
    
    log_step "Installing Traefik Plugin"
    
    local plugin_source_dir="$project_root/traefik/deployment/plugins-local/src/$plugin_name"
    local plugin_dest_dir="/plugins-local/src/$plugin_name"
    
    if [ ! -d "$plugin_source_dir" ]; then
        log_error "Plugin source directory not found: $plugin_source_dir"
    fi
    
    log_info "Installing plugin: $plugin_name"
    sudo mkdir -p "$plugin_dest_dir"
    sudo cp -r "$plugin_source_dir/"* "$plugin_dest_dir/"
    
    # Create plugin manifest if not exists
    if [ ! -f "$plugin_dest_dir/.traefik.yml" ]; then
        log_info "Creating plugin manifest..."
        sudo tee "$plugin_dest_dir/.traefik.yml" > /dev/null <<EOF
displayName: "Weighted Redirector"
type: "middleware"
summary: "A plugin for weighted HTTP redirection"
import: "$plugin_name"
testData:
  redirections:
    - weight: 50
      url: "http://example1.com"
    - weight: 50
      url: "http://example2.com"
EOF
    fi
    
    log_info "Plugin installed successfully"
}

# Configure Traefik
configure_traefik() {
    local scheduling_ip="$1"
    local scheduling_port="$2"
    local project_root="$3"
    
    log_step "Configuring Traefik"
    
    # Create structs directory
    sudo mkdir -p /etc/traefik/conf.d
    
    # Generate static configuration
    log_info "Creating static configuration..."
    sudo tee /etc/traefik/traefik.yml > /dev/null <<EOF
# Traefik Static Configuration
# Auto-generated by deploy_traefik.sh

api:
  dashboard: true
  insecure: true

entryPoints:
  web:
    address: ":80"
  traefik:
    address: ":8080"

providers:
  http:
    endpoint: "http://${scheduling_ip}:${scheduling_port}/traefik-dynamic-config"
    pollInterval: "10s"
    pollTimeout: "5s"

experimental:
  localPlugins:
    myWeightedRedirector:
      moduleName: "weightedredirector"

log:
  level: "INFO"
  format: "common"

global:
  checkNewVersion: true

serversTransport:
  maxIdleConnsPerHost: 200

tcpServersTransport:
  dialTimeout: "30s"
  dialKeepAlive: "15s"
EOF
    
    # Copy dynamic configuration
    log_info "Copying dynamic configuration..."
    if [ -d "$project_root/traefik/deployment/conf.d" ]; then
        sudo cp -r "$project_root/traefik/deployment/conf.d/"* /etc/traefik/conf.d/
    fi
    
    # Set permissions
    sudo chown -R root:root /etc/traefik
    sudo chmod -R 640 /etc/traefik
    sudo chmod 750 /etc/traefik/conf.d
    
    log_info "Traefik configuration completed"
}

# Create Traefik systemd service
create_traefik_service() {
    log_step "Creating Traefik Service"
    
    sudo tee /etc/systemd/system/traefik.service > /dev/null <<EOF
[Unit]
Description=Traefik Ingress Controller
Documentation=https://doc.traefik.io/traefik
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=/
ExecStart=/usr/local/bin/traefik --configFile=/etc/traefik/traefik.yml
Restart=on-failure
RestartSec=5
LimitNOFILE=65536
StandardOutput=journal
StandardError=journal
SyslogIdentifier=traefik

[Install]
WantedBy=multi-user.target
EOF
    
    sudo systemctl daemon-reload
    sudo systemctl enable traefik.service
    
    log_info "Traefik service created"
}

# Generate probing_client probing_report configuration
generate_client_probe_config() {
    local config_file="$1"
    local scheduling_ip="$2"
    local scheduling_port="$3"
    local probe_interval="$4"
    local probe_timeout="$5"
    local log_level="$6"
    
    log_step "Generating Client Probe Configuration"
    
    cat > "$config_file" <<EOF
# Arcturus Client Probe Configuration
# Auto-generated by deploy_traefik.sh

# Controller API endpoint
controller_addr = "http://${scheduling_ip}:${scheduling_port}"

# Probe settings
probe_interval = ${probe_interval}  # seconds
probe_timeout = ${probe_timeout}    # milliseconds

# Logging
log_level = "${log_level}"  # DEBUG, INFO, WARN, ERROR
EOF
    
    log_info "Client probe configuration generated"
}

# Build and install probing_client probing_report
build_and_install_client_probe() {
    local project_root="$1"
    
    log_step "Building Client Probe"
    
    cd "$project_root/traefik/client_probing"
    
    log_info "Building Go binary..."
    export PATH=$PATH:/usr/local/go/bin
    export GOPATH=$HOME/go
    /usr/local/go/bin/go build -o traefik-client-probe . || log_error "Failed to build client probe"
    
    log_info "Installing binary to /usr/local/bin..."
    sudo mv traefik-client-probe /usr/local/bin/
    sudo chmod +x /usr/local/bin/traefik-client-probe
    
    log_info "Client probe binary installed"
}

# Create probing_client probing_report systemd service
create_client_probe_service() {
    local project_root="$1"
    
    log_step "Creating Client Probe Service"
    
    sudo tee /etc/systemd/system/traefik-client-probe.service > /dev/null <<EOF
[Unit]
Description=Arcturus Traefik Client Probe
Documentation=https://github.com/Bootes2022/Arcturus
After=network.target
Wants=network.target

[Service]
Type=simple
User=root
WorkingDirectory=$project_root/traefik/client_probing
ExecStart=/usr/local/bin/traefik-client-probe -structs config.toml
Restart=on-failure
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=traefik-client-probe

[Install]
WantedBy=multi-user.target
EOF
    
    sudo systemctl daemon-reload
    sudo systemctl enable traefik-client-probe.service
    
    log_info "Client probe service created"
}

# Configure firewall
configure_firewall() {
    local http_port="$1"
    local dashboard_port="$2"
    
    log_step "Configuring Firewall"
    
    if command_exists firewall-cmd; then
        log_info "Configuring firewalld..."
        sudo firewall-cmd --permanent --add-port=${http_port}/tcp
        sudo firewall-cmd --permanent --add-port=${dashboard_port}/tcp
        sudo firewall-cmd --reload
        log_info "Firewall rules added for ports $http_port and $dashboard_port"
    elif command_exists ufw; then
        log_info "Configuring ufw..."
        sudo ufw allow ${http_port}/tcp
        sudo ufw allow ${dashboard_port}/tcp
        log_info "Firewall rules added for ports $http_port and $dashboard_port"
    else
        log_warn "No firewall detected. Please manually open ports $http_port and $dashboard_port"
    fi
}

# Main deployment function
main() {
    log_step "Arcturus Traefik Gateway + Client Probe Deployment"
    
    check_root
    
    # Clean up existing services first
    cleanup_existing_services
    
    # Determine script and project paths
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
    DEPLOY_CONFIG="$SCRIPT_DIR/deploy_config.toml"
    
    log_info "Script directory: $SCRIPT_DIR"
    log_info "Project root: $PROJECT_ROOT"
    log_info "Configuration file: $DEPLOY_CONFIG"
    
    if [ ! -f "$DEPLOY_CONFIG" ]; then
        log_error "Configuration file not found: $DEPLOY_CONFIG"
    fi
    
    # Read configuration
    log_info "Reading configuration..."
    GO_VERSION=$(parse_toml "$DEPLOY_CONFIG" "environment" "go_version")
    GO_ARCH=$(parse_toml "$DEPLOY_CONFIG" "environment" "go_arch")
    
    SCHEDULING_IP=$(parse_toml "$DEPLOY_CONFIG" "services" "scheduling_ip")
    SCHEDULING_TRAEFIK_PORT=$(parse_toml "$DEPLOY_CONFIG" "services" "scheduling_traefik_port")
    SCHEDULING_API_PORT=$(parse_toml "$DEPLOY_CONFIG" "services" "scheduling_api_port")
    
    TRAEFIK_VERSION=$(parse_toml "$DEPLOY_CONFIG" "traefik" "version")
    PLUGIN_NAME=$(parse_toml "$DEPLOY_CONFIG" "traefik" "plugin_name")
    HTTP_PORT=$(parse_toml "$DEPLOY_CONFIG" "traefik" "http_port")
    DASHBOARD_PORT=$(parse_toml "$DEPLOY_CONFIG" "traefik" "dashboard_port")
    
    PROBE_INTERVAL=$(parse_toml "$DEPLOY_CONFIG" "traefik.client_probe" "probe_interval")
    PROBE_TIMEOUT=$(parse_toml "$DEPLOY_CONFIG" "traefik.client_probe" "probe_timeout")
    LOG_LEVEL=$(parse_toml "$DEPLOY_CONFIG" "traefik.client_probe" "log_level")
    
    # Install dependencies
    install_go "$GO_VERSION" "$GO_ARCH"
    
    # Install and configure Traefik
    install_traefik "$TRAEFIK_VERSION"
    install_traefik_plugin "$PROJECT_ROOT" "$PLUGIN_NAME"
    configure_traefik "$SCHEDULING_IP" "$SCHEDULING_TRAEFIK_PORT" "$PROJECT_ROOT"
    create_traefik_service
    
    # Install and configure probing_client probing_report
    generate_client_probe_config "$PROJECT_ROOT/traefik/client_probing/config.toml" \
        "$SCHEDULING_IP" "$SCHEDULING_API_PORT" "$PROBE_INTERVAL" "$PROBE_TIMEOUT" "$LOG_LEVEL"
    build_and_install_client_probe "$PROJECT_ROOT"
    create_client_probe_service "$PROJECT_ROOT"
    
    # Configure firewall
    configure_firewall "$HTTP_PORT" "$DASHBOARD_PORT"
    
    # Start services
    log_step "Starting Services"
    
    log_info "Starting Traefik..."
    sudo systemctl start traefik.service
    sleep 3
    
    log_info "Starting Client Probe..."
    sudo systemctl start traefik-client-probe.service
    sleep 2
    
    # Check service status
    local traefik_status=$(sudo systemctl is-active traefik.service)
    local probe_status=$(sudo systemctl is-active traefik-client-probe.service)
    
    if [ "$traefik_status" == "active" ] && [ "$probe_status" == "active" ]; then
        log_info "✅ Traefik Gateway and Client Probe deployed successfully!"
        echo ""
        echo "=== Service Status ==="
        echo "Traefik: $traefik_status"
        echo "Client Probe: $probe_status"
        echo ""
        echo "=== Access Information ==="
        local server_ip=$(hostname -I | awk '{print $1}')
        echo "Dashboard: http://${server_ip}:${DASHBOARD_PORT}/dashboard/"
        echo "API: http://${server_ip}:${DASHBOARD_PORT}/api/rawdata"
        echo "HTTP Entry: http://${server_ip}:${HTTP_PORT}"
        echo ""
        echo "=== Configuration ==="
        echo "Scheduling Server: ${SCHEDULING_IP}:${SCHEDULING_TRAEFIK_PORT}"
        echo "Probe Interval: ${PROBE_INTERVAL}s"
        echo "Plugin: $PLUGIN_NAME"
        echo ""
        echo "=== Useful Commands ==="
        echo "Traefik:"
        echo "  - Check status: sudo systemctl status traefik"
        echo "  - View logs: sudo journalctl -u traefik -f"
        echo "  - Restart: sudo systemctl restart traefik"
        echo ""
        echo "Client Probe:"
        echo "  - Check status: sudo systemctl status traefik-client-probe"
        echo "  - View logs: sudo journalctl -u traefik-client-probe -f"
        echo "  - Restart: sudo systemctl restart traefik-client-probe"
    else
        log_error "Service failed to start. Check logs with: sudo journalctl -xe"
    fi
}

# Run main function
main "$@"

