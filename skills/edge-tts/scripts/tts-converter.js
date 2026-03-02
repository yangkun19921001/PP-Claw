#!/usr/bin/env node
/**
 * TTS Converter using node-edge-tts
 *
 * Converts text to speech using Microsoft Edge's online TTS service.
 * Supports multiple voices, languages, speeds, and output formats.
 *
 * Usage:
 *   node tts-converter.js "Your text here" --voice en-US-AriaNeural --rate +10% --output audio.mp3
 */

const { EdgeTTS } = require('node-edge-tts');
const { program } = require('commander');
const fs = require('fs/promises');
const path = require('path');
const os = require('os');

// Constants
const DEFAULT_TIMEOUT_MS = 10000;
const MAX_TEXT_LENGTH = 10000;
const TEMP_DIR = path.join(os.tmpdir(), 'edge-tts-temp');

// Default voice configurations
const DEFAULT_VOICES = {
  en: 'en-US-MichelleNeural',
  es: 'es-ES-ElviraNeural',
  fr: 'fr-FR-DeniseNeural',
  de: 'de-DE-KatjaNeural',
  it: 'it-IT-ElsaNeural',
  ja: 'ja-JP-NanamiNeural',
  zh: 'zh-CN-XiaoxiaoNeural',
  ko: 'ko-KR-SunHiNeural',
  ar: 'ar-SA-ZariyahNeural',
};

/**
 * Validate prosody value (pitch, rate, volume)
 */
function validateProsodyValue(value) {
  if (value === 'default') return true;
  if (typeof value === 'string' && value.endsWith('%')) {
    const num = parseInt(value);
    return !isNaN(num) && num >= -100 && num <= 100;
  }
  return false;
}

/**
 * Ensure temp directory exists
 */
async function ensureTempDir() {
  try {
    await fs.access(TEMP_DIR);
  } catch (error) {
    await fs.mkdir(TEMP_DIR, { recursive: true });
  }
}

/**
 * Generate unique temporary file path
 */
function generateTempPath(extension = '.mp3') {
  const timestamp = Date.now();
  const random = Math.random().toString(36).substring(2, 8);
  const filename = `tts_${timestamp}_${random}${extension}`;
  return path.join(TEMP_DIR, filename);
}

/**
 * Convert text to speech
 */
async function textToSpeech(text, options = {}) {
  const {
    voice,
    lang = 'en-US',
    outputFormat = 'audio-24khz-48kbitrate-mono-mp3',
    pitch = 'default',
    rate = 'default',
    volume = 'default',
    saveSubtitles = false,
    outputPath = null,
    proxy,
    timeout = DEFAULT_TIMEOUT_MS,
  } = options;

  if (!text || typeof text !== 'string' || text.trim().length === 0) {
    throw new Error('Text cannot be empty');
  }

  if (text.length > MAX_TEXT_LENGTH) {
    console.warn(`Warning: Text is very long (${text.length} characters), may cause issues`);
  }

  if (!validateProsodyValue(pitch)) {
    throw new Error(`Invalid pitch value: "${pitch}". Must be "default" or percentage (e.g., "+10%", "-20%")`);
  }
  if (!validateProsodyValue(rate)) {
    throw new Error(`Invalid rate value: "${rate}". Must be "default" or percentage (e.g., "+10%", "-20%")`);
  }
  if (!validateProsodyValue(volume)) {
    throw new Error(`Invalid volume value: "${volume}". Must be "default" or percentage (e.g., "+10%", "-20%")`);
  }

  const finalVoice = voice || DEFAULT_VOICES[lang.split('-')[0]] || DEFAULT_VOICES.en;

  await ensureTempDir();
  const finalOutputPath = outputPath || generateTempPath('.mp3');

  // Filter out TTS-related keywords from text
  const ttsKeywords = ['tts', 'text-to-speech', 'text to speech'];
  const filteredText = text.split(/\s+/).filter(word => {
    const lowerWord = word.toLowerCase().replace(/[^\w\s-]/g, '');
    return !ttsKeywords.includes(lowerWord);
  }).join(' ');

  if (filteredText !== text.trim()) {
    console.log(`Filtered TTS keywords: "${text}" -> "${filteredText}"`);
  }

  console.log(`Converting text to speech...`);
  console.log(`  Text: ${filteredText.substring(0, 80)}${filteredText.length > 80 ? '...' : ''}`);
  console.log(`  Voice: ${finalVoice}`);
  console.log(`  Rate: ${rate}, Pitch: ${pitch}, Volume: ${volume}`);

  try {
    const tts = new EdgeTTS({
      voice: finalVoice,
      lang,
      outputFormat,
      saveSubtitles,
      proxy,
      timeout,
      pitch,
      rate,
      volume,
    });

    await tts.ttsPromise(filteredText, finalOutputPath);

    const stats = await fs.stat(finalOutputPath);
    console.log(`\nAudio saved: ${finalOutputPath} (${stats.size} bytes)`);

    if (saveSubtitles) {
      const subtitlePath = finalOutputPath.replace(/\.[^/.]+$/, '.json');
      try {
        await fs.stat(subtitlePath);
        console.log(`Subtitles saved: ${subtitlePath}`);
      } catch (e) {
        // no subtitles generated
      }
    }

    return finalOutputPath;
  } catch (error) {
    console.error('Conversion failed:', error.message);
    throw error;
  }
}

