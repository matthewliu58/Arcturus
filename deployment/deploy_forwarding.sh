#!/bin/bash
# ============================================================================
# Arcturus Forwarding Plane Deployment Script
# ============================================================================
# This script automatically deploys the forwarding plane (data plane)
# by reading configuration from deploy_config.toml
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

# Parse TOML array
parse_toml_array() {
    local file="$1"
    local section="$2"
    local key="$3"
    
    if [ ! -f "$file" ]; then
        log_error "Configuration file not found: $file"
    fi
    
    local value=$(awk -F= -v section="[$section]" -v key="$key" '
        $0 == section { in_section=1; next }
        /^\[/ { in_section=0 }
        in_section && $1 ~ "^"key"$" {
            gsub(/^[ \t]*|[ \t]*$/, "", $2)
            gsub(/^\[|\]$/, "", $2)
            gsub(/"/, "", $2)
            print $2
            exit
        }
    ' "$file")
    
    echo "$value"
}

# Clean up existing deployments
cleanup_existing_services() {
    log_step "Cleaning up existing Arcturus services"
    
    # Only clean up forwarding-related services, don't touch traefik
    local services=("arcturus-forwarding")
    
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
    
    # Remove old forwarding binary only
    log_info "Removing old binaries..."
    rm -f /usr/local/bin/arcturus-forwarding 2>/dev/null || true
    
    # Wait for system to fully release resources
    log_info "Waiting for system to release resources..."
    sleep 2
    
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

# Install Go
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

# Install etcd probing_client tools
install_etcd_client() {
    local etcd_version="$1"
    local etcd_arch="$2"
    
    log_step "Installing etcd Client Tools"
    
    if command_exists etcdctl; then
        log_info "etcdctl is already installed: $(etcdctl version | head -n1)"
        return 0
    fi
    
    log_info "Downloading etcd $etcd_version..."
    local etcd_tarball="etcd-${etcd_version}-${etcd_arch}.tar.gz"
    local etcd_url="https://github.com/etcd-io/etcd/releases/download/${etcd_version}/${etcd_tarball}"
    
    wget -q "$etcd_url" -O "/tmp/${etcd_tarball}" || log_error "Failed to download etcd"
    
    log_info "Installing etcdctl..."
    tar -xzf "/tmp/${etcd_tarball}" -C /tmp
    sudo mv "/tmp/etcd-${etcd_version}-${etcd_arch}/etcdctl" /usr/local/bin/
    sudo chmod +x /usr/local/bin/etcdctl
    
    rm -rf "/tmp/${etcd_tarball}" "/tmp/etcd-${etcd_version}-${etcd_arch}"
    
    log_info "etcdctl installed successfully"
}

# Generate forwarding configuration file
generate_forwarding_config() {
    local config_file="$1"
    local scheduling_ip="$2"
    local scheduling_port="$3"
    
    log_step "Generating Forwarding Configuration"
    
    log_info "Creating configuration file: $config_file"
    
    cat > "$config_file" <<EOF
# Arcturus Forwarding Configuration
# Auto-generated by deploy_forwarding.sh
# DO NOT EDIT MANUALLY - Edit deploy/deploy_config.toml instead

[metrics_processing]
# The IP address of the server where the Scheduling module is deployed
server_addr = "${scheduling_ip}:${scheduling_port}"
EOF
    
    log_info "Configuration file generated successfully"
}

# Build and install forwarding service
build_and_install_service() {
    local project_root="$1"
    
    log_step "Building Forwarding Service"
    
    cd "$project_root/forwarding/cmd"
    
    log_info "Building Go binary..."
    export PATH=$PATH:/usr/local/go/bin
    export GOPATH=$HOME/go
    /usr/local/go/bin/go build -o arcturus-forwarding main.go || log_error "Failed to build forwarding service"
    
    log_info "Installing binary to /usr/local/bin..."
    sudo mv arcturus-forwarding /usr/local/bin/
    sudo chmod +x /usr/local/bin/arcturus-forwarding
    
    # Create systemd service
    log_info "Creating systemd service..."
    sudo tee /etc/systemd/system/arcturus-forwarding.service > /dev/null <<EOF
[Unit]
Description=Arcturus Forwarding Plane
Documentation=https://github.com/Bootes2022/Arcturus
After=network.target
Wants=network.target

[Service]
Type=simple
User=root
WorkingDirectory=$project_root/forwarding/cmd
ExecStart=/usr/local/bin/arcturus-forwarding
Restart=on-failure
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=arcturus-forwarding

# Resource limits
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF
    
    sudo systemctl daemon-reload
    sudo systemctl enable arcturus-forwarding.service
    
    log_info "Service installed successfully"
}

# Configure firewall
configure_firewall() {
    local port_start="$1"
    local port_end="$2"
    
    log_step "Configuring Firewall"
    
    if command_exists firewall-cmd; then
        log_info "Configuring firewalld..."
        sudo firewall-cmd --permanent --add-port=${port_start}-${port_end}/tcp
        sudo firewall-cmd --reload
        log_info "Firewall rules added for ports $port_start-$port_end"
    elif command_exists ufw; then
        log_info "Configuring ufw..."
        sudo ufw allow ${port_start}:${port_end}/tcp
        log_info "Firewall rules added for ports $port_start-$port_end"
    else
        log_warn "No firewall detected. Please manually open ports $port_start-$port_end"
    fi
}

# Main deployment function
main() {
    log_step "Arcturus Forwarding Plane Deployment"
    
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
    ETCD_VERSION=$(parse_toml "$DEPLOY_CONFIG" "environment" "etcd_version")
    ETCD_ARCH=$(parse_toml "$DEPLOY_CONFIG" "environment" "etcd_arch")
    
    SCHEDULING_IP=$(parse_toml "$DEPLOY_CONFIG" "services" "scheduling_ip")
    SCHEDULING_METRICS_PORT=$(parse_toml "$DEPLOY_CONFIG" "services" "scheduling_metrics_port")
    
    PORT_START=$(parse_toml "$DEPLOY_CONFIG" "forwarding" "port_start")
    PORT_END=$(parse_toml "$DEPLOY_CONFIG" "forwarding" "port_end")
    
    # Install dependencies
    install_go "$GO_VERSION" "$GO_ARCH"
    install_etcd_client "$ETCD_VERSION" "$ETCD_ARCH"
    
    # Generate forwarding configuration
    generate_forwarding_config "$PROJECT_ROOT/forwarding/cmd/forwarding_config.toml" \
        "$SCHEDULING_IP" "$SCHEDULING_METRICS_PORT"
    
    # Build and install service
    build_and_install_service "$PROJECT_ROOT"
    
    # Configure firewall
    configure_firewall "$PORT_START" "$PORT_END"
    
    # Start service
    log_step "Starting Service"
    sudo systemctl start arcturus-forwarding.service
    sleep 3
    
    # Check service status
    if sudo systemctl is-active --quiet arcturus-forwarding.service; then
        log_info "✅ Forwarding plane deployed successfully!"
        echo ""
        echo "Service Status: $(sudo systemctl is-active arcturus-forwarding.service)"
        echo "Forwarding Ports: $PORT_START-$PORT_END"
        echo "Reporting to: ${SCHEDULING_IP}:${SCHEDULING_METRICS_PORT}"
        echo ""
        echo "Useful Commands:"
        echo "  - Check status: sudo systemctl status arcturus-forwarding"
        echo "  - View logs: sudo journalctl -u arcturus-forwarding -f"
        echo "  - Restart: sudo systemctl restart arcturus-forwarding"
    else
        log_error "Service failed to start. Check logs with: sudo journalctl -u arcturus-forwarding -xe"
    fi
}

# Run main function
main "$@"

