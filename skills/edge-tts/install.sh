#!/bin/bash
# Edge-TTS Skill Installation Script

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

echo "Installing Edge-TTS Skill dependencies..."
echo ""

cd "$SCRIPT_DIR/scripts"
npm install --production

echo ""
echo "Edge-TTS Skill installed successfully!"
echo ""
echo "Test:   node $SCRIPT_DIR/scripts/tts-converter.js \"Hello\" --output /tmp/test.mp3"
echo "Config: node $SCRIPT_DIR/scripts/config-manager.js --help"
