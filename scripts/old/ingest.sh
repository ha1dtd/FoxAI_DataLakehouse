#!/usr/bin/env bash
set -euo pipefail

# --------------------------------------------
# FoxAI ingestion helper (enhanced)
# Supports:
#   1) upload local file to MinIO bronze bucket
#   2) download file from URL then upload to MinIO bronze bucket
# Default: raw/{source}/{date}/file
# --------------------------------------------

MINIO_ALIAS="${MINIO_ALIAS:-foxai}"
MINIO_ENDPOINT="${MINIO_ENDPOINT:-http://localhost:9001}"
MINIO_ACCESS_KEY="${MINIO_ACCESS_KEY:-minioadmin}"
MINIO_SECRET_KEY="${MINIO_SECRET_KEY:-minioadmin}"
MINIO_BUCKET="${MINIO_BUCKET:-bronze}"

# Source name MUST be provided via env or detected from filename
SOURCE="${SOURCE:-misc}"  
DATE=$(date +%F)  # current date
INPUT="${1:-}"

usage() {
  cat <<EOF
Usage:
  $0 <local-file-path | http(s)://url>

Environment variables:
  MINIO_ALIAS      MinIO alias (default: foxai)
  MINIO_ENDPOINT   MinIO endpoint (default: http://127.0.0.1:9001)
  MINIO_ACCESS_KEY MinIO access key
  MINIO_SECRET_KEY MinIO secret key
  MINIO_BUCKET     Target bucket (default: bronze)
  SOURCE           Dataset source (e.g., taxi, hr, finance)

Examples:
  SOURCE=taxi $0 ./yellow_tripdata_2025-01.parquet
  SOURCE=finance $0 https://example.com/file.parquet
EOF
}

cleanup() {
  if [[ -n "${TMP_FILE:-}" && -f "$TMP_FILE" ]]; then
    rm -f "$TMP_FILE"
  fi
}
trap cleanup EXIT

if [[ -z "$INPUT" ]]; then
  usage
  exit 1
fi

if ! command -v mc >/dev/null 2>&1; then
  echo "Error: mc (MinIO Client) is not installed."
  exit 1
fi

# Configure alias each run
mc alias set "$MINIO_ALIAS" "$MINIO_ENDPOINT" "$MINIO_ACCESS_KEY" "$MINIO_SECRET_KEY" >/dev/null

TARGET_PATH="$MINIO_ALIAS/$MINIO_BUCKET/raw/$SOURCE/$DATE"

if [[ "$INPUT" =~ ^https?:// ]]; then
  echo "Downloading from URL..."
  TMP_FILE="$(mktemp /tmp/foxai_ingest_XXXXXX)"
  FILENAME="$(basename "${INPUT%%\?*}")"
  [[ -z "$FILENAME" || "$FILENAME" == "/" ]] && FILENAME="downloaded_file"

  if command -v curl >/dev/null 2>&1; then
    curl -L --fail "$INPUT" -o "$TMP_FILE"
  else
    wget -O "$TMP_FILE" "$INPUT"
  fi

  echo "Uploading to MinIO at $TARGET_PATH/$FILENAME ..."
  mc cp "$TMP_FILE" "$TARGET_PATH/$FILENAME"
else
  if [[ ! -f "$INPUT" ]]; then
    echo "Error: local file not found: $INPUT"
    exit 1
  fi

  FILENAME="$(basename "$INPUT")"
  echo "Uploading local file to $TARGET_PATH/$FILENAME ..."
  mc cp "$INPUT" "$TARGET_PATH/$FILENAME"
fi

echo "✅ Done -> $TARGET_PATH/$FILENAME"