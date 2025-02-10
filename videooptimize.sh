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
video_codec="$(ffprobe -v error -select_streams v:0 -show_entries stream=codec_name -of default=noprint_wrappers=1:nokey=1 "$INPUT_PATH")"
audio_codec="$(ffprobe -v error -select_streams a:0 -show_entries stream=codec_name -of default=noprint_wrappers=1:nokey=1 "$INPUT_PATH")"
if { test "$video_codec" = "av1" || test "$video_codec" = "vp9" } && test "$audio_codec" = "opus"; then
  mv "$INPUT_PATH" "$OUTPUT_PATH"
elif test "$video_codec" = "h264" && test "$audio_codec" = "aac"; then
  ffmpeg -hide_banner -loglevel error -i "$INPUT_PATH" -codec copy -movflags +faststart "$OUTPUT_PATH"
else
  ffmpeg -hide_banner -loglevel error -i "$INPUT_PATH" -codec:v libx264 -preset medium -crf 23 -codec:a aac -b:a 128k -filter:v "format=yuv420p" -movflags +faststart "$OUTPUT_PATH"
fi
