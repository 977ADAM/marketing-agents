<script lang="ts">
	import type { PageData } from './$types';
	import ReviewRun from '#lib/components/ReviewRun.svelte';

	let { data }: { data: PageData } = $props();
</script>

{#key data.id}
	<ReviewRun id={data.id} />
{/key}
