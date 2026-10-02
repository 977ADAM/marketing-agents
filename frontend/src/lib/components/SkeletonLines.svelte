<script lang="ts">
	import Skeleton from './Skeleton.svelte';

	let { lines = 3 }: { lines?: number } = $props();
</script>

<div class="skeleton-lines">
	{#each Array.from({ length: lines }, (_, i) => i) as i (i)}
		<Skeleton width={i === lines - 1 ? '60%' : '100%'} />
	{/each}
</div>