/**
 * List available voices
 */
function listVoices() {
  console.log('Common voices by language:\n');

  const voicesByLang = {
    'zh (Chinese)': ['zh-CN-XiaoxiaoNeural (female)', 'zh-CN-YunyangNeural (male)', 'zh-CN-YunxiNeural (male)', 'zh-TW-HsiaoChenNeural (female)'],
    'en (English)': ['en-US-MichelleNeural (female)', 'en-US-AriaNeural (female)', 'en-US-GuyNeural (male)', 'en-GB-SoniaNeural (female)', 'en-GB-RyanNeural (male)'],
    'ja (Japanese)': ['ja-JP-NanamiNeural (female)', 'ja-JP-KeitaNeural (male)'],
    'ko (Korean)': ['ko-KR-SunHiNeural (female)', 'ko-KR-InJoonNeural (male)'],
    'es (Spanish)': ['es-ES-ElviraNeural (female)', 'es-MX-DaliaNeural (female)'],
    'fr (French)': ['fr-FR-DeniseNeural (female)', 'fr-FR-HenriNeural (male)'],
    'de (German)': ['de-DE-KatjaNeural (female)', 'de-DE-ConradNeural (male)'],
    'ar (Arabic)': ['ar-SA-ZariyahNeural (female)', 'ar-SA-HamedNeural (male)'],
  };

  for (const [lang, voices] of Object.entries(voicesByLang)) {
    console.log(`${lang}:`);
    voices.forEach(v => console.log(`  ${v}`));
    console.log('');
  }

  console.log('Voice format: {lang}-{region}-{Name}Neural');
  console.log('Preview voices: https://tts.travisvn.com/');
}

// CLI
program
  .argument('[text]', 'Text to convert to speech')
  .option('-v, --voice <voice>', 'Voice name (e.g., zh-CN-XiaoxiaoNeural)')
  .option('-l, --lang <language>', 'Language code (e.g., en-US, zh-CN)', 'en-US')
  .option('-o, --format <format>', 'Output format', 'audio-24khz-48kbitrate-mono-mp3')
  .option('--pitch <pitch>', 'Pitch adjustment (e.g., +10%, -20%, default)', 'default')
  .option('-r, --rate <rate>', 'Rate adjustment (e.g., +10%, -20%, default)', 'default')
  .option('--volume <volume>', 'Volume adjustment (e.g., +0%, -50%, default)', 'default')
  .option('-s, --save-subtitles', 'Save subtitles as JSON file', false)
  .option('-f, --output <path>', 'Output file path (default: temp file)')
  .option('-p, --proxy <proxy>', 'Proxy URL (e.g., http://localhost:7890)')
  .option('--timeout <ms>', 'Request timeout in milliseconds', '10000')
  .option('-L, --list-voices', 'List available voices')
  .description('Convert text to speech using Microsoft Edge TTS')
  .version('2.0.0');

program.parse(process.argv);
const options = program.opts();
const text = program.args[0];

if (options.listVoices) {
  listVoices();
  process.exit(0);
}

if (!text) {
  console.error('Error: No text provided');
  console.log('Usage: node tts-converter.js "Your text" [options]');
  console.log('Run with --list-voices to see available voices');
  process.exit(1);
}

textToSpeech(text, {
  voice: options.voice,
  lang: options.lang,
  outputFormat: options.format,
  pitch: options.pitch,
  rate: options.rate,
  volume: options.volume,
  saveSubtitles: options.saveSubtitles,
  outputPath: options.output,
  proxy: options.proxy,
  timeout: parseInt(options.timeout),
}).catch(error => {
  console.error('Error:', error.message);
  process.exit(1);
});

module.exports = { textToSpeech, listVoices };
