export const defaultPrimaryColors = {
  light: '#08766d',
  dark: '#6dd8c8'
} as const;

const lightKey = 'inferencerig.theme.primary.light';
const darkKey = 'inferencerig.theme.primary.dark';

export type PrimaryColors = { light: string; dark: string };

export function loadPrimaryColors(storage: Storage): PrimaryColors {
  return {
    light: validColor(storage.getItem(lightKey)) || defaultPrimaryColors.light,
    dark: validColor(storage.getItem(darkKey)) || defaultPrimaryColors.dark
  };
}

export function savePrimaryColors(storage: Storage, colors: PrimaryColors) {
  storage.setItem(lightKey, colors.light);
  storage.setItem(darkKey, colors.dark);
  applyPrimaryColors(colors);
}

export function resetPrimaryColors(storage: Storage) {
  storage.removeItem(lightKey);
  storage.removeItem(darkKey);
  applyPrimaryColors(defaultPrimaryColors);
  return { ...defaultPrimaryColors };
}

export function applyPrimaryColors(colors: PrimaryColors, root = document.documentElement) {
  root.style.setProperty('--user-primary-light', colors.light);
  root.style.setProperty('--user-primary-dark', colors.dark);
  root.style.setProperty('--user-primary-light-foreground', contrastColor(colors.light));
  root.style.setProperty('--user-primary-dark-foreground', contrastColor(colors.dark));
}

function validColor(value: string | null) {
  return value && /^#[0-9a-f]{6}$/i.test(value) ? value : '';
}

function contrastColor(hex: string) {
  const channels = [1, 3, 5].map((offset) => Number.parseInt(hex.slice(offset, offset + 2), 16) / 255);
  const luminance = channels
    .map((channel) => (channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4))
    .reduce((sum, channel, index) => sum + channel * [0.2126, 0.7152, 0.0722][index], 0);
  const whiteContrast = 1.05 / (luminance + 0.05);
  const blackContrast = (luminance + 0.05) / 0.05;
  return whiteContrast >= blackContrast ? '#ffffff' : '#111827';
}
