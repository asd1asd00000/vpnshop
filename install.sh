#!/bin/bash

# ============================================================
#  VPNShop - Automated Installation Script
# ============================================================

echo ""
echo "=================================================="
echo "        VPNShop - Automated Installer"
echo "=================================================="
echo ""

# ------------------------------------------------------------
# Step 1: Configuration (interactive prompts)
# ------------------------------------------------------------
echo "[1/4] Configuration"
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
# Step 2: Install system dependencies
# ------------------------------------------------------------
echo "[2/4] Installing system dependencies..."
echo "--------------------------------------------------"
sudo apt update
sudo apt install -y golang-go git build-essential
echo "Dependencies installed."
echo ""

# ------------------------------------------------------------
# Step 3: Download source code and build
# ------------------------------------------------------------
echo "[3/4] Downloading source code and building..."
echo "--------------------------------------------------"
cd /root
rm -rf vpnshop
git clone https://github.com/asd1asd00000/vpnshop.git
cd vpnshop
go mod tidy
CGO_ENABLED=1 go build -o vpnshop-app main.go
echo "Build completed."
echo ""

# ------------------------------------------------------------
# Step 4: Create and start systemd service
# ------------------------------------------------------------
echo "[4/4] Setting up systemd service..."
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

# Guard panel settings
Environment="PANEL_URL=https://core.erfjab.com"
Environment="PANEL_USER=Javatava"
Environment="PANEL_PASS=3cet2&xhsA&X"

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
