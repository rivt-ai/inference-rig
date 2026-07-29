<script lang="ts">
  import { AreaChart } from 'layerchart';

  type Point = { capturedAt: string; value: number | null };
  type Props = { label: string; points: Point[]; unit?: string; maximum?: number };
  let { label, points, unit = '%', maximum = 100 }: Props = $props();

  const data = $derived(
    points
      .filter((point): point is { capturedAt: string; value: number } => point.value != null)
      .map((point) => ({ capturedAt: new Date(point.capturedAt), value: point.value }))
  );
  const current = $derived(data[data.length - 1]?.value);
</script>

<div class="space-y-3" aria-label={`${label} trend over the last five minutes`}>
  <div class="flex items-baseline justify-between gap-3">
    <div><p class="text-sm font-medium">{label}</p><p class="text-xs text-muted-foreground">Last 5 minutes</p></div>
    <span class="font-mono text-sm tabular-nums text-muted-foreground">{current == null ? '—' : `${current.toFixed(unit === '°C' ? 0 : 1)}${unit}`}</span>
  </div>
  {#if data.length >= 2}
    <div class="h-32">
      <AreaChart
        {data}
        x="capturedAt"
        y="value"
        yDomain={[0, maximum]}
        props={{
          area: { class: 'fill-primary/15', line: { class: 'stroke-primary stroke-2' } },
          tooltip: { context: { mode: 'bisect-x' } }
        }}
      />
    </div>
  {:else}
    <div class="flex h-32 items-center justify-center rounded-md border border-dashed text-sm text-muted-foreground">Collecting data…</div>
  {/if}
</div>
