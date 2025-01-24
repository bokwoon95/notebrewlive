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
*.svg)
  mv "$INPUT_PATH" "$OUTPUT_PATH"
  exit 0
  ;;
esac
width="$(vipsheader "$INPUT_PATH" -f width)"
height="$(vipsheader "$INPUT_PATH" -f height)"
scale_factor="$(awk -v "width=$width" -v "height=$height" "BEGIN {
  aspect_ratio = width / height;
  is_tall_img = aspect_ratio < 9 / 16;
  if (is_tall_img || width > height) {
    if (width <= 1080) {
      print 1;
    } else {
      print 1080 / width;
    }
  } else {
    if (height <= 1080) {
      print 1;
    } else {
      print 1080 / height;
    }
  }
}")"
if test "$scale_factor" = "1"; then
  mv "$INPUT_PATH" "$OUTPUT_PATH"
else
  vips resize "$INPUT_PATH" "$OUTPUT_PATH" "$scale_factor"
  # echo "$INPUT_PATH: input_width = $width, input_height = $height"
  # echo "$OUTPUT_PATH: output_width = $(vipsheader "$OUTPUT_PATH" -f width), output_height = $(vipsheader "$OUTPUT_PATH" -f height)"
fi
