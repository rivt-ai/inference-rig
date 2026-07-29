import { render } from '@testing-library/svelte';
import { describe, expect, it } from 'vitest';
import ResourceMeter from './ResourceMeter.svelte';

function indicator(container: HTMLElement) {
  return container.querySelector('[data-slot="progress-indicator"]') as HTMLElement;
}

describe('ResourceMeter', () => {
  it('renders the label, percentage, and detail', () => {
    const { getByText } = render(ResourceMeter, { label: 'CPU', percent: 34.6, detail: '12 cores' });
    expect(getByText('CPU')).toBeInTheDocument();
    expect(getByText('35% · 12 cores')).toBeInTheDocument();
  });

  it('shades the bar green/yellow/red by load', () => {
    expect(indicator(render(ResourceMeter, { label: 'a', percent: 30 }).container).className).toContain('bg-success');
    expect(indicator(render(ResourceMeter, { label: 'b', percent: 75 }).container).className).toContain('bg-warning');
    expect(indicator(render(ResourceMeter, { label: 'c', percent: 95 }).container).className).toContain('bg-destructive');
  });

  it('clamps out-of-range and missing percentages', () => {
    expect(render(ResourceMeter, { label: 'a', percent: 250 }).getByText('100%')).toBeInTheDocument();
    expect(render(ResourceMeter, { label: 'b', percent: undefined }).getByText('0%')).toBeInTheDocument();
  });
});
