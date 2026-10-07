<script lang="ts">
	// Лента брифа: реплики диалога с живыми дельтами, ввод, сводка брифа и
	// прогон запущенной кампании прямо в ленте. Состояние живёт в сторе
	// `interview` (см. #lib/stores/interview.ts) — компонент только рисует его.
	import { resolve } from '$app/paths';
	import type { Campaign, Snapshot } from '#lib/api/types.js';
	import ArticleCard from './ArticleCard.svelte';
	import BriefSummary from './BriefSummary.svelte';
	import ProgressPanel from './ProgressPanel.svelte';
	import TrajectoryPanel from './TrajectoryPanel.svelte';
	import { refreshHistory } from '#lib/stores/history.js';
	import { interview, OVER_LIMIT_MESSAGE } from '#lib/stores/interview.js';
	import { campaignRun, type RunStore } from '#lib/stores/run.js';

	// Лимит тем из /api/limits (загрузчик +page.ts) — уходит в сводку брифа.
	let { maxTopics }: { maxTopics: number } = $props();

	const { messages, streaming, error, campaignId, tooLong, storageWarning } = interview;

	let text = $state('');

	// --- прогон в ленте ---
	// Значения прогона зеркалим в обычные руны: стор создаётся на каждый
	// campaignId, а шаблону нужны простые значения, а не nullable-стор.
	let runId = $state<string | undefined>(undefined);
	let runData = $state<Campaign | null>(null);
	let runError = $state<string | null>(null);
	let runSnapshot = $state<Snapshot | null>(null);
	let runReconnecting = $state(false);
	let runTerminal = $state(false);
	let runStore: RunStore<Campaign> | null = null;

	$effect(() => {
		const id = $campaignId;
		runId = id;
		if (!id) {
			runStore = null;
			runData = null;
			runError = null;
			runSnapshot = null;
			runReconnecting = false;
			runTerminal = false;
			return;
		}
		const store = campaignRun(id);
		runStore = store;
		const unsubscribes = [
			store.data.subscribe((value) => (runData = value)),
			store.error.subscribe((value) => (runError = value)),
			store.progress.snapshot.subscribe((value) => (runSnapshot = value)),
			store.progress.reconnecting.subscribe((value) => (runReconnecting = value)),
			store.progress.terminal.subscribe((value) => {
				runTerminal = value;
				// Прогон завершился — обновляем историю в сайдбаре.
				if (value) void refreshHistory();
			})
		];
		// Смена кампании или размонтирование: гасим старый прогон целиком.
		return () => {
			runStore = null;
			for (const unsubscribe of unsubscribes) unsubscribe();
			store.destroy();
		};
	});

	function refreshRun(): void {
		void runStore?.refresh();
	}

	async function submit(): Promise<void> {
		if ($streaming || $tooLong) return;
		const value = text.trim();
		if (value === '') return;
		text = '';
		await interview.send(value);
	}

	/** Enter отправляет, Shift+Enter переносит строку; ввод иероглифов не ломаем. */
	function onKeydown(event: KeyboardEvent): void {
		if (event.key !== 'Enter' || event.shiftKey || event.isComposing) return;
		event.preventDefault();
		void submit();
	}
</script>

<div class="thread-head">
	<h2>Бриф кампании</h2>
	<button type="button" class="btn btn-secondary" onclick={() => interview.reset()}>
		Новый диалог
	</button>
</div>

<div class="thread" aria-live="polite">
	{#if $messages.length === 0 && !$streaming}
		<p class="muted thread-empty">
			Расскажите о продукте, цели и аудитории — интервьюер соберёт бриф и предложит запуск.
		</p>
	{/if}
	{#each $messages as message, i (i)}
		<div
			class="bubble bubble-{message.role}"
			class:bubble-live={$streaming && message.role === 'assistant' && i === $messages.length - 1}
		>
			<p class="bubble-text">{message.content}</p>
		</div>
	{/each}
	{#if $streaming}
		<p class="typing" role="status">Ассистент печатает…</p>
	{/if}
</div>

{#if $error}
	<div class="bubble bubble-error" role="alert">
		<p class="error">{$error}</p>
		<button
			type="button"
			class="btn btn-secondary"
			disabled={$streaming || $tooLong}
			onclick={() => void interview.retry()}
		>
			Повторить
		</button>
	</div>
{/if}

<form
	class="thread-input"
	onsubmit={(event) => {
		event.preventDefault();
		void submit();
	}}
>
	<textarea
		class="control"
		rows="2"
		aria-label="Ваш ответ"
		placeholder="Ваш ответ…"
		bind:value={text}
		disabled={$streaming || $tooLong}
		onkeydown={onKeydown}
	></textarea>
	<button type="submit" class="btn btn-primary" disabled={$streaming || $tooLong || text.trim() === ''}>
		Отправить
	</button>
</form>

{#if $tooLong}
	<p class="thread-hint">{OVER_LIMIT_MESSAGE}</p>
{/if}

{#if $storageWarning}
	<p class="thread-warning" role="status">{$storageWarning}</p>
{/if}

<BriefSummary {maxTopics} />

{#if runId}
	{@const id = runId}
	<section class="thread-run" aria-label="Прогон кампании">
		{#if runError}
			<p class="error" role="alert">{runError}</p>
			<button type="button" class="btn btn-secondary" onclick={refreshRun}>Обновить</button>
		{:else if runData?.status === 'failed'}
			<p class="error" role="alert">{runData.error ?? 'Кампания завершилась с ошибкой'}</p>
		{:else if runData?.status === 'done'}
			{#if runData.deliverables && runData.deliverables.length > 0}
				<div class="articles">
					{#each runData.deliverables as deliverable, i (i)}
						<ArticleCard {deliverable} />
					{/each}
				</div>
			{:else}
				<p class="muted">Кампания завершена, но статьи не сохранены.</p>
			{/if}
		{:else}
			<ProgressPanel
				title={runData?.brief.product ?? 'Кампания'}
				snapshot={runSnapshot ?? runData?.progress ?? null}
			/>
			{#if runReconnecting}
				<p class="muted">Переподключение…</p>
			{/if}
		{/if}

		<p class="thread-run-link">
			<a class="btn btn-secondary" href={resolve('/campaigns/[id]', { id })}>
				Открыть страницу кампании →
			</a>
		</p>
		<TrajectoryPanel kind="campaign" {id} done={runTerminal} />
	</section>
{/if}
