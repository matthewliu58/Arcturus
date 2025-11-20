# Multi-Server Deployment Script - PowerShell
# Deploy Arcturus to Forwarding, Traefik, and Scheduling servers

param(
    [switch]$Parallel = $false,
    [int]$MaxJobs = 5
)

$forwardingServers = @(
    "217.69.3.223",
    "45.77.137.43",
    "208.85.23.210",
    "64.177.67.90",
    "45.76.76.46",
    "66.135.18.151",
    "155.138.162.162",
    "149.28.25.2",
    "139.84.220.90",
    "139.180.174.10",
    "67.219.99.230"
)

$traefikServers = @(
    "80.240.28.138",
    "149.248.8.34",
    "45.32.24.199"
)

$schedulingServers = @(
    "45.77.90.250"
)

$username = "root"
$password = "Arcturus@Test2024"
$timestamp = Get-Date -Format "yyyyMMdd_HHmmss"
$logFile = "deployment_$timestamp.log"

function Write-Log {
    param([string]$Message, [string]$Level = "INFO")
    $ts = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    $msg = "[$ts] $Level : $Message"
    
    if ($Level -eq "ERROR") { Write-Host $msg -ForegroundColor Red }
    elseif ($Level -eq "WARNING") { Write-Host $msg -ForegroundColor Yellow }
    elseif ($Level -eq "SUCCESS") { Write-Host $msg -ForegroundColor Green }
    elseif ($Level -eq "INFO") { Write-Host $msg -ForegroundColor Blue }
    else { Write-Host $msg }
    
    Add-Content -Path $logFile -Value $msg
}

function Deploy-Server {
    param([string]$Server, [string]$Type)
    
    Write-Log "Connecting to $Server ($Type)" "INFO"
    
    $cmd = switch ($Type) {
        "Forwarding" {
"set -e
cd /root
if [ -d Arcturus ]; then rm -rf Arcturus; fi
echo 'Cloning repository...'
git clone -q --progress https://github.com/Bootes2022/Arcturus.git 2>&1 | grep -E 'Receiving objects:|done'
cd Arcturus/deployment
sudo bash deploy_forwarding.sh
sudo systemctl status arcturus-forwarding | grep running"
        }
        "Traefik" {
"set -e
cd /root
if [ -d Arcturus ]; then rm -rf Arcturus; fi
echo 'Cloning repository...'
git clone -q --progress https://github.com/Bootes2022/Arcturus.git 2>&1 | grep -E 'Receiving objects:|done'
cd Arcturus/deployment
sudo bash deploy_traefik.sh
sudo bash deploy_forwarding.sh
sudo systemctl status traefik | grep running
sudo systemctl status traefik-client-probe | grep running
sudo systemctl status arcturus-forwarding | grep running"
        }
        "Scheduling" {
"set -e
cd /root
if [ -d Arcturus ]; then rm -rf Arcturus; fi
echo 'Cloning repository...'
git clone -q --progress https://github.com/Bootes2022/Arcturus.git 2>&1 | grep -E 'Receiving objects:|done'
cd Arcturus/deployment
sudo bash deploy_scheduling.sh
sudo systemctl status arcturus-scheduling | grep running"
        }
    }
    
    $proc = Start-Process -FilePath ssh -ArgumentList "-o StrictHostKeyChecking=no", "-o UserKnownHostsFile=/dev/null", "$username@$Server", $cmd -NoNewWindow -PassThru -Wait
    
    if ($proc.ExitCode -eq 0) {
        Write-Log "✓ $Server succeeded" "SUCCESS"
        return $true
    } else {
        Write-Log "✗ $Server failed" "ERROR"
        return $false
    }
}

function Deploy-Type {
    param([string]$Type, [array]$Servers)
    
    if ($Servers.Count -eq 0) { return }
    
    Write-Log "Deploying $Type servers ($($Servers.Count))" "INFO"
    
    $success = 0
    $failed = 0
    $failedServers = @()
    
    foreach ($server in $Servers) {
        try {
            if (Deploy-Server -Server $server -Type $Type) {
                $success++
            } else {
                $failed++
                $failedServers += $server
            }
        } catch {
            Write-Log "Exception deploying $server : $_" "ERROR"
            $failed++
            $failedServers += $server
        }
        Start-Sleep -Seconds 1
    }
    
    Write-Log "$Type: Success=$success Failed=$failed" "INFO"
    if ($failed -gt 0) {
        Write-Log "$Type failed servers: $($failedServers -join ', ')" "WARNING"
    }
}

Write-Log "Starting Deployment" "INFO"
Write-Log "Forwarding: $($forwardingServers.Count) Traefik: $($traefikServers.Count) Scheduling: $($schedulingServers.Count)" "INFO"

# Deploy each type, continue even if one fails
try { Deploy-Type -Type "Scheduling" -Servers $schedulingServers } catch { Write-Log "Scheduling deployment failed: $_" "ERROR" }
try { Deploy-Type -Type "Traefik" -Servers $traefikServers } catch { Write-Log "Traefik deployment failed: $_" "ERROR" }
try { Deploy-Type -Type "Forwarding" -Servers $forwardingServers } catch { Write-Log "Forwarding deployment failed: $_" "ERROR" }

Write-Log "Deployment Complete - Log: $logFile" "SUCCESS"
