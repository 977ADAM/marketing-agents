<script lang="ts">
	// Сводка брифа: только чтение, запуск кампании и предупреждение о пробелах.
	// Пробелы приходят машинными ключами (product, goal, audience, tone) — здесь
	// переводим их в русские подписи своим словарём; неизвестный ключ показываем
	// как есть, чтобы новый ключ сервера не потерялся на экране.
	import { createCampaign } from '#lib/api/campaigns.js';
	import { errorMessage } from '#lib/api/client.js';
	import type { BriefDraft } from '#lib/api/types.js';
	import { refreshHistory } from '#lib/stores/history.js';
	import { interview } from '#lib/stores/interview.js';
	import { toast } from '#lib/stores/toast.js';

	// Лимит тем из /api/limits: подсказка к полю «Число статей».
	let { maxTopics }: { maxTopics: number } = $props();

	const { draft, missing, streaming } = interview;

	const FIELDS: { key: keyof BriefDraft; label: string }[] = [
		{ key: 'product', label: 'Продукт / бренд' },
		{ key: 'goal', label: 'Цель кампании' },
		{ key: 'audience', label: 'Аудитория' },
		{ key: 'tone', label: 'Tone of voice' },
		{ key: 'region', label: 'Регион (geo ID)' },
		{ key: 'topics_count', label: 'Число статей' }
	];

	/** Машинный ключ пробела → подпись поля. */
	const MISSING_LABELS: Record<string, string> = {
		product: 'Продукт / бренд',
		goal: 'Цель кампании',
		audience: 'Аудитория',
		tone: 'Tone of voice'
	};

	const missingLabels = $derived($missing.map((key) => MISSING_LABELS[key] ?? key));

	let launching = $state(false);
	let launchError = $state<string | null>(null);

	/** Пустое поле — прочерк; пустое «Число статей» значит «подберём сами». */
	function fieldValue(key: keyof BriefDraft): string {
		const value = $draft[key];
		if (key === 'topics_count' && value === undefined) return 'автоматически';
		const text = value === undefined ? '' : String(value).trim();
		return text === '' ? '—' : text;
	}

	async function launch(): Promise<void> {
		if (launching || $streaming) return;
		launching = true;
		launchError = null;
		try {
			// createCampaign сам добавляет Idempotency-Key. id кладём в стор: по нему
			// лента рисует блок прогона и восстанавливает его после перезагрузки.
			const { id } = await createCampaign(interview.brief());
			interview.campaignId.set(id);
			await refreshHistory();
			toast.success('Кампания создана');
		} catch (err) {
			launchError = errorMessage(err);
			toast.error(launchError);
		} finally {
			launching = false;
		}
	}
</script>

<section class="brief-block" aria-label="Сводка брифа">
	<h3>Сводка брифа</h3>

	<dl class="brief">
		{#each FIELDS as field (field.key)}
			<div class="brief-row">
				<dt class="brief-label">{field.label}</dt>
				<dd class="brief-value">
					{fieldValue(field.key)}
					{#if field.key === 'topics_count'}
						<span class="field-hint">лимит: {maxTopics}</span>
					{/if}
				</dd>
			</div>
		{/each}
	</dl>

	{#if missingLabels.length > 0}
		<p class="brief-gap" role="status">Не хватает: {missingLabels.join(', ')}</p>
	{/if}

	<button
		type="button"
		class="btn btn-primary btn-block"
		disabled={$streaming || launching}
		onclick={() => void launch()}
	>
		{launching ? 'Запускаем…' : 'Запустить кампанию'}
	</button>

	{#if launchError}
		<p class="error" role="alert">{launchError}</p>
	{/if}
</section>
