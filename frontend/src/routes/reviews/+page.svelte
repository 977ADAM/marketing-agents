<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { errorMessage } from '#lib/api/client.js';
	import { createReview, extractDocx } from '#lib/api/reviews.js';
	import type { ReviewText } from '#lib/api/types.js';
	import { refreshHistory } from '#lib/stores/history.js';
	import { toast } from '#lib/stores/toast.js';

	let brief = $state('');
	let texts = $state<ReviewText[]>([{ title: '', body: '' }]);
	let busy = $state(false);
	let serverError = $state<string | null>(null);

	let briefFile: HTMLInputElement;
	let textFiles: HTMLInputElement[] = [];

	// Разбор .docx и валидация — на стороне API: клиент только отправляет файл
	// и раскладывает готовые title/body по полям.
	async function handleDocx(e: Event, target: 'brief' | number) {
		const input = e.currentTarget as HTMLInputElement;
		const file = input.files?.[0];
		input.value = ''; // позволяем повторно выбрать тот же файл
		if (!file) return;
		serverError = null;
		try {
			const doc = await extractDocx(file);
			if (target === 'brief') {
				brief = doc.text;
			} else {
				texts[target].title = doc.title;
				texts[target].body = doc.body;
			}
		} catch (err) {
			serverError = `Не удалось разобрать .docx: ${errorMessage(err)}`;
		}
	}

	function addText() {
		texts.push({ title: '', body: '' });
	}

	function removeText(i: number) {
		texts.splice(i, 1);
	}

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		serverError = null;
		try {
			const { id } = await createReview({ brief, texts });
			await refreshHistory();
			toast.success('Проверка запущена');
			await goto(resolve('/reviews/[id]', { id }));
		} catch (err) {
			serverError = errorMessage(err);
			toast.error(serverError);
			busy = false;
		}
	}
</script>

<form class="new-campaign review-form" onsubmit={submit} novalidate>
	<h2>Проверка готовых текстов</h2>

	<label class="field">
		<span class="field-label">Бриф</span>
		<textarea
			class="control"
			bind:value={brief}
			placeholder="Требования клиента: продукт, аудитория, УТП, запреты…"
		></textarea>
		<span class="field-hint">Скопируйте текст брифа или загрузите .docx</span>
	</label>
	<input
		bind:this={briefFile}
		type="file"
		accept=".docx"
		hidden
		onchange={(e) => void handleDocx(e, 'brief')}
	/>
	<button type="button" class="btn btn-secondary" onclick={() => briefFile?.click()}>
		📄 Загрузить бриф (.docx)
	</button>

	<div class="review-texts">
		<div class="review-texts-label">Тексты для проверки</div>
		{#each texts as t, i (i)}
			<div class="review-text">
				<input class="control" bind:value={t.title} placeholder="Заголовок статьи" />
				<textarea class="control" bind:value={t.body} placeholder="Текст статьи…"></textarea>
				<div class="review-text-actions">
					<input
						bind:this={textFiles[i]}
						type="file"
						accept=".docx"
						hidden
						onchange={(e) => void handleDocx(e, i)}
					/>
					<button type="button" class="btn btn-secondary" onclick={() => textFiles[i]?.click()}>
						📄 Загрузить статью (.docx)
					</button>
					{#if texts.length > 1}
						<button type="button" class="btn btn-danger" onclick={() => removeText(i)}>Убрать</button>
					{/if}
				</div>
			</div>
		{/each}
		<button type="button" class="btn btn-secondary" onclick={addText}>+ Добавить текст</button>
	</div>

	{#if serverError}
		<p class="error" role="alert">{serverError}</p>
	{/if}
	<button type="submit" class="btn btn-primary btn-block" disabled={busy}>
		{busy ? 'Проверяем…' : 'Проверить агентами →'}
	</button>
</form>
