import { describe, expect, it } from 'vitest';
import { buildDiffPreview } from './diff';

describe('buildDiffPreview', () => {
  it('reports no changes', () => {
    expect(buildDiffPreview('a', 'a')).toBe('No changes.');
  });

  it('shows changed lines', () => {
    expect(buildDiffPreview('a\nb', 'a\nc')).toContain('-2: b');
    expect(buildDiffPreview('a\nb', 'a\nc')).toContain('+2: c');
  });
});
