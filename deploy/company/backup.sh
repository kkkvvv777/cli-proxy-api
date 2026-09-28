#!/bin/sh
set -eu
umask 077
cd "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
: "${BACKUP_PASSWORD_FILE:?Path to a restricted backup encryption password file}"
test -r "$BACKUP_PASSWORD_FILE"
mkdir -p backups
stamp=$(date -u +%Y%m%dT%H%M%SZ)
archive="backups/company-${stamp}.tar.gz.enc"
temporary=$(mktemp -d backups/.backup-XXXXXX)
cleanup() {
    rm -f "$temporary/archive.tar.gz"
    rmdir "$temporary"
    docker compose up -d gateway >/dev/null
}
trap cleanup EXIT
docker compose stop gateway
tar -czf "$temporary/archive.tar.gz" runtime .env
openssl enc -aes-256-cbc -salt -pbkdf2 -iter 200000 \
    -pass "file:$BACKUP_PASSWORD_FILE" -in "$temporary/archive.tar.gz" -out "$archive"
shasum -a 256 "$archive" > "$archive.sha256"
printf 'Encrypted backup: %s\n' "$archive"
