<script lang="ts">
	import { resolve } from '$app/paths';
	import Skeleton from './Skeleton.svelte';
	import StatusChip from './StatusChip.svelte';
	import { campaigns, historyLoaded } from '#lib/stores/history.js';
</script>

{#if !$historyLoaded}
	<Skeleton height={18} />
{:else if $campaigns.length === 0}
	<p class="muted">Пока пусто</p>
{:else}
	<ul class="history">
		{#each $campaigns as c (c.id)}
			<li>
				<a href={resolve('/campaigns/[id]', { id: c.id })}>
					<span class="hist-title">{c.brief.product}</span>
					<StatusChip status={c.status} />
				</a>
			</li>
		{/each}
	</ul>
{/if}
