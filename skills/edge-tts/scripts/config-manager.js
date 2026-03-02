#!/usr/bin/env node
/**
 * TTS Configuration Manager
 *
 * Manages user preferences for TTS settings.
 * Stores config in ~/.tts-config.json.
 *
 * Usage:
 *   node config-manager.js --set-voice zh-CN-XiaoxiaoNeural
 *   node config-manager.js --get
 *   node config-manager.js --reset
 */

const fs = require('fs/promises');
const path = require('path');
const { program } = require('commander');

const DEFAULT_CONFIG_PATH = path.join(require('os').homedir(), '.tts-config.json');

const DEFAULT_CONFIG = {
  voice: 'en-US-MichelleNeural',
  lang: 'en-US',
  outputFormat: 'audio-24khz-48kbitrate-mono-mp3',
  pitch: 'default',
  rate: 'default',
  volume: 'default',
  saveSubtitles: false,
  proxy: '',
  timeout: 10000,
};

async function loadConfig(configPath = DEFAULT_CONFIG_PATH) {
  try {
    const data = await fs.readFile(configPath, 'utf-8');
    return { ...DEFAULT_CONFIG, ...JSON.parse(data) };
  } catch (error) {
    return { ...DEFAULT_CONFIG };
  }
}

async function saveConfig(config, configPath = DEFAULT_CONFIG_PATH) {
  try {
    await fs.writeFile(configPath, JSON.stringify(config, null, 2));
    return true;
  } catch (error) {
    console.error('Error saving config:', error.message);
    return false;
  }
}

async function setConfig(key, value, configPath = DEFAULT_CONFIG_PATH) {
  const config = await loadConfig(configPath);
  config[key] = value;
  return saveConfig(config, configPath);
}

async function resetConfig(configPath = DEFAULT_CONFIG_PATH) {
  try {
    await fs.unlink(configPath);
  } catch (error) {
    // file doesn't exist
  }
  return { ...DEFAULT_CONFIG };
}

// CLI
program
  .option('--config-path <path>', 'Path to config file')
  .option('-g, --get [key]', 'Get config value (or all)')
  .option('--set-voice <voice>', 'Set default voice')
  .option('--set-lang <lang>', 'Set default language')
  .option('--set-format <format>', 'Set default output format')
  .option('--set-pitch <pitch>', 'Set default pitch')
  .option('--set-rate <rate>', 'Set default rate')
  .option('--set-volume <volume>', 'Set default volume')
  .option('--set-proxy <proxy>', 'Set proxy URL')
  .option('--set-timeout <ms>', 'Set timeout in ms')
  .option('--toggle-subtitles', 'Toggle subtitle saving')
  .option('--reset', 'Reset to defaults')
  .description('Manage TTS configuration')
  .version('2.0.0');

program.parse(process.argv);
const options = program.opts();
const configPath = options.configPath || DEFAULT_CONFIG_PATH;

async function main() {
  if (options.reset) {
    const config = await resetConfig(configPath);
    console.log('Configuration reset to defaults');
    console.log(JSON.stringify(config, null, 2));
    return;
  }

  if (options.get !== undefined) {
    const config = await loadConfig(configPath);
    if (typeof options.get === 'string') {
      console.log(JSON.stringify(config[options.get], null, 2));
    } else {
      console.log(JSON.stringify(config, null, 2));
    }
    return;
  }

  const setters = {
    setVoice: 'voice',
    setLang: 'lang',
    setFormat: 'outputFormat',
    setPitch: 'pitch',
    setRate: 'rate',
    setVolume: 'volume',
    setProxy: 'proxy',
  };

  for (const [opt, key] of Object.entries(setters)) {
    if (options[opt]) {
      await setConfig(key, options[opt], configPath);
      console.log(`Set ${key} = ${options[opt]}`);
      return;
    }
  }

  if (options.setTimeout) {
    await setConfig('timeout', parseInt(options.setTimeout), configPath);
    console.log(`Set timeout = ${options.setTimeout}ms`);
    return;
  }

  if (options.toggleSubtitles) {
    const config = await loadConfig(configPath);
    await setConfig('saveSubtitles', !config.saveSubtitles, configPath);
    console.log(`Toggled subtitles: ${!config.saveSubtitles}`);
    return;
  }

  // Show current config
  const config = await loadConfig(configPath);
  console.log(JSON.stringify(config, null, 2));
}

main().catch(error => {
  console.error('Error:', error.message);
  process.exit(1);
});

module.exports = { loadConfig, saveConfig, setConfig, resetConfig };
