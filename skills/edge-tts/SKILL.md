---
name: edge-tts
description: |
  Text-to-speech using Microsoft Edge's neural TTS service (free, no API key).
  Supports multiple voices, languages, speed/pitch/volume adjustment, and subtitle generation.
  Use when user requests audio/voice output, or mentions "tts" keyword.
requires_bins: node,npm
---

# Edge-TTS Skill

Generate high-quality text-to-speech audio using Microsoft Edge's neural TTS service via node-edge-tts. Free, no API key required.

## Quick Start

When user requests TTS conversion, use `shell_exec` to run the converter script:

```bash
# First time: install dependencies
cd <SKILL_DIR>/scripts && npm install --production

# Convert text to speech
node <SKILL_DIR>/scripts/tts-converter.js "Your text here" --voice zh-CN-XiaoxiaoNeural --output /tmp/output.mp3
```

Replace `<SKILL_DIR>` with the actual skill directory path (use `pwd` or the skill's location).

## Trigger Detection

Recognize these as TTS requests:
- "tts", "text to speech", "text-to-speech"
- User explicitly asks to convert text to audio/voice/speech
- User wants content read aloud

## Usage

### Basic Conversion
```bash
node scripts/tts-converter.js "Hello world" --output hello.mp3
```

### With Voice and Rate
```bash
node scripts/tts-converter.js "Hello world" \
  --voice en-US-AriaNeural \
  --rate +10% \
  --output hello.mp3
```

### List Available Voices
```bash
node scripts/tts-converter.js --list-voices
```

### Configuration Manager
```bash
# Set default voice
node scripts/config-manager.js --set-voice zh-CN-XiaoxiaoNeural

# Set default rate
node scripts/config-manager.js --set-rate +10%

# View current settings
node scripts/config-manager.js --get

# Reset to defaults
node scripts/config-manager.js --reset
```

## CLI Options

| Option | Short | Description | Default |
|--------|-------|-------------|---------|
| `--voice` | `-v` | Voice name | `en-US-MichelleNeural` |
| `--lang` | `-l` | Language code | `en-US` |
| `--format` | `-o` | Audio format | `audio-24khz-48kbitrate-mono-mp3` |
| `--pitch` | | Pitch adjustment | `default` |
| `--rate` | `-r` | Speed adjustment | `default` |
| `--volume` | | Volume adjustment | `default` |
| `--save-subtitles` | `-s` | Save subtitles JSON | `false` |
| `--output` | `-f` | Output file path | temp file |
| `--proxy` | `-p` | Proxy URL | - |
| `--timeout` | | Timeout in ms | `10000` |
| `--list-voices` | `-L` | List voices | - |

## Common Voices

**Chinese:**
- `zh-CN-XiaoxiaoNeural` - female, natural (recommended for Chinese)
- `zh-CN-YunyangNeural` - male, natural

**English:**
- `en-US-MichelleNeural` - female, natural (default)
- `en-US-AriaNeural` - female, natural
- `en-US-GuyNeural` - male, natural
- `en-GB-SoniaNeural` - female, British
- `en-GB-RyanNeural` - male, British

**Other:**
- `ja-JP-NanamiNeural` - Japanese female
- `ko-KR-SunHiNeural` - Korean female
- `fr-FR-DeniseNeural` - French female
- `de-DE-KatjaNeural` - German female
- `es-ES-ElviraNeural` - Spanish female

## Rate Guidelines

| Value | Effect | Scenario |
|-------|--------|----------|
| `-20%` ~ `-10%` | Slow | Tutorials, stories, accessibility |
| `default` | Normal | General use |
| `+10%` ~ `+20%` | Slightly fast | Summaries |
| `+30%` ~ `+50%` | Fast | News, efficiency |

## Output Formats

| Format | Quality | Use Case |
|--------|---------|----------|
| `audio-24khz-48kbitrate-mono-mp3` | Standard | Voice notes, messages |
| `audio-24khz-96kbitrate-mono-mp3` | High | Presentations |
| `audio-48khz-96kbitrate-stereo-mp3` | Highest | Professional audio |

## Workflow

1. Detect TTS intent from user message
2. Extract text to convert (filter out "tts" keywords)
3. Determine voice based on text language (auto-detect or user preference)
4. Run `tts-converter.js` via `shell_exec`
5. Return `MEDIA: /path/to/output.mp3` so the channel can deliver audio

## Installation

Dependencies are installed automatically on first use, or manually:

```bash
cd skills/edge-tts/scripts
npm install --production
```

Requires: Node.js >= 14, npm, internet connection.

## Notes

- Free service, no API key needed
- Neural voices (ending in `Neural`) provide the best quality
- Requires internet connection (Microsoft Edge online service)
- Output is MP3 by default
- Supports subtitle generation (JSON with word-level timing)
- Test voices at: https://tts.travisvn.com/
