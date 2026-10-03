<script lang="ts">
	import { onDestroy } from 'svelte';
	import ArticleCard from './ArticleCard.svelte';
	import ErrorState from './ErrorState.svelte';
	import ProgressPanel from './ProgressPanel.svelte';
	import SkeletonLines from './SkeletonLines.svelte';
	import TrajectoryPanel from './TrajectoryPanel.svelte';
	import { formatCost } from '#lib/format.js';
	import { refreshHistory } from '#lib/stores/history.js';
	import { campaignRun } from '#lib/stores/run.js';

	// Компонент создаётся заново на каждый id (см. {#key} в +page.svelte),
	// поэтому стор прогона всегда привязан к своей кампании.
	let { id }: { id: string } = $props();

	// svelte-ignore state_referenced_locally — компонент пересоздаётся на каждый id
	// (см. {#key} в +page.svelte), поэтому захват начального значения корректен
	const run = campaignRun(id);
	const { data: campaign, error, loading } = run;
	const { snapshot, reconnecting, terminal } = run.progress;

	// Прогон завершился — обновляем историю в сайдбаре.
	const unsubscribe = run.progress.terminal.subscribe((done) => {
		if (done) void refreshHistory();
	});

	onDestroy(() => {
		unsubscribe();
		run.destroy();
	});
</script>

{#if $error}
	<ErrorState message={$error} onRetry={() => void run.refresh()} />
{:else if $loading && !$campaign}
	<SkeletonLines lines={4} />
{:else if $campaign?.status === 'failed'}
	<div class="failed">
		<h2>Ошибка</h2>
		<p class="error">{$campaign.error}</p>
	</div>
{:else if $campaign?.status === 'done'}
	<div class="result">
		<h2>{$campaign.brief.product}</h2>
		{#if $campaign.strategy}
			<div class="positioning">
				<h3>Позиционирование</h3>
				<p>{$campaign.strategy.positioning}</p>
			</div>
		{/if}
		<p class="muted">Стоимость прогона: {formatCost($campaign.cost_usd)}</p>
		<div class="articles">
			{#each $campaign.deliverables ?? [] as d, i (i)}
				<ArticleCard deliverable={d} />
			{/each}
		</div>
	</div>
{:else if $campaign}
	<ProgressPanel title={$campaign.brief.product} snapshot={$snapshot ?? $campaign.progress ?? null} />
	{#if $reconnecting}
		<p class="muted">Переподключение…</p>
	{/if}
{/if}

<!-- Трасса видна всегда: она объясняет и удачный прогон, и провалившийся. -->
<TrajectoryPanel kind="campaign" {id} done={$terminal} />
