#!/bin/bash
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
*.svg)
  mv "$INPUT_PATH" "$OUTPUT_PATH"
  exit 0
  ;;
esac

# Dependency check with priority order
QT_FASTSTART=$(which qt-faststart 2>/dev/null || true)
MP4BOX=$(which MP4Box 2>/dev/null || true)
FFMPEG=$(which ffmpeg 2>/dev/null || true)

# Check if already optimized (fast atomic check)
check_optimized() {
    if "$QT_FASTSTART" -c "$INPUT" 2>/dev/null; then
        echo "Already optimized" >&2
        exit 0
    fi
}

optimize() {
    # Try fastest methods first
    if [ -n "$QT_FASTSTART" ]; then
        cp "$INPUT" "$OUTPUT"
        "$QT_FASTSTART" "$OUTPUT" "$OUTPUT.tmp" && mv "$OUTPUT.tmp" "$OUTPUT"
    elif [ -n "$MP4BOX" ]; then
        "$MP4BOX" -quiet -fast-start "$INPUT" -out "$OUTPUT"
    elif [ -n "$FFMPEG" ]; then
        "$FFMPEG" -v error -i "$INPUT" -c copy -movflags +faststart "$OUTPUT"
    else
        echo "No optimization tools found!" >&2
        exit 1
    fi
}

# Validate input
file -b --mime-type "$INPUT" | grep -q video/mp4 || {
    echo "Invalid MP4 file" >&2
    exit 1
}

check_optimized
optimize

# Verify output
if ! "$QT_FASTSTART" -c "$OUTPUT"; then
    echo "Optimization failed" >&2
    rm "$OUTPUT"
    exit 1
fi

echo "Optimized: $OUTPUT"
