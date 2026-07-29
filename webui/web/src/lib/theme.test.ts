import { beforeEach, describe, expect, it } from 'vitest';
import { applyPrimaryColors, defaultPrimaryColors, loadPrimaryColors, resetPrimaryColors, savePrimaryColors } from './theme';

describe('primary theme colors', () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.removeAttribute('style');
  });

  it('loads defaults and ignores invalid stored colors', () => {
    localStorage.setItem('inferencerig.theme.primary.light', 'teal');
    expect(loadPrimaryColors(localStorage)).toEqual(defaultPrimaryColors);
  });

  it('persists and applies separate light and dark primaries', () => {
    const colors = { light: '#123456', dark: '#abcdef' };
    savePrimaryColors(localStorage, colors);
    expect(loadPrimaryColors(localStorage)).toEqual(colors);
    expect(document.documentElement.style.getPropertyValue('--user-primary-light')).toBe('#123456');
    expect(document.documentElement.style.getPropertyValue('--user-primary-dark')).toBe('#abcdef');
  });

  it('chooses readable foregrounds and resets overrides', () => {
    applyPrimaryColors({ light: '#000000', dark: '#ffffff' });
    expect(document.documentElement.style.getPropertyValue('--user-primary-light-foreground')).toBe('#ffffff');
    expect(document.documentElement.style.getPropertyValue('--user-primary-dark-foreground')).toBe('#111827');
    expect(resetPrimaryColors(localStorage)).toEqual(defaultPrimaryColors);
  });
});
