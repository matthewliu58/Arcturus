#!/usr/bin/env bash
set -e

GO_VERSION="1.21.3"
GO_TAR="go${GO_VERSION}.linux-amd64.tar.gz"
GO_URL="https://dl.google.com/go/${GO_TAR}"

echo "==> Update system"
sudo apt update && sudo apt upgrade -y

echo "==> Install basic tools"
sudo apt install -y git vim wget build-essential ca-certificates libssl-dev pkg-config

# ---------- tier-1 essential tools ----------
echo "==> Install tier-1 essential tools (curl / htop / tmux)"
sudo apt install -y curl htop tmux
echo "==> Verify tier-1 tools installation"
curl --version | head -n 1
htop --version
tmux -V
# -------------------------------------------

echo "==> Install Go ${GO_VERSION}"
if [ -d "/usr/local/go" ]; then
    echo "Found existing /usr/local/go, backing up to /usr/local/go.bak"
    sudo mv /usr/local/go /usr/local/go.bak
fi

wget -q ${GO_URL} -O /tmp/${GO_TAR}
sudo tar -C /usr/local -xzf /tmp/${GO_TAR}
rm -f /tmp/${GO_TAR}

echo "==> Configure Go environment"
BASHRC="$HOME/.bashrc"

# 避免重复写入
if ! grep -q "GOROOT=/usr/local/go" "$BASHRC"; then
cat << EOF >> "$BASHRC"

# Go ${GO_VERSION} environment
export GOROOT=/usr/local/go
export GOPATH=\$HOME/go
export PATH=\$PATH:\$GOROOT/bin:\$GOPATH/bin
export GOPROXY="https://proxy.golang.org,direct"
EOF
fi

mkdir -p "$HOME/go"

# ===== 立即生效 Go 环境（关键！不用再 source ~/.bashrc）=====
export GOROOT=/usr/local/go
export GOPATH=$HOME/go
export PATH=$PATH:$GOROOT/bin:$GOPATH/bin

# ========== Rust 环境 ==========
echo -e "\n==> Install Rust (Stable)"
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y

# 让 Rust 立即生效（关键！不用重启终端）
source $HOME/.cargo/env

# 写入环境变量到 bashrc（永久生效）
if ! grep -q ".cargo/env" "$BASHRC"; then
cat << EOF >> "$BASHRC"

# Rust environment
source \$HOME/.cargo/env
EOF
fi

echo "==> Verify Rust installation"
rustc --version
cargo --version
rustup --version

# ========== 自动安装项目必需依赖 ==========
#echo -e "\n==> Install required Rust dependencies for data-proxy"
#cargo add futures_util
#cargo add bytes
#cargo add once_cell
#cargo add sysinfo
#cargo add tracing
#cargo add hyper-tls

# ===== 结束 =====
echo -e "\n==> All installation done!"
echo "Go  and Rust are ready!"
echo "You can now run: cargo build  or  cargo build --release"