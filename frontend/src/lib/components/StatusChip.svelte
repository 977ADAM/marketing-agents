<script lang="ts">
	import type { Status } from '#lib/api/types.js';
	import { statusLabel } from '#lib/labels.js';

	let { status, runningLabel }: { status: Status; runningLabel?: string } = $props();
</script>

<span class="chip chip-{status}">{statusLabel(status, runningLabel)}</span>
