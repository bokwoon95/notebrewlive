#!/bin/bash
set -e
set -u
set -o pipefail
sudo apt update
sudo apt install -y build-essential pkg-config g++ git cmake yasm libc6-dev
sudo apt install -y wget libvips libvips-dev ffmpeg

# qt-faststart
wget https://raw.githubusercontent.com/FFmpeg/FFmpeg/master/tools/qt-faststart.c
gcc -O3 -o /usr/local/bin/qt-faststart qt-faststart.c
rm qt-faststart.c

# gpac MP4Box
sudo apt install -y zlib1g-dev libfreetype6-dev libjpeg62-turbo-dev libpng-dev libmad0-dev libfaad-dev libogg-dev libvorbis-dev libtheora-dev liba52-0.7.4-dev libavcodec-dev libavformat-dev libavutil-dev libswscale-dev libavdevice-dev libnghttp2-dev libopenjp2-7-dev libcaca-dev libxv-dev x11proto-video-dev libgl1-mesa-dev libglu1-mesa-dev x11proto-gl-dev libxvidcore-dev libssl-dev libjack-jackd2-dev libasound2-dev libpulse-dev libsdl2-dev dvb-apps mesa-utils
git clone https://github.com/gpac/gpac.git
pushd gpac
./configure
make
sudo make install
popd
