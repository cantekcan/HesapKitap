#!/usr/bin/env bash
# Günlük PostgreSQL yedeği (canlı sunucuda cron ile çalıştırılır).
#   0 3 * * *  cd /opt/hesapkitap && ./scripts/backup.sh >> /var/log/hesapkitap-backup.log 2>&1
# Ortam: BACKUP_DIR (varsayılan ./backups), KEEP_DAYS (varsayılan 30),
#        OFFSITE_CMD (isteğe bağlı: dosyayı sunucu dışına gönderen komut; dosya yolu $1 olarak verilir,
#        örn. OFFSITE_CMD='rclone copy "$1" remote:hesapkitap-yedek').
set -euo pipefail

cd "$(dirname "$0")/.."
BACKUP_DIR="${BACKUP_DIR:-./backups}"
KEEP_DAYS="${KEEP_DAYS:-30}"
COMPOSE=(docker compose -f docker-compose.prod.yml --env-file .env.prod)

mkdir -p "$BACKUP_DIR"
file="$BACKUP_DIR/hesapkitap-$(date +%Y%m%d-%H%M%S).sql.gz"

"${COMPOSE[@]}" exec -T db pg_dump -U hesapkitap -d hesapkitap --no-owner | gzip -9 > "$file"

# Boş/bozuk yedeği kabul etme
if [ "$(gzip -dc "$file" | head -c 100 | wc -c)" -lt 50 ]; then
  echo "HATA: yedek boş görünüyor: $file" >&2
  rm -f "$file"
  exit 1
fi
echo "Yedek alındı: $file ($(du -h "$file" | cut -f1))"

if [ -n "${OFFSITE_CMD:-}" ]; then
  bash -c "$OFFSITE_CMD" _ "$file"
  echo "Sunucu dışına gönderildi."
else
  echo "UYARI: OFFSITE_CMD ayarlı değil; yedek yalnızca bu sunucuda duruyor." >&2
fi

find "$BACKUP_DIR" -name 'hesapkitap-*.sql.gz' -mtime +"$KEEP_DAYS" -delete
