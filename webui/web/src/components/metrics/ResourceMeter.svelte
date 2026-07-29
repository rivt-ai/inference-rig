<script lang="ts">
  import { Progress } from '$lib/components/ui/progress';

  type Props = { label: string; percent: number | undefined | null; detail?: string; history?: number[] };
  let { label, percent, detail, history = [] }: Props = $props();

  const clamped = $derived(Math.max(0, Math.min(100, percent || 0)));
  const loadTone = $derived(clamped >= 90 ? 'destructive' : clamped >= 70 ? 'warning' : 'success');
  // Shade the bar by load so utilisation reads from colour and length, mirroring
  // the green/yellow/red thresholds used by the TUI resource meters.
  const indicatorClass = $derived(
    loadTone === 'destructive' ? 'bg-destructive' : loadTone === 'warning' ? 'bg-warning' : 'bg-success'
  );
  const sparklineClass = $derived(
    loadTone === 'destructive' ? 'text-destructive' : loadTone === 'warning' ? 'text-warning' : 'text-success'
  );
  const sparklinePoints = $derived(
    history.length < 2
      ? ''
      : history
          .map((value, index) => {
            const x = (index / (history.length - 1)) * 100;
            const y = 100 - Math.max(0, Math.min(100, value || 0));
            return `${x},${y}`;
          })
          .join(' ')
  );
</script>

<div class="space-y-1.5">
  <div class="flex items-baseline justify-between gap-2 text-sm">
    <span class="font-medium">{label}</span>
    <span class="flex items-center gap-2 text-muted-foreground tabular-nums">
      <span>{clamped.toFixed(0)}%{detail ? ` · ${detail}` : ''}</span>
      {#if sparklinePoints}
        <span class="rounded border border-border bg-muted/40 px-1 py-0.5" title="trend">
          <svg class={`block h-4 w-16 overflow-visible ${sparklineClass}`} viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true">
            <polyline
              points={sparklinePoints}
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              stroke-linecap="round"
              stroke-linejoin="round"
              vector-effect="non-scaling-stroke"
            />
          </svg>
        </span>
      {/if}
    </span>
  </div>
  <Progress value={clamped} class="h-2" {indicatorClass} />
</div>
