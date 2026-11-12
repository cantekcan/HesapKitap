#!/usr/bin/env bash
# Faz 0.2 — Trendyol E-Faturam STAGE duman testi.
# Çalıştır (Git Bash): bash scripts/stage-smoke.sh [VKN_veya_TCKN]
#
# Şifre terminalde gizli sorulur; token ve şifre ekrana YAZILMAZ, dosyaya kaydedilmez.
# Çıktıyı (token içermez) Claude'a yapıştırabilirsiniz.
set -u

BASE="${TRENDYOL_BASE_URL:-https://stage-apigateway.trendyolefaturam.com}"
TAXID="${1:-}"

echo "== 0) Dış IPv4 adresi (portalda kayıtlı IP ile birebir aynı olmalı)"
curl -4 -s --max-time 10 https://ifconfig.me || echo "(IP öğrenilemedi)"
echo; echo

read -r -p "Portal e-postası: " EMAIL
read -r -s -p "Stage şifresi (görünmez): " PASS
echo

esc() { printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'; }
hdr=$(mktemp); trap 'rm -f "$hdr"' EXIT

echo; echo "== 1) POST /api/auth/signin"
printf '{"email":"%s","password":"%s"}' "$(esc "$EMAIL")" "$(esc "$PASS")" |
  curl -4 -s --max-time 20 -o /dev/null -D "$hdr" -w "HTTP %{http_code}\n" \
    -X POST "$BASE/api/auth/signin" -H 'Content-Type: application/json' --data-binary @-
unset PASS

TOKEN=$(tr -d '\r' < "$hdr" | awk -F': ' 'tolower($1)=="x-access-token"{print $2}')
echo "Header adları: $(tr -d '\r' < "$hdr" | awk -F': ' 'NR>1 && $1!=""{printf "%s ", tolower($1)}')"
if [ -z "$TOKEN" ]; then
  echo "x-access-token header'ı YOK  (403 → IP sorunu, 401 → şifre/e-posta sorunu)"
  exit 1
fi
echo "x-access-token header'ı var (değer gösterilmiyor)"

echo; echo "== 2) JWT claim adları (değerler gösterilmiyor)"
payload=$(printf '%s' "$TOKEN" | cut -d. -f2 | tr '_-' '/+')
case $(( ${#payload} % 4 )) in 2) payload="$payload==";; 3) payload="$payload=";; esac
printf '%s' "$payload" | base64 -d 2>/dev/null | grep -o '"[^"]*"[[:space:]]*:' | tr -d '":' | tr '\n' ' '
echo
exp=$(printf '%s' "$payload" | base64 -d 2>/dev/null | grep -o '"exp"[[:space:]]*:[[:space:]]*[0-9]*' | grep -o '[0-9]*$')
iat=$(printf '%s' "$payload" | base64 -d 2>/dev/null | grep -o '"iat"[[:space:]]*:[[:space:]]*[0-9]*' | grep -o '[0-9]*$')
[ -n "$exp" ] && [ -n "$iat" ] && echo "Token ömrü: $(( (exp - iat) / 60 )) dakika"

if [ -n "$TAXID" ]; then
  echo; echo "== 3) GET /api/invoice/taxpayers/$TAXID"
  curl -4 -s --max-time 20 -w "\nHTTP %{http_code}\n" \
    "$BASE/api/invoice/taxpayers/$TAXID" -H "x-access-token: $TOKEN" | head -c 1500
  echo
else
  echo; echo "3) Mükellef sorgusu atlandı. Denemek için: bash scripts/stage-smoke.sh <VKN_veya_TCKN>"
fi
