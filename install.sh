#!/bin/bash

# ============================================================
#  VPNShop - Automated Installation Script
#  - Latest Go from go.dev (not apt)
#  - goproxy.cn for fast module downloads
#  - Interactive configuration
#  - Optional Nginx + SSL with certbot
# ============================================================

echo ""
echo "=================================================="
echo "        VPNShop - Automated Installer"
echo "=================================================="
echo ""

# ------------------------------------------------------------
# Step 1: Configuration (interactive prompts)
# ------------------------------------------------------------
echo "[1/5] Configuration"
echo "--------------------------------------------------"

# Shop domain
read -p "Shop domain (e.g. shop.example.com) [Enter to skip]: " domain_name </dev/tty

# Admin username
read -p "Admin username [default: admin]: " admin_user </dev/tty
admin_user=${admin_user:-admin}

# Admin password (silent input)
default_admin_pass=$(head -c 200 /dev/urandom | tr -dc 'a-z0-9' | head -c 16)
read -sp "Admin password [Enter to auto-generate]: " admin_pass </dev/tty
echo ""
admin_pass=${admin_pass:-$default_admin_pass}

# Admin secret path
default_admin_path=$(head -c 200 /dev/urandom | tr -dc 'a-z0-9' | head -c 24)
read -p "Admin secret path [Enter to auto-generate]: " admin_path </dev/tty
admin_path=${admin_path:-$default_admin_path}

echo ""
echo "Configuration saved."
echo ""

# ------------------------------------------------------------
# Step 2: Install system dependencies (without Go)
# ------------------------------------------------------------
echo "[2/5] Installing system dependencies..."
echo "--------------------------------------------------"
sudo apt update
sudo apt install -y git build-essential curl wget ca-certificates
echo "Dependencies installed."
echo ""

# ------------------------------------------------------------
# Step 3: Install latest Go from go.dev
# ------------------------------------------------------------
echo "[3/5] Installing latest Go from go.dev..."
echo "--------------------------------------------------"

# Detect architecture
ARCH=$(uname -m)
if [ "$ARCH" = "x86_64" ]; then
    GOARCH="amd64"
elif [ "$ARCH" = "aarch64" ]; then
    GOARCH="arm64"
else
    echo "❌ Unsupported architecture: $ARCH"
    exit 1
fi

# Fetch latest Go version
LATEST_VERSION=$(curl -fsSL "https://go.dev/VERSION?m=text" | head -n1 | tr -d '\r\n ')
if [ -z "$LATEST_VERSION" ]; then
    echo "❌ Failed to fetch Go version from go.dev"
    exit 1
fi

echo "Latest Go version: $LATEST_VERSION"

# Remove old Go installation
sudo rm -rf /usr/local/go

# Download and install
cd /tmp
TARBALL="${LATEST_VERSION}.linux-${GOARCH}.tar.gz"
echo "Downloading $TARBALL..."
if ! curl -fSL --retry 3 --retry-delay 5 -o "$TARBALL" "https://go.dev/dl/${TARBALL}"; then
    echo "❌ Failed to download Go"
    exit 1
fi

sudo tar -C /usr/local -xzf "$TARBALL"
rm -f "$TARBALL"

# Set PATH permanently
echo 'export PATH="/usr/local/go/bin:$PATH"' >> ~/.bashrc
echo 'export GOPATH="/root/go"' >> ~/.bashrc
export PATH="/usr/local/go/bin:$PATH"
export GOPATH="/root/go"

echo "✅ Go installed: $(go version)"

# Configure Go environment
go env -w GOPROXY=https://goproxy.cn,direct
go env -w GOSUMDB=sum.golang.org
go env -w GOPATH=/root/go
go env -w GOTOOLCHAIN=local

echo "✅ Go environment configured"
echo ""

# ------------------------------------------------------------
# Step 4: Download source code and build
# ------------------------------------------------------------
echo "[4/5] Downloading source code and building..."
echo "--------------------------------------------------"
cd /root
rm -rf vpnshop
git clone https://github.com/asd1asd00000/vpnshop.git
cd vpnshop
go mod tidy
CGO_ENABLED=1 go build -o vpnshop-app .
echo "Build completed."
echo ""

# ------------------------------------------------------------
# Step 5: Create and start systemd service
# ------------------------------------------------------------
echo "[5/5] Setting up systemd service..."
echo "--------------------------------------------------"

cat <<EOF > /etc/systemd/system/vpnshop.service
[Unit]
Description=VPNShop Golang Service
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/root/vpnshop
ExecStart=/root/vpnshop/vpnshop-app
Restart=always
RestartSec=5

# Admin dashboard credentials (set during installation)
Environment="ADMIN_USER=$admin_user"
Environment="ADMIN_PASS=$admin_pass"
Environment="ADMIN_SECRET_PATH=$admin_path"

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable vpnshop
systemctl restart vpnshop
echo "VPNShop service installed and started."
echo ""

# ------------------------------------------------------------
# Nginx + SSL setup (only if a domain was provided)
# ------------------------------------------------------------
if [ -n "$domain_name" ]; then
    echo "Configuring Nginx and SSL for $domain_name ..."
    echo "--------------------------------------------------"

    if ! command -v nginx &> /dev/null; then
        apt update && apt install -y nginx certbot python3-certbot-nginx
    fi

    mkdir -p /etc/nginx/sites-available /etc/nginx/sites-enabled

    cat <<EOF > /etc/nginx/sites-available/vpnshop
server {
    listen 80;
    server_name $domain_name;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }
}
EOF

    ln -sf /etc/nginx/sites-available/vpnshop /etc/nginx/sites-enabled/
    nginx -t && systemctl restart nginx

    certbot --nginx -d "$domain_name" --non-interactive --agree-tos -m "admin@$domain_name" --redirect \
        || echo "WARNING: SSL certificate failed. Make sure your domain points to this server."

    shop_url="https://$domain_name"
    admin_url="https://$domain_name/$admin_path/admin"
    echo "Domain $domain_name configured with HTTPS."
else
    echo "No domain provided. Skipping Nginx/SSL setup."
    shop_url="http://<SERVER_IP>:8080"
    admin_url="http://<SERVER_IP>:8080/$admin_path/admin"
fi

echo ""

# ------------------------------------------------------------
# Final summary table
# ------------------------------------------------------------
echo "=================================================="
echo "   Installation completed successfully!"
echo "=================================================="
echo ""

lines=()
lines+=("Shop URL          : $shop_url")
lines+=("Admin URL         : $admin_url")
lines+=("Admin Username    : $admin_user")
lines+=("Admin Password    : $admin_pass")
lines+=("Admin Secret Path : $admin_path")
lines+=("Go Version        : $LATEST_VERSION")

max=0
for l in "${lines[@]}"; do
    [ ${#l} -gt $max ] && max=${#l}
done

border=$(printf '─%.0s' $(seq 1 $((max + 2))))
echo "┌$border┐"
for l in "${lines[@]}"; do
    printf '│ %-*s │\n' "$max" "$l"
done
echo "└$border┘"

echo ""
echo "IMPORTANT: Save your admin credentials and secret path in a safe place!"
echo ""
