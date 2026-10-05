<script lang="ts">
	import {retryReview} from '#lib/api/reviews.js';
 import {toast} from '#lib/stores/toast.js';
 import {errorMessage} from '#lib/api/client.js';
	import { onDestroy } from 'svelte';
	import ErrorState from './ErrorState.svelte';
	import ProgressPanel from './ProgressPanel.svelte';
	import ReportCard from './ReportCard.svelte';
	import SkeletonLines from './SkeletonLines.svelte';
	import TrajectoryPanel from './TrajectoryPanel.svelte';
	import { formatCost } from '#lib/format.js';
	import { REVIEW_PHASE_LABELS, REVIEW_TOPIC_LABELS } from '#lib/labels.js';
	import { refreshHistory } from '#lib/stores/history.js';
	import { reviewRun } from '#lib/stores/run.js';

	let { id }: { id: string } = $props();

	// svelte-ignore state_referenced_locally — компонент пересоздаётся на каждый id
	// (см. {#key} в +page.svelte), поэтому захват начального значения корректен
	const run = reviewRun(id);
	const { data: review, error, loading } = run;
	const { snapshot, terminal } = run.progress;

	const unsubscribe = run.progress.terminal.subscribe((done) => {
		if (done) void refreshHistory();
	});

	onDestroy(() => {
		unsubscribe();
		run.destroy();
	});
 let retrying=$state(false);
 async function resume(){retrying=true;try{await retryReview(id);run.progress.restart();await run.refresh();await refreshHistory();}catch(err){toast.error(errorMessage(err));}finally{retrying=false;}}
</script>

{#if $error}
	<ErrorState message={$error} onRetry={() => void run.refresh()} />
{:else if $loading && !$review}
	<SkeletonLines lines={5} />
{:else if $review?.status === 'failed'}
	<div class="failed">
		<h2>Ошибка проверки</h2>
		<p class="error">{$review.error}</p>
 {#if $review.resume_available}<button class="btn btn-primary" disabled={retrying} onclick={resume}>{retrying ? 'Запускаем…' : 'Продолжить'}</button>{/if}
 <p class="muted">Оценочная стоимость: {formatCost($review.cost_usd,$review.cost_known)}</p>
 {#each $review.result?.items ?? [] as report,i (i)}<ReportCard {report} />{/each}
	</div>
{:else if $review?.status === 'done' && $review.result}
	<div class="result">
		<h2>Отчёт по текстам</h2>
		<p class="muted">
			Проверено текстов: {$review.result.items.length}, прошло: {$review.result.passed} · Оценочная стоимость:
			{formatCost($review.cost_usd,$review.cost_known)}
		</p>
		<details class="brief-box">
			<summary>Бриф</summary>
			<p class="brief-text">{$review.brief_text}</p>
		</details>
		<div class="reports">
			{#each $review.result.items as report, i (i)}
				<ReportCard {report} />
			{/each}
		</div>
	</div>
{:else if $review}
	<ProgressPanel
		title="Проверка текстов"
		snapshot={$snapshot ?? $review.progress ?? null}
		phaseLabels={REVIEW_PHASE_LABELS}
		topicLabels={REVIEW_TOPIC_LABELS}
		showIter={false}
	/>
{/if}

<TrajectoryPanel kind="review" {id} done={$terminal} />
