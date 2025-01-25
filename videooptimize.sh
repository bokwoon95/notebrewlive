#!/usr/bin/env bash
set -e
set -u
set -o pipefail
INPUT_PATH="$1"
OUTPUT_PATH="$2"
if test ! -f "$INPUT_PATH"; then
  echo "INPUT_PATH $INPUT_PATH does not exist"
  exit 1
fi
case "$INPUT_PATH" in
*.webm)
  mv "$INPUT_PATH" "$OUTPUT_PATH"
  exit 0
  ;;
esac
ffmpeg -hide_banner -loglevel error -i "$INPUT_PATH" -codec copy -movflags +faststart "$OUTPUT_PATH"
