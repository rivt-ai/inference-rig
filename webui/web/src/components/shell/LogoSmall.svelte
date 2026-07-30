<script lang="ts">
  let { class: className = '' }: { class?: string } = $props();
</script>

<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 210" role="img" aria-labelledby="logo-small-title" class={className}>
  <title id="logo-small-title">InferenceRig icon</title>

  <text
    x="100"
    y="146"
    text-anchor="middle"
    font-family="Inter Variable, Inter, system-ui, sans-serif"
    font-size="150"
    font-weight="600"
    letter-spacing="-4"
  ><tspan fill="currentColor">i</tspan><tspan fill="var(--primary)">r</tspan></text>
</svg>
