#!/bin/bash
# Peer Compute Gateway Deployment Script
# Domain: peercompute.xdastechnology.com

set -e

DOMAIN="peercompute.xdastechnology.com"
ACME_EMAIL="admin@xdastechnology.com"
INSTALL_DIR="/opt/peercompute"
DATA_DIR="/var/lib/peercompute-gateway"

echo "=========================================="
echo "Peer Compute Gateway Deployment"
echo "Domain: $DOMAIN"
echo "=========================================="

# Update system
echo "[1/7] Updating system..."
sudo apt-get update -y
sudo apt-get upgrade -y

# Install dependencies
echo "[2/7] Installing dependencies..."
sudo apt-get install -y wget git

# Install Go
echo "[3/7] Installing Go 1.22..."
if ! command -v go &> /dev/null; then
    wget -q https://go.dev/dl/go1.22.10.linux-amd64.tar.gz
    sudo tar -C /usr/local -xzf go1.22.10.linux-amd64.tar.gz
    rm go1.22.10.linux-amd64.tar.gz
    export PATH=$PATH:/usr/local/go/bin
    echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
fi
echo "Go version: $(go version)"

# Clone and build
echo "[4/7] Building gateway..."
sudo mkdir -p $INSTALL_DIR
cd /tmp
git clone https://github.com/xdas-research/peer-compute.git || true
cd peer-compute
go build -o gateway ./cmd/gateway
sudo mv gateway $INSTALL_DIR/
sudo chmod +x $INSTALL_DIR/gateway

# Create data directory
echo "[5/7] Creating directories..."
sudo mkdir -p $DATA_DIR

# Create systemd service
echo "[6/7] Creating systemd service..."
sudo tee /etc/systemd/system/peercompute-gateway.service > /dev/null << EOF
[Unit]
Description=Peer Compute Gateway
After=network.target

[Service]
Type=simple
User=root
ExecStart=$INSTALL_DIR/gateway \\
  --domain $DOMAIN \\
  --acme-email $ACME_EMAIL \\
  --https-port 443 \\
  --http-port 80 \\
  --tunnel-port 8443 \\
  --data-dir $DATA_DIR
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

# Start service
echo "[7/7] Starting gateway..."
sudo systemctl daemon-reload
sudo systemctl enable peercompute-gateway
sudo systemctl start peercompute-gateway

echo ""
echo "=========================================="
echo "Deployment Complete!"
echo "=========================================="
echo ""
echo "Gateway URL: https://$DOMAIN"
echo "Tunnel Port: 8443"
echo ""
echo "Commands:"
echo "  Status:  sudo systemctl status peercompute-gateway"
echo "  Logs:    sudo journalctl -u peercompute-gateway -f"
echo "  Restart: sudo systemctl restart peercompute-gateway"
echo ""
echo "DNS Required:"
echo "  $DOMAIN     -> A record -> <this-server-ip>"
echo "  *.$DOMAIN   -> A record -> <this-server-ip>"
echo ""
echo "Security Group Ports:"
echo "  80   (HTTP)   - Let's Encrypt"
echo "  443  (HTTPS)  - Public traffic"
echo "  8443 (Tunnel) - Provider connections"
