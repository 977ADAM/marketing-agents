<script lang="ts">
	import { onDestroy } from 'svelte';
	import { createUploadCoordinator } from '#lib/stores/uploads.js';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { errorMessage } from '#lib/api/client.js';
	import { createReview, extractDocx } from '#lib/api/reviews.js';
	import type { ReviewText } from '#lib/api/types.js';
	import { refreshHistory } from '#lib/stores/history.js';
	import { toast } from '#lib/stores/toast.js';

	let brief = $state('');
	let texts = $state<(ReviewText & { id: string })[]>([{ id: crypto.randomUUID(), title: '', body: '' }]);
	const uploads = createUploadCoordinator();
	let uploading = $state(false);
	onDestroy(() => uploads.destroy());
	let busy = $state(false);
	let serverError = $state<string | null>(null);

	let briefFile: HTMLInputElement;
	let textFiles: Record<string, HTMLInputElement> = {};

	// Разбор .docx и валидация — на стороне API: клиент только отправляет файл
	// и раскладывает готовые title/body по полям.
	async function handleDocx(e: Event, target: string) {
		const input = e.currentTarget as HTMLInputElement;
		const file = input.files?.[0];
		input.value = ''; // позволяем повторно выбрать тот же файл
		if (!file) return;
		serverError = null;
		const upload = uploads.start(target);
		uploading = uploads.busy();
		try {
			const doc = await extractDocx(file);
			if (!upload.current()) return;
			if (target === 'brief') {
				brief = doc.text;
			} else {
				const row = texts.find(text => text.id === target);
				if (row) { row.title = doc.title; row.body = doc.body; }
			}
		} catch (err) {
			if (upload.current()) serverError = `Не удалось разобрать .docx: ${errorMessage(err)}`;
		} finally {
			upload.finish();
			uploading = uploads.busy();
		}
	}

	function addText() {
		texts.push({ id: crypto.randomUUID(), title: '', body: '' });
	}

	function removeText(id: string) {
		uploads.remove(id);
		uploading = uploads.busy();
		texts = texts.filter(text => text.id !== id);
	}

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (uploads.busy() || busy) return;
		busy = true;
		serverError = null;
		try {
			const { id } = await createReview({ brief, texts: texts.map(({title, body}) => ({title, body})) });
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
		{#each texts as t (t.id)}
			<div class="review-text">
				<input class="control" bind:value={t.title} placeholder="Заголовок статьи" />
				<textarea class="control" bind:value={t.body} placeholder="Текст статьи…"></textarea>
				<div class="review-text-actions">
					<input
						bind:this={textFiles[t.id]}
						type="file"
						accept=".docx"
						hidden
						onchange={(e) => void handleDocx(e, t.id)}
					/>
					<button type="button" class="btn btn-secondary" onclick={() => textFiles[t.id]?.click()}>
						📄 Загрузить статью (.docx)
					</button>
					{#if texts.length > 1}
						<button type="button" class="btn btn-danger" onclick={() => removeText(t.id)}>Убрать</button>
					{/if}
				</div>
			</div>
		{/each}
		<button type="button" class="btn btn-secondary" onclick={addText}>+ Добавить текст</button>
	</div>

	{#if serverError}
		<p class="error" role="alert">{serverError}</p>
	{/if}
	<button type="submit" class="btn btn-primary btn-block" disabled={busy || uploading}>
		{busy ? 'Проверяем…' : 'Проверить агентами →'}
	</button>
</form>
