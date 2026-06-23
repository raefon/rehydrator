#!/usr/bin/env bash
set -euo pipefail

OUT="${1:-rehydrator-preroll.mp4}"
TEXT="${TEXT:-Rehydrating media cache...\nIf playback does not start, wait a minute and press Play again.}"
DURATION="${DURATION:-8}"
SIZE="${SIZE:-1920x1080}"
FPS="${FPS:-30}"

if ! command -v ffmpeg >/dev/null 2>&1; then
  echo "ffmpeg is required" >&2
  exit 1
fi

ffmpeg -y \
  -f lavfi -i "color=c=black:s=${SIZE}:r=${FPS}:d=${DURATION}" \
  -vf "drawtext=text='${TEXT}':fontcolor=white:fontsize=56:x=(w-text_w)/2:y=(h-text_h)/2:line_spacing=18" \
  -c:v libx264 -pix_fmt yuv420p -movflags +faststart \
  "$OUT"

echo "Wrote $OUT"
