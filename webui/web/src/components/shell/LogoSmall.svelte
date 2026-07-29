<script lang="ts">
  let { class: className = '' }: { class?: string } = $props();
</script>

<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 210" role="img" aria-labelledby="logo-small-title" class={className}>
  <title id="logo-small-title">InferenceRig icon</title>

  <defs>
    <clipPath id="logo-small-left-half">
      <rect x="0" y="0" width="92" height="210" />
    </clipPath>
  </defs>

  <g clip-path="url(#logo-small-left-half)" fill="var(--primary)">
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
</svg>
