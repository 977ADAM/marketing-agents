<script lang="ts">
	import type { Severity } from '#lib/api/types.js';

	let { score, severity }: { score: number; severity: Severity } = $props();
</script>

<span class="score score-{severity}">{score}</span>
