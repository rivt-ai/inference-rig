import { describe, expect, it } from 'vitest';
import { canApplyDownload, chooseProfileSelection, isTerminalDownloadState } from './tasks';

describe('download states', () => {
  it('knows terminal and apply states', () => {
    expect(isTerminalDownloadState('completed')).toBe(true);
    expect(isTerminalDownloadState('running')).toBe(false);
    expect(canApplyDownload('already_downloaded')).toBe(true);
    expect(canApplyDownload('failed')).toBe(false);
  });
});

describe('chooseProfileSelection', () => {
  const profiles = [{ name: 'default' }, { name: 'active' }, { name: 'other' }];

  it('prefers explicit selection', () => {
    expect(chooseProfileSelection(profiles, 'other', ['active'], 'default', '')).toBe('other');
  });

  it('falls back through active, default, selected, first', () => {
    expect(chooseProfileSelection(profiles, '', ['active'], 'default', '')).toBe('active');
    expect(chooseProfileSelection(profiles, '', [], 'default', '')).toBe('default');
    expect(chooseProfileSelection(profiles, '', [], '', 'other')).toBe('other');
    expect(chooseProfileSelection(profiles, '', [], '', '')).toBe('default');
  });

  it('returns nothing when no profiles exist', () => {
    expect(chooseProfileSelection([], 'other', ['active'], 'default', 'x')).toBe('');
  });
});
