<script lang="ts">
	import type { PageData } from './$types';
	import CampaignRun from '#lib/components/CampaignRun.svelte';

	let { data }: { data: PageData } = $props();
</script>

<!-- {#key} пересоздаёт компонент при переходе между кампаниями из сайдбара -->
{#key data.id}
	<CampaignRun id={data.id} />
{/key}
