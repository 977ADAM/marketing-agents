<script lang="ts">
	import { resolve } from '$app/paths';
	import Skeleton from './Skeleton.svelte';
	import StatusChip from './StatusChip.svelte';
	import { reviewCursor, historyLoading, loadMoreHistory, historyLoaded, reviews } from '#lib/stores/history.js';
</script>

{#if !$historyLoaded}
	<Skeleton height={18} />
{:else if $reviews.length === 0}
	<p class="muted">Пока пусто</p>
{:else}
	<ul class="history">
		{#each $reviews as r (r.id)}
			<li>
				<a href={resolve('/reviews/[id]', { id: r.id })}>
					<span class="hist-title">{r.brief_title || 'Без названия'}</span>
					<StatusChip status={r.status} runningLabel="Проверка" />
				</a>
			</li>
		{/each}
	</ul>
{/if}

{#if $reviewCursor}
 <button type="button" disabled={$historyLoading} onclick={() => void loadMoreHistory('review')}>Загрузить ещё</button>
{/if}
