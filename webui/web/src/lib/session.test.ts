import { describe, expect, it, vi } from 'vitest';
import { isLoopbackHost, takeLaunchToken } from './session';

function fakeLocation(hash: string, pathname = '/', search = '') {
  return { hash, pathname, search } as Location;
}

describe('takeLaunchToken', () => {
  it('reads the token `inferencerig web` puts in the launch URL', () => {
    const replaceState = vi.fn();
    const token = takeLaunchToken(fakeLocation('#token=abc123'), { replaceState } as unknown as History);

    expect(token).toBe('abc123');
  });

  // A token left in the address bar survives in history, bookmarks and shared
  // screenshots, which is a leaked credential.
  it('strips the fragment without losing the path or query', () => {
    const replaceState = vi.fn();
    takeLaunchToken(fakeLocation('#token=abc123', '/app', '?section=logs'), { replaceState } as unknown as History);

    expect(replaceState).toHaveBeenCalledWith(null, '', '/app?section=logs');
  });

  it('leaves an ordinary visit alone', () => {
    const replaceState = vi.fn();

    expect(takeLaunchToken(fakeLocation(''), { replaceState } as unknown as History)).toBe('');
    expect(takeLaunchToken(fakeLocation('#section=logs'), { replaceState } as unknown as History)).toBe('');
    expect(takeLaunchToken(fakeLocation('#token='), { replaceState } as unknown as History)).toBe('');
    expect(replaceState).not.toHaveBeenCalled();
  });
});

describe('isLoopbackHost', () => {
  it('separates the local single-user case from a networked one', () => {
    expect(isLoopbackHost('localhost')).toBe(true);
    expect(isLoopbackHost('127.0.0.1')).toBe(true);
    expect(isLoopbackHost('[::1]')).toBe(true);
    expect(isLoopbackHost('192.168.1.5')).toBe(false);
    expect(isLoopbackHost('rig.example')).toBe(false);
  });
});
