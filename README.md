# Arcturus (Quasar)
Arcturus is a cross-cloud Global Accelerator that provides acceleration for service providers, enabling globally distributed guests to access service servers quickly through the accelerator proxy without requiring any additional configuration. It is designed to serve as a counterpart to AWS Global Accelerator and GCP Global Load Balancing.

#  Deployment Guide

This directory contains unified deployment scripts and configuration for all Arcturus components.

## 📋 Overview

Arcturus consists of three main deployment components:

1. **Scheduling Plane** (Controller) - The Scheduling Plane serves as the central coordination and configuration hub. It maintains global metadata, synchronizes system-wide state, and performs last-mile scheduling. Typically, 2–3 Controller instances are deployed to provide redundancy and ensure stable control.
2. **Forwarding Plane** (Proxy) - The Forwarding Plane comprises globally distributed Proxy nodes responsible for traffic forwarding and middle-mile path selection. For large-scale acceleration workloads (e.g., millions of QPS), dozens of Proxy nodes are deployed. Proxies are grouped by geographic regions to reduce scheduling complexity and ensure that users connect to nearby nodes, improving latency and system stability.
3. **Traefik Gateway** (Controller-pop) - The Traefik Gateway is deployed at the edge for each Proxy region, with multiple instances (≥1) to ensure redundancy. Its responsibilities are twofold: (i) performing external network probing and (ii) delivering last-mile routing decisions from the Controller to users. By placing routing decision logic closer to users, the gateway enables clients to obtain routing information through geoDNS and access the nearest available edge point. The gateway implements dynamic routing by customizing Traefik.

## 🚀 Quick Start

## File Structure

```
deployment/
├── deploy_config.toml          # Unified configuration file
├── deploy_scheduling.sh        # Scheduling plane deployment script
├── deploy_forwarding.sh        # Forwarding plane deployment script
└── deploy_traefik.sh           # Traefik + Client Probe deployment script
```

## Configuration File

The `deploy_config.toml` file contains all configuration for all components:

- **[environment]** - Go and etcd versions
- **[database]** - MySQL configuration (for scheduling)
- **[[nodes]]** - Global node list
- **[[domains]]** - Domain and origin configurations
- **[[bpr_scheduling_tasks]]** - Scheduling task definitions
- **[services]** - Service endpoints and ports
- **[traefik]** - Traefik version and plugin settings

The `deploy_to_servers.sh` file contains all configuration for all components:

- **[FORWARDING_SERVERS]** - The following IPs are assigned as **Forwarding Servers** in the Arcturus deployment
- **[TRAEFIK_SERVERS]** - The following IPs are assigned as **TRAEFIK_SERVERS** 
- **[[SCHEDULING_SERVERS]]** - The following IPs are assigned as **SCHEDULING_SERVERS**

### Step 1: Configure Deployment

### 1. Edit the unified configuration file:

```bash
vim deployment/deploy_config.toml
```

Configure the following sections:
- Database credentials (for scheduling plane)
- Node list (all forwarding nodes)
- Domain configurations
- Service endpoints

### Step 2: Batch Deployment

### 1. Set Unified VM Password
Modify the unified VM password:

```bash
PASSWORD="Arcturus@Test2024"
```

### 2. Edit the batch deployment file:

```bash
vim deployment/deploy_to_server.sh
```

### 3. Run the batch deployment script:
```bash
bash deployment/deploy_to_server.sh
```

## Service Management

### Check Service Status

```bash
# Scheduling
sudo systemctl status arcturus-scheduling

# Forwarding
sudo systemctl status arcturus-forwarding

# Traefik
sudo systemctl status traefik
sudo systemctl status traefik-probing_client-probing_report
```

### View Logs

```bash
# Scheduling
sudo journalctl -u arcturus-scheduling -f

# Forwarding
sudo journalctl -u arcturus-forwarding -f

# Traefik
sudo journalctl -u traefik -f
sudo journalctl -u traefik-probing_client-probing_report -f
```

### Restart Services

```bash
# Scheduling
sudo systemctl restart arcturus-scheduling

# Forwarding
sudo systemctl restart arcturus-forwarding

# Traefik
sudo systemctl restart traefik
sudo systemctl restart traefik-probing_client-probing_report
```

## Testing

We already provide acceleration for the Google homepage and GitHub homepage by default. You can access these services through the Controller-pop.

For example, you can visit:

http://80.240.28.138/resolve/google.com

Here, the IP address should be the Controller-pop (Traefik) deployed in your region. The request will automatically redirect to:

http://45.77.90.250:50055/resolve/google.com

where 45.77.90.250 is the Proxy node in your region, and 50055 is the access port, ultimately serving the Google homepage.

## Troubleshooting

### Scheduling Plane Issues

- Check MySQL connection: `mysql -u myapp_user -p`
- Verify etcd cluster: `etcdctl endpoint health`
- Check API accessibility: `curl http://localhost:8080/ping`

### Forwarding Plane Issues

- Check etcd connectivity
- Verify metrics reporting to scheduling plane
- Check port availability (50050-50059)

### Traefik Issues

- Check Traefik dashboard: `http://<traefik_ip>:8080/dashboard/`
- Verify plugin loading in logs
- Check client probe connectivity to scheduling plane

## 📝 License

Apache 2.0 - See LICENSE file for details

