#!/usr/bin/env bash
# Yedekten geri yükleme. DİKKAT: mevcut veritabanının üzerine yazar.
#   ./scripts/restore.sh backups/hesapkitap-YYYYMMDD-HHMMSS.sql.gz
# Önce boş bir deneme veritabanına yüklemek için: RESTORE_DB=hesapkitap_deneme ./scripts/restore.sh <dosya>
set -euo pipefail

[[ $# -eq 1 ]] || { echo "Kullanım: $0 <yedek.sql.gz>" >&2; exit 1; }
cd "$(dirname "$0")/.."
DB="${RESTORE_DB:-hesapkitap}"
COMPOSE=(docker compose -f docker-compose.prod.yml --env-file .env.prod)

if [[ "$DB" != "hesapkitap" ]]; then
  "${COMPOSE[@]}" exec -T db psql -U hesapkitap -d postgres -c "drop database if exists \"$DB\"" -c "create database \"$DB\""
else
  read -r -p "'$DB' veritabanı SİLİNİP yedekten yeniden oluşturulacak. Devam? (evet yazın) " ans
  [[ "$ans" = "evet" ]] || exit 1
  "${COMPOSE[@]}" stop app
  "${COMPOSE[@]}" exec -T db psql -U hesapkitap -d postgres -c "drop database \"$DB\"" -c "create database \"$DB\""
fi

gzip -dc "$1" | "${COMPOSE[@]}" exec -T db psql -U hesapkitap -d "$DB" -v ON_ERROR_STOP=1 -q
echo "Geri yüklendi: $DB"
[[ "$DB" != "hesapkitap" ]] || "${COMPOSE[@]}" start app
