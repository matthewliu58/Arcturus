#!/bin/bash

CLUSTER_INFO_FILE="cluster-info"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${GREEN}=== SkyAccel Deployment Script ===${NC}"

if [ ! -f "$CLUSTER_INFO_FILE" ]; then
    echo -e "${RED}Error: Cluster information file not found${NC}"
    exit 1
fi

read_cluster_info() {
    local node_name="$1"
    local key="$2"
    grep -A 20 "^${node_name}:" "$CLUSTER_INFO_FILE" | grep "${key}:" | awk -F': ' '{print $2}' | sed 's/^"//;s/"$//'
}

get_all_master_private_ips() {
    grep -A 20 "role: master" "$CLUSTER_INFO_FILE" | grep "private_ip:" | awk -F': ' '{print $2}' | sed 's/^"//;s/"$//'
}

deploy_to_target() {
    local node_name="$1"
    local public_ip=$(read_cluster_info "$node_name" "ip")
    local provider=$(read_cluster_info "$node_name" "provider")
    local continent=$(read_cluster_info "$node_name" "continent")
    local country=$(read_cluster_info "$node_name" "country")
    local city=$(read_cluster_info "$node_name" "city")
    local role=$(read_cluster_info "$node_name" "role")
    local server_private_ip=$(read_cluster_info "$node_name" "server")
    local ssh_user=$(read_cluster_info "$node_name" "user")
    local ssh_pw=$(read_cluster_info "$node_name" "pw")

    echo -e "${GREEN}Deploying to $node_name | IP: $public_ip | User: $ssh_user${NC}"

    local temp_dir=$(mktemp -d)
    cp -r ./* "$temp_dir"/

    # Generate config/node.yaml with node info from cluster-info
    mkdir -p "$temp_dir/config"
    cat > "$temp_dir/config/node.yaml" <<EOF
# Shared node configuration for Arcturus modules
# Auto-generated from cluster-info by deploy.sh

provider: "$provider"
continent: "$continent"
country: "$country"
city: "$city"
ip:
  public: "$public_ip"
EOF

    local cfg="$temp_dir/control-plane/config.yaml"
    local master_private_ips=($(get_all_master_private_ips))

    if [ "$role" = "master" ]; then
        sed -i "s|server_ip: \"[^\"]*\"|server_ip: \"$private_ip\"|" "$cfg"
        server_list="  - \"$private_ip\"\n"
        for ip in "${master_private_ips[@]}"; do
            if [ "$ip" != "$private_ip" ]; then
                server_list+="  - \"$ip\"\n"
            fi
        done
    else
        sed -i "s|server_ip: \"[^\"]*\"|server_ip: \"\"|" "$cfg"
        server_list="  - \"$server_private_ip\"\n"
        for ip in "${master_private_ips[@]}"; do
            if [ "$ip" != "$server_private_ip" ]; then
                server_list+="  - \"$ip\"\n"
            fi
        done
    fi

    sed -i "/^server_list:/,/^[^ ]/c\\server_list:\n$server_list" "$cfg"

    tar -czf "$temp_dir/SkyAccel.tar.gz" -C "$temp_dir" .

    # Copy and deploy to remote
    echo -e "${GREEN}[1/3] Copying files to $public_ip...${NC}"
    if [ -n "$ssh_pw" ]; then
        sshpass -p "$ssh_pw" scp -o StrictHostKeyChecking=no "$temp_dir/SkyAccel.tar.gz" ${ssh_user}@${public_ip}:/root/
    else
        scp -o StrictHostKeyChecking=no "$temp_dir/SkyAccel.tar.gz" ${ssh_user}@${public_ip}:/root/
    fi

    echo -e "${GREEN}[2/3] Running basic-env.sh + optimize-network.sh...${NC}"
    if [ -n "$ssh_pw" ]; then
        sshpass -p "$ssh_pw" ssh -o StrictHostKeyChecking=no ${ssh_user}@${public_ip} \
            "mkdir -p /root/SkyAccel && tar -xzf /root/SkyAccel.tar.gz -C /root/SkyAccel && cd /root/SkyAccel && bash basic-env.sh && bash optimize-network.sh"
    else
        ssh -o StrictHostKeyChecking=no ${ssh_user}@${public_ip} \
            "mkdir -p /root/SkyAccel && tar -xzf /root/SkyAccel.tar.gz -C /root/SkyAccel && cd /root/SkyAccel && bash basic-env.sh && bash optimize-network.sh"
    fi

    echo -e "${GREEN}[3/3] Building and starting services...${NC}"
    if [ -n "$ssh_pw" ]; then
        sshpass -p "$ssh_pw" ssh -o StrictHostKeyChecking=no ${ssh_user}@${public_ip} \
            "cd /root/SkyAccel && bash setup-systemd.sh"
    else
        ssh -o StrictHostKeyChecking=no ${ssh_user}@${public_ip} \
            "cd /root/SkyAccel && bash setup-systemd.sh"
    fi

    rm -rf "$temp_dir"
    echo -e "${GREEN}$node_name deployed successfully!${NC}"
}

deploy_all() {
    local nodes=$(grep -E "^[a-zA-Z0-9_-]+:" "$CLUSTER_INFO_FILE" | cut -d':' -f1)
    for node in $nodes; do
        deploy_to_target "$node"
    done
}

show_help() {
    echo -e "${YELLOW}Usage:$NC"
    echo "  $0 --deploy-all"
    echo "  $0 --deploy <node-name>"
    echo "  $0 --help"
}

if [ $# -eq 0 ]; then
    show_help
    exit 0
fi

while [ $# -gt 0 ]; do
    case "$1" in
        --deploy-all) deploy_all; shift ;;
        --deploy) deploy_to_target "$2"; shift 2 ;;
        --help) show_help; exit 0 ;;
        *) echo -e "${RED}Unknown option$NC"; exit 1 ;;
    esac
done