#!/bin/bash
# ============================================================================
# Arcturus Scheduling Plane Deployment Script
# ============================================================================
# This script automatically deploys the scheduling plane (control plane)
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
# This is a simple TOML parser for basic key-value pairs
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
            gsub(/^[ \t]*|[ \t]*$/, "", $2)
            gsub(/^"|"$/, "", $2)
            print $2
            exit
        }
    ' "$file")
    
    echo "$value"
}

# Parse TOML array (for etcd_endpoints)
parse_toml_array() {
    local file="$1"
    local section="$2"
    local key="$3"
    
    if [ ! -f "$file" ]; then
        log_error "Configuration file not found: $file"
    fi
    
    # Extract array values
    local value=$(awk -F= -v section="[$section]" -v key="$key" '
        # Skip comments
        /^[[:space:]]*#/ { next }
        # Match section header
        $0 == section { in_section=1; next }
        # Exit section on new section header
        /^\[/ { in_section=0 }
        # Extract array value in current section
        in_section && $1 ~ "^[[:space:]]*"key"[[:space:]]*$" {
            gsub(/^[ \t]*|[ \t]*$/, "", $2)
            gsub(/^\[|\]$/, "", $2)
            gsub(/"/, "", $2)
            print $2
            exit
        }
    ' "$file")
    
    echo "$value"
}

# Check if a role should be included in node_region table
# Now includes all nodes regardless of role
should_include_in_node_region() {
    local role="$1"
    
    # Always include all nodes
    return 0  # Include
}

# Clean up existing deployments
cleanup_existing_services() {
    log_step "Cleaning up existing Arcturus services"
    
    # Stop and disable all possible Arcturus services
    local services=("arcturus-scheduling" "arcturus-forwarding" "traefik" "traefik-client-probe")
    
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
    
    # Remove old binaries
    log_info "Removing old binaries..."
    rm -f /usr/local/bin/arcturus-scheduling 2>/dev/null || true
    rm -f /usr/local/bin/arcturus-forwarding 2>/dev/null || true
    rm -f /usr/local/bin/traefik 2>/dev/null || true
    rm -f /usr/local/bin/client-probe 2>/dev/null || true
    
    # Remove old configuration directories (but keep logs)
    log_info "Removing old configurations..."
    rm -rf /etc/traefik 2>/dev/null || true
    rm -rf /etc/client-probe 2>/dev/null || true
    
    # Wait for system to fully release resources
    log_info "Waiting for system to release resources..."
    sleep 3
    
    # Keep database and logs, only clean up binaries and services
    log_info "✅ Cleanup completed. Database and logs preserved."
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

# Install etcd
install_etcd() {
    local etcd_version="$1"
    local etcd_arch="$2"
    local node_name="$3"
    local initial_advertise_peer_urls="$4"
    local listen_peer_urls="$5"
    local advertise_client_urls="$6"
    local listen_client_urls="$7"
    local initial_cluster="$8"
    
    log_step "Installing etcd"
    
    if command_exists etcd; then
        log_info "etcd is already installed: $(etcd --version | head -n1)"
        return 0
    fi
    
    log_info "Downloading etcd $etcd_version..."
    local etcd_tarball="etcd-${etcd_version}-${etcd_arch}.tar.gz"
    local etcd_url="https://github.com/etcd-io/etcd/releases/download/${etcd_version}/${etcd_tarball}"
    
    wget -q "$etcd_url" -O "/tmp/${etcd_tarball}" || log_error "Failed to download etcd"
    
    log_info "Installing etcd..."
    sudo mkdir -p /usr/local/etcd
    tar -xzf "/tmp/${etcd_tarball}" -C /tmp
    sudo mv "/tmp/etcd-${etcd_version}-${etcd_arch}/etcd" /usr/local/bin/
    sudo mv "/tmp/etcd-${etcd_version}-${etcd_arch}/etcdctl" /usr/local/bin/
    sudo mv "/tmp/etcd-${etcd_version}-${etcd_arch}/etcdutl" /usr/local/bin/
    sudo chmod +x /usr/local/bin/etcd /usr/local/bin/etcdctl /usr/local/bin/etcdutl
    
    rm -rf "/tmp/${etcd_tarball}" "/tmp/etcd-${etcd_version}-${etcd_arch}"
    
    # Create etcd data directory
    sudo mkdir -p /var/lib/etcd
    
    # Create systemd service
    log_info "Creating etcd systemd service..."
    sudo tee /etc/systemd/system/etcd.service > /dev/null <<EOF
[Unit]
Description=etcd distributed key-value store
Documentation=https://github.com/etcd-io/etcd
After=network.target

[Service]
Type=notify
ExecStart=/usr/local/bin/etcd \\
  --data-dir=/var/lib/etcd \\
  --name=${node_name} \\
  --initial-advertise-peer-urls=${initial_advertise_peer_urls} \\
  --listen-peer-urls=${listen_peer_urls} \\
  --advertise-client-urls=${advertise_client_urls} \\
  --listen-client-urls=${listen_client_urls} \\
  --initial-cluster=${initial_cluster}
Restart=on-failure
RestartSec=10
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF
    
    sudo systemctl daemon-reload
    sudo systemctl enable etcd.service
    sudo systemctl start etcd.service
    
    log_info "etcd installed and started successfully"
}

# Install MySQL
install_mysql() {
    local mysql_root_password="$1"
    
    log_step "Installing MySQL"
    
    if command_exists mysql; then
        log_info "MySQL is already installed: $(mysql --version)"
        return 0
    fi
    
    detect_os
    
    if [[ "$OS_FAMILY" == *"debian"* ]]; then
        log_info "Installing MySQL on Debian-based system..."
        sudo apt-get update -qq
        sudo DEBIAN_FRONTEND=noninteractive apt-get install -y mysql-server
        MYSQL_SERVICE="mysql"
    elif [[ "$OS_FAMILY" == *"rhel"* ]] || [[ "$OS_FAMILY" == *"fedora"* ]]; then
        log_info "Installing MySQL on RHEL-based system..."
        
        # Determine RHEL version
        local rhel_version=$(echo "$OS_VERSION" | cut -d. -f1)
        [ -z "$rhel_version" ] && rhel_version="8"
        
        # Remove old repo packages
        sudo yum remove -y 'mysql*-community-release*' 2>/dev/null || true
        
        # Install MySQL repo
        local mysql_repo_url="https://dev.mysql.com/get/mysql80-community-release-el${rhel_version}.rpm"
        sudo yum install -y "$mysql_repo_url"
        
        # Disable default mysql module if using DNF
        if command_exists dnf; then
            sudo dnf module reset -y mysql 2>/dev/null || true
            sudo dnf module disable -y mysql 2>/dev/null || true
        fi
        
        sudo yum install -y mysql-community-server
        MYSQL_SERVICE="mysqld"
    else
        log_error "Unsupported OS for MySQL installation: $OS_NAME"
    fi
    
    # Start MySQL service
    sudo systemctl start "$MYSQL_SERVICE"
    sudo systemctl enable "$MYSQL_SERVICE"
    
    log_info "MySQL installed successfully"
    
    # Set root password for RHEL-based systems
    if [[ "$OS_FAMILY" == *"rhel"* ]] || [[ "$OS_FAMILY" == *"fedora"* ]]; then
        log_info "Configuring MySQL root password..."
        sleep 5  # Wait for MySQL to fully start
        
        if sudo grep -q 'temporary password' /var/log/mysqld.log 2>/dev/null; then
            local temp_password=$(sudo grep 'temporary password' /var/log/mysqld.log | awk '{print $NF}' | tail -n 1)
            if [ -n "$temp_password" ]; then
                mysql --connect-expired-password -uroot -p"$temp_password" <<EOF
ALTER USER 'root'@'localhost' IDENTIFIED BY '$mysql_root_password';
FLUSH PRIVILEGES;
EOF
                log_info "MySQL root password set successfully"
            fi
        fi
    fi
}

# Configure MySQL database and user
configure_mysql() {
    local mysql_root_password="$1"
    local db_name="$2"
    local db_user="$3"
    local db_password="$4"
    local sql_script="$5"
    
    log_step "Configuring MySQL Database"
    
    detect_os
    
    # Determine MySQL connection method
    local MYSQL_CMD
    if [[ "$OS_FAMILY" == *"debian"* ]]; then
        MYSQL_CMD="sudo mysql -u root"
    else
        MYSQL_CMD="mysql -u root -p${mysql_root_password}"
    fi
    
    # Create database
    log_info "Creating database '$db_name'..."
    $MYSQL_CMD <<EOF
CREATE DATABASE IF NOT EXISTS \`$db_name\` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
EOF
    
    # Create user
    log_info "Creating user '$db_user'..."
    local user_exists=$($MYSQL_CMD -sN -e "SELECT COUNT(*) FROM mysql.user WHERE user='$db_user' AND host='localhost';" 2>/dev/null || echo "0")
    
    if [ "$user_exists" -eq 0 ]; then
        $MYSQL_CMD <<EOF
CREATE USER '$db_user'@'localhost' IDENTIFIED WITH mysql_native_password BY '$db_password';
GRANT ALL PRIVILEGES ON \`$db_name\`.* TO '$db_user'@'localhost';
FLUSH PRIVILEGES;
EOF
        log_info "User '$db_user' created successfully"
    else
        log_info "User '$db_user' already exists, updating password..."
        $MYSQL_CMD <<EOF
ALTER USER '$db_user'@'localhost' IDENTIFIED WITH mysql_native_password BY '$db_password';
GRANT ALL PRIVILEGES ON \`$db_name\`.* TO '$db_user'@'localhost';
FLUSH PRIVILEGES;
EOF
    fi
    
    # Execute SQL script
    if [ -f "$sql_script" ]; then
        log_info "Executing SQL script: $sql_script"
        $MYSQL_CMD "$db_name" < "$sql_script"
        log_info "Database tables created successfully"
    else
        log_warn "SQL script not found: $sql_script"
    fi
}

# Generate scheduling configuration file
generate_scheduling_config() {
    local config_file="$1"
    local db_user="$2"
    local db_password="$3"
    local db_name="$4"
    
    log_step "Generating Scheduling Configuration"
    
    log_info "Reading domain and node configurations from deploy_config.toml..."
    
    # Start building the structs file
    cat > "$config_file" <<EOF
# Arcturus Scheduling Configuration
# Auto-generated by deploy_scheduling.sh
# DO NOT EDIT MANUALLY - Edit deploy/deploy_config.toml instead

[database]
username = "$db_user"
password = "$db_password"
dbname = "$db_name"

EOF
    
    # Parse and add domain origins
    log_info "Adding domain configurations..."
    local in_domains=0
    while IFS= read -r line; do
        # Skip comments and empty lines
        [[ "$line" =~ ^[[:space:]]*# ]] && continue
        [[ -z "$line" ]] && continue
        
        if [[ "$line" == "[[domains]]" ]]; then
            in_domains=1
            echo "[[domain_origins]]" >> "$config_file"
        elif [[ "$line" =~ ^\[\[ ]] && [[ "$line" != "[[domains]]" ]]; then
            in_domains=0
        elif [ $in_domains -eq 1 ]; then
            if [[ "$line" =~ ^[[:space:]]*domain[[:space:]]*= ]]; then
                echo "$line" >> "$config_file"
            elif [[ "$line" =~ ^[[:space:]]*origin_ip[[:space:]]*= ]]; then
                echo "$line" >> "$config_file"
                echo "" >> "$config_file"
            fi
        fi
    done < "$DEPLOY_CONFIG"
    
    # Parse and add domain configurations for BPR
    in_domains=0
    local current_domain=""
    local current_redistribution=""
    local in_region_increment=0
    local region_increment_lines=""
    
    while IFS= read -r line; do
        # Skip comments and empty lines
        [[ "$line" =~ ^[[:space:]]*# ]] && continue
        [[ -z "$line" ]] && continue
        
        if [[ "$line" == "[[domains]]" ]]; then
            # Before starting a new domain, close the previous one if it exists
            if [ $in_domains -eq 1 ] && [ -n "$current_redistribution" ]; then
                echo "RedistributionProportion = $current_redistribution" >> "$config_file"
                echo "" >> "$config_file"
            fi
            
            in_domains=1
            current_redistribution=""
            echo "[[domain_config]]" >> "$config_file"
        elif [[ "$line" =~ ^\[\[ ]] && [[ "$line" != "[[domains]]" ]]; then
            # End of domains section, close last domain
            if [ $in_domains -eq 1 ] && [ -n "$current_redistribution" ]; then
                echo "RedistributionProportion = $current_redistribution" >> "$config_file"
                echo "" >> "$config_file"
            fi
            in_domains=0
            in_region_increment=0
        elif [ $in_domains -eq 1 ]; then
            if [[ "$line" =~ ^[[:space:]]*domain[[:space:]]*= ]]; then
                current_domain=$(echo "$line" | cut -d= -f2 | tr -d ' "')
                echo "DomainName = \"$current_domain\"" >> "$config_file"
            elif [[ "$line" =~ ^[[:space:]]*redistribution_proportion[[:space:]]*= ]]; then
                # Capture the redistribution_proportion value for this domain
                current_redistribution=$(echo "$line" | cut -d= -f2 | tr -d ' ')
            elif [[ "$line" =~ ^[[:space:]]*region_req_increment[[:space:]]*= ]]; then
                # Start of region_req_increment block - collect all lines
                in_region_increment=1
                region_increment_lines="$line"
            elif [ $in_region_increment -eq 1 ]; then
                # Collect lines into single line
                region_increment_lines="$region_increment_lines $line"
                # Check if this is the closing brace
                if [[ "$line" =~ ^[[:space:]]*}[[:space:]]*$ ]]; then
                    in_region_increment=0
                    # Convert to single line format (remove newlines and extra spaces)
                    single_line=$(echo "$region_increment_lines" | tr '\n' ' ' | sed 's/  */ /g' | sed 's/ = /=/g' | sed 's/ }/}/g' | sed 's/{ /{/g')
                    echo "$single_line" >> "$config_file"
                fi
            fi
        fi
    done < "$DEPLOY_CONFIG"
    
    # Handle the last domain if file ends while in_domains=1
    if [ $in_domains -eq 1 ] && [ -n "$current_redistribution" ]; then
        echo "RedistributionProportion = $current_redistribution" >> "$config_file"
        echo "" >> "$config_file"
    fi
    
    # Parse and add BPR scheduling tasks
    local in_bpr=0
    while IFS= read -r line; do
        # Skip comments and empty lines
        [[ "$line" =~ ^[[:space:]]*# ]] && continue
        [[ -z "$line" ]] && continue
        
        if [[ "$line" == "[[bpr_scheduling_tasks]]" ]]; then
            in_bpr=1
            echo "[[bpr_scheduling_task]]" >> "$config_file"
        elif [[ "$line" =~ ^\[\[ ]] && [[ "$line" != "[[bpr_scheduling_tasks]]" ]]; then
            in_bpr=0
        elif [ $in_bpr -eq 1 ]; then
            if [[ "$line" =~ ^[[:space:]]*region[[:space:]]*= ]]; then
                local value=$(echo "$line" | cut -d= -f2 | tr -d ' "')
                echo "Region = \"$value\"" >> "$config_file"
            elif [[ "$line" =~ ^[[:space:]]*interval_seconds[[:space:]]*= ]]; then
                local value=$(echo "$line" | cut -d= -f2 | tr -d ' ')
                echo "IntervalSeconds = $value" >> "$config_file"
                echo "" >> "$config_file"
            fi
        fi
    done < "$DEPLOY_CONFIG"
    
    # Parse and add node regions (only forwarding and scheduling nodes, NOT traefik)
    local in_nodes=0
    local current_role=""
    local current_node_lines=()
    
    while IFS= read -r line; do
        # Skip comments and empty lines
        [[ "$line" =~ ^[[:space:]]*# ]] && continue
        [[ -z "$line" ]] && continue
        
        if [[ "$line" == "[[nodes]]" ]]; then
            # Save previous node if it should be included in node_region
            if [ $in_nodes -eq 1 ] && should_include_in_node_region "$current_role"; then
                echo "[[node_regions]]" >> "$config_file"
                for node_line in "${current_node_lines[@]}"; do
                    echo "$node_line" >> "$config_file"
                done
                echo "" >> "$config_file"
            fi
            
            # Reset for new node
            in_nodes=1
            current_role=""
            current_node_lines=()
        elif [[ "$line" =~ ^\[\[ ]] && [[ "$line" != "[[nodes]]" ]]; then
            # End of nodes section, save last node if applicable
            if [ $in_nodes -eq 1 ] && should_include_in_node_region "$current_role"; then
                echo "[[node_regions]]" >> "$config_file"
                for node_line in "${current_node_lines[@]}"; do
                    echo "$node_line" >> "$config_file"
                done
                echo "" >> "$config_file"
            fi
            in_nodes=0
        elif [ $in_nodes -eq 1 ]; then
            # Extract role
            if [[ "$line" =~ ^[[:space:]]*role[[:space:]]*=[[:space:]]*\"(.*)\" ]]; then
                current_role="${BASH_REMATCH[1]}"
            fi
            
            # Collect node data lines (ip, region, hostname, description)
            if [[ "$line" =~ ^[[:space:]]*ip[[:space:]]*= ]]; then
                current_node_lines+=("$line")
            elif [[ "$line" =~ ^[[:space:]]*region[[:space:]]*= ]]; then
                current_node_lines+=("$line")
            elif [[ "$line" =~ ^[[:space:]]*hostname[[:space:]]*= ]]; then
                current_node_lines+=("$line")
            elif [[ "$line" =~ ^[[:space:]]*description[[:space:]]*= ]]; then
                current_node_lines+=("$line")
            fi
        fi
    done < "$DEPLOY_CONFIG"
    
    # Handle last node if file ends while in_nodes=1
    if [ $in_nodes -eq 1 ] && should_include_in_node_region "$current_role"; then
        echo "[[node_regions]]" >> "$config_file"
        for node_line in "${current_node_lines[@]}"; do
            echo "$node_line" >> "$config_file"
        done
        echo "" >> "$config_file"
    fi
    
    log_info "Configuration file generated: $config_file"
}

# Build and install scheduling service
build_and_install_service() {
    local project_root="$1"
    
    log_step "Building Scheduling Service"
    
    cd "$project_root/scheduling"
    
    log_info "Building Go binary..."
    export PATH=$PATH:/usr/local/go/bin
    export GOPATH=$HOME/go
    /usr/local/go/bin/go build -o arcturus-scheduling main.go || log_error "Failed to build scheduling service"
    
    log_info "Installing binary to /usr/local/bin..."
    sudo mv arcturus-scheduling /usr/local/bin/
    sudo chmod +x /usr/local/bin/arcturus-scheduling
    
    # Create systemd service
    log_info "Creating systemd service..."
    sudo tee /etc/systemd/system/arcturus-scheduling.service > /dev/null <<EOF
[Unit]
Description=Arcturus Scheduling Plane
Documentation=https://github.com/Bootes2022/Arcturus
After=network.target mysql.service
Wants=mysql.service

[Service]
Type=simple
User=root
WorkingDirectory=$project_root/scheduling
ExecStart=/usr/local/bin/arcturus-scheduling
Restart=on-failure
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=arcturus-scheduling

[Install]
WantedBy=multi-user.target
EOF
    
    sudo systemctl daemon-reload
    sudo systemctl enable arcturus-scheduling.service
    
    log_info "Service installed successfully"
}

# Configure firewall
configure_firewall() {
    local api_port="$1"
    local traefik_port="$2"
    
    log_step "Configuring Firewall"
    
    # Port allocation:
    # 8080 - HeartbeatServer (gRPC metrics collection)
    # 8081 - Client Delay API (HTTP)
    # 8090 - Traefik Config Provider (HTTP)
    
    if command_exists firewall-cmd; then
        log_info "Configuring firewalld..."
        sudo firewall-cmd --permanent --add-port=8080/tcp  # HeartbeatServer
        sudo firewall-cmd --permanent --add-port=8081/tcp  # Client Delay API
        sudo firewall-cmd --permanent --add-port=${traefik_port}/tcp
        sudo firewall-cmd --reload
        log_info "Firewall rules added for ports 8080, 8081, and $traefik_port"
    elif command_exists ufw; then
        log_info "Configuring ufw..."
        sudo ufw allow 8080/tcp  # HeartbeatServer
        sudo ufw allow 8081/tcp  # Client Delay API
        sudo ufw allow ${traefik_port}/tcp
        log_info "Firewall rules added for ports 8080, 8081, and $traefik_port"
    else
        log_warn "No firewall detected. Please manually open ports 8080, 8081, and $traefik_port"
    fi
}

# Main deployment function
main() {
    log_step "Arcturus Scheduling Plane Deployment"
    
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
    
    MYSQL_ROOT_PASSWORD=$(parse_toml "$DEPLOY_CONFIG" "database" "mysql_root_password")
    MYSQL_DB_NAME=$(parse_toml "$DEPLOY_CONFIG" "database" "mysql_db_name")
    MYSQL_DB_USER=$(parse_toml "$DEPLOY_CONFIG" "database" "mysql_db_user")
    MYSQL_DB_PASSWORD=$(parse_toml "$DEPLOY_CONFIG" "database" "mysql_db_password")
    SQL_SCRIPT_PATH=$(parse_toml "$DEPLOY_CONFIG" "database" "sql_script_path")
    
    ETCD_NODE_NAME=$(parse_toml "$DEPLOY_CONFIG" "services.etcd" "node_name")
    ETCD_INITIAL_ADVERTISE_PEER_URLS=$(parse_toml "$DEPLOY_CONFIG" "services.etcd" "initial_advertise_peer_urls")
    ETCD_LISTEN_PEER_URLS=$(parse_toml "$DEPLOY_CONFIG" "services.etcd" "listen_peer_urls")
    ETCD_ADVERTISE_CLIENT_URLS=$(parse_toml "$DEPLOY_CONFIG" "services.etcd" "advertise_client_urls")
    ETCD_LISTEN_CLIENT_URLS=$(parse_toml "$DEPLOY_CONFIG" "services.etcd" "listen_client_urls")
    ETCD_INITIAL_CLUSTER=$(parse_toml "$DEPLOY_CONFIG" "services.etcd" "initial_cluster")
    
    SCHEDULING_API_PORT=$(parse_toml "$DEPLOY_CONFIG" "services" "scheduling_api_port")
    SCHEDULING_TRAEFIK_PORT=$(parse_toml "$DEPLOY_CONFIG" "services" "scheduling_traefik_port")
    
    # Install dependencies
    install_go "$GO_VERSION" "$GO_ARCH"
    # Note: Scheduling plane does not use etcd, only MySQL
    # etcd is only needed by forwarding plane
    install_mysql "$MYSQL_ROOT_PASSWORD"
    
    # Configure MySQL
    configure_mysql "$MYSQL_ROOT_PASSWORD" "$MYSQL_DB_NAME" "$MYSQL_DB_USER" \
        "$MYSQL_DB_PASSWORD" "$PROJECT_ROOT/$SQL_SCRIPT_PATH"
    
    # Generate scheduling configuration
    generate_scheduling_config "$PROJECT_ROOT/scheduling/scheduling_config.toml" \
        "$MYSQL_DB_USER" "$MYSQL_DB_PASSWORD" "$MYSQL_DB_NAME"
    
    # Build and install service
    build_and_install_service "$PROJECT_ROOT"
    
    # Configure firewall
    configure_firewall "$SCHEDULING_API_PORT" "$SCHEDULING_TRAEFIK_PORT"
    
    # Start service
    log_step "Starting Service"
    sudo systemctl start arcturus-scheduling.service
    sleep 3
    
    # Check service status
    if sudo systemctl is-active --quiet arcturus-scheduling.service; then
        log_info "✅ Scheduling plane deployed successfully!"
        echo ""
        echo "Service Status: $(sudo systemctl is-active arcturus-scheduling.service)"
        echo "API Port: $SCHEDULING_API_PORT"
        echo "Traefik Config Port: $SCHEDULING_TRAEFIK_PORT"
        echo ""
        echo "Useful Commands:"
        echo "  - Check status: sudo systemctl status arcturus-scheduling"
        echo "  - View logs: sudo journalctl -u arcturus-scheduling -f"
        echo "  - Restart: sudo systemctl restart arcturus-scheduling"
    else
        log_error "Service failed to start. Check logs with: sudo journalctl -u arcturus-scheduling -xe"
    fi
}

# Run main function
main "$@"

