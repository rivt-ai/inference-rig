<script lang="ts">
  import { buildDiffPreview } from '../../lib/diff';
  let { original, current }: { original: string; current: string } = $props();
</script>

<pre class="diff-preview">{buildDiffPreview(original, current)}</pre>
