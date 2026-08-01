<script lang="ts">
  let { class: className = '' }: { class?: string } = $props();
</script>

<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 440 220" role="img" aria-labelledby="logo-wide-title" class={className}>
  <title id="logo-wide-title">InferenceRig</title>

  <text
    x="12"
    y="132"
    font-family="Inter Variable, Inter, system-ui, sans-serif"
    font-size="58"
    font-weight="600"
    letter-spacing="-1"
  ><tspan fill="currentColor">inference</tspan><tspan fill="var(--primary)">rig</tspan></text>

</svg>
