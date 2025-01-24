#!/bin/bash
set -e
set -u
set -o pipefail
sudo apt update
sudo apt install -y wget build-essential libc6-dev libvips libvips-dev gpac ffmpeg
wget https://raw.githubusercontent.com/FFmpeg/FFmpeg/master/tools/qt-faststart.c
gcc -O3 -o /usr/local/bin/qt-faststart qt-faststart.c
rm qt-faststart.c
