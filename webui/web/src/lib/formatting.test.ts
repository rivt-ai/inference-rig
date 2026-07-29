import { describe, expect, it } from 'vitest';
import { basename, formatBytes, formatDate } from './formatting';

describe('formatting', () => {
  it('gets basenames for unix and windows paths', () => {
    expect(basename('/models/a.gguf')).toBe('a.gguf');
    expect(basename('C:\\models\\b.gguf')).toBe('b.gguf');
  });

  it('formats bytes', () => {
    expect(formatBytes(512)).toBe('512 B');
    expect(formatBytes(1536)).toBe('1.5 KB');
  });

  it('keeps invalid dates readable', () => {
    expect(formatDate('not-date')).toBe('not-date');
  });
});
