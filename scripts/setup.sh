#!/bin/bash
# Iris — DigitalOcean one-shot setup script
# Run as root on a fresh Ubuntu 24.04 droplet:
#   curl -fsSL https://raw.githubusercontent.com/YOURUSER/Iris/main/scripts/setup.sh | bash
set -e

REPO_URL="https://github.com/eulerbutcooler/Iris.git"
APP_DIR="/home/iris/Iris"
DOMAIN="${1:-}"   # pass domain as arg or set here, e.g. "iris.amanyd.me" (no https://)
# Strip https:// or http:// if accidentally included
DOMAIN="${DOMAIN#https://}"
DOMAIN="${DOMAIN#http://}"

echo "==> Installing dependencies..."
apt-get update -q
apt-get install -y -q git curl nginx certbot python3-certbot-nginx postgresql-client

# Docker (for Postgres + NATS)
curl -fsSL https://get.docker.com | sh
systemctl enable docker

# Go
snap install go --classic

# Node + PM2 (for Next.js)
curl -fsSL https://deb.nodesource.com/setup_20.x | bash -
apt-get install -y nodejs
npm install -g pm2

echo "==> Creating iris user..."
useradd -m -s /bin/bash iris 2>/dev/null || true
usermod -aG docker iris

echo "==> Cloning repo..."
sudo -u iris git clone "$REPO_URL" "$APP_DIR"

echo "==> Building Go binaries..."
cd "$APP_DIR"
export PATH=$PATH:/snap/bin
mkdir -p bin
go build -o bin/core     ./services/core/cmd/api
go build -o bin/hooks    ./services/hooks/cmd/server
go build -o bin/worker   ./services/worker/cmd
go build -o bin/telegram ./services/iris-telegram/cmd/bot
chown -R iris:iris bin/

echo "==> Building frontend..."
cd "$APP_DIR/web/iris-web"
sudo -u iris npm install
sudo -u iris npm run build
cd "$APP_DIR"

echo "==> Starting infra (Postgres + NATS)..."
docker compose up -d

echo "==> Waiting for Postgres to be ready..."
sleep 5

echo "==> Running migrations..."
source .env
psql "$DATABASE_URL" -f services/core/db/migrations/000001_initial.up.sql || true
psql "$DATABASE_URL" -f services/core/db/migrations/000002_system_settings.up.sql || true

echo "==> Creating systemd services..."

for svc in core hooks worker telegram; do
  BINARY="core"
  [ "$svc" = "hooks" ]    && BINARY="hooks"
  [ "$svc" = "worker" ]   && BINARY="worker"
  [ "$svc" = "telegram" ] && BINARY="telegram"

  cat > /etc/systemd/system/iris-$svc.service << EOF
[Unit]
Description=Iris $svc
After=network.target docker.service
Requires=docker.service

[Service]
User=iris
WorkingDirectory=$APP_DIR
EnvironmentFile=$APP_DIR/.env
ExecStart=$APP_DIR/bin/$BINARY
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF
done

systemctl daemon-reload
systemctl enable iris-core iris-hooks iris-worker iris-telegram
systemctl start  iris-core iris-hooks iris-worker
# Telegram starts after core is healthy
sleep 3
systemctl start iris-telegram

echo "==> Starting frontend with PM2..."
cd "$APP_DIR/web/iris-web"
sudo -u iris pm2 start npm --name "iris-web" -- start
sudo -u iris pm2 startup
sudo -u iris pm2 save
cd "$APP_DIR"

if [ -n "$DOMAIN" ]; then
  echo "==> Configuring Nginx..."
  cat > /etc/nginx/sites-available/iris << EOF
server { server_name $DOMAIN; location / { proxy_pass http://localhost:3001; proxy_set_header Host \$host; proxy_set_header X-Real-IP \$remote_addr; } }
server { server_name api.$DOMAIN; location / { proxy_pass http://localhost:3000; proxy_set_header Host \$host; proxy_set_header X-Real-IP \$remote_addr; } }
server { server_name hooks.$DOMAIN; location / { proxy_pass http://localhost:8080; proxy_set_header Host \$host; proxy_set_header X-Real-IP \$remote_addr; } }
EOF
  ln -sf /etc/nginx/sites-available/iris /etc/nginx/sites-enabled/iris
  nginx -t && systemctl reload nginx

  echo "==> Getting SSL certs..."
  certbot --nginx -d "$DOMAIN" -d "api.$DOMAIN" -d "hooks.$DOMAIN" --non-interactive --agree-tos -m admin@$DOMAIN
fi

echo ""
echo "✅ Iris is running!"
echo "   Frontend: http://$(curl -s ifconfig.me):3001"
[ -n "$DOMAIN" ] && echo "   Frontend: https://$DOMAIN"
echo ""
echo "📝 Next steps:"
echo "   1. Edit $APP_DIR/.env with your real API keys"
echo "   2. sudo systemctl restart iris-core iris-hooks iris-worker iris-telegram"
echo "   3. Open the dashboard → Connections → paste your Telegram bot token"
