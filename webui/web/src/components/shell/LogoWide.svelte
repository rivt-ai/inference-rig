<script lang="ts">
  let { class: className = '' }: { class?: string } = $props();
</script>

<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 640 220" role="img" aria-labelledby="logo-wide-title" class={className}>
  <title id="logo-wide-title">InferenceRig horizontal logo</title>

  <defs>
    <clipPath id="logo-wide-left-half">
      <rect x="0" y="0" width="92" height="210" />
    </clipPath>
  </defs>

  <g transform="translate(22 5)">
    <g transform="translate(114 52.5) scale(.5)">
      <g clip-path="url(#logo-wide-left-half)" fill="var(--primary)">
        <circle cx="96" cy="105" r="43" fill="none" stroke="var(--primary)" stroke-width="12" />
        <rect x="144" y="97" width="25" height="16" rx="1" transform="rotate(110 96 105)" />
        <rect x="144" y="97" width="25" height="16" rx="1" transform="rotate(145 96 105)" />
        <rect x="144" y="97" width="25" height="16" rx="1" transform="rotate(180 96 105)" />
        <rect x="144" y="97" width="25" height="16" rx="1" transform="rotate(215 96 105)" />
        <rect x="144" y="97" width="25" height="16" rx="1" transform="rotate(250 96 105)" />
      </g>
      <rect x="92" y="18" width="8" height="174" rx="1.5" fill="currentColor" />
      <path
        d="M120 28V74H164V113H138V129L163 155V192"
        fill="none"
        stroke="currentColor"
        stroke-width="8"
        stroke-linecap="round"
        stroke-linejoin="round"
      />
    </g>
  </g>

  <text
    x="250"
    y="132"
    font-family="Inter Variable, Inter, system-ui, sans-serif"
    font-size="58"
    font-weight="600"
    letter-spacing="-1"
  ><tspan fill="currentColor">inference</tspan><tspan fill="var(--primary)">rig</tspan></text>

</svg>
