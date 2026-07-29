import '@testing-library/jest-dom/vitest';
import { afterEach } from 'vitest';

// bits-ui's body scroll lock schedules a 24ms setTimeout to reset the body
// style when a dialog unmounts. Let those timers drain before vitest tears
// down jsdom, otherwise the callback runs with `document` gone and throws
// "document is not defined".
afterEach(async () => {
  await new Promise((resolve) => setTimeout(resolve, 30));
});

Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false
  })
});

class TestResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}

Object.defineProperty(globalThis, 'ResizeObserver', { writable: true, value: TestResizeObserver });
