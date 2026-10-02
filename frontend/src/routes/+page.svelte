<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { createCampaign } from '#lib/api/campaigns.js';
	import { errorMessage } from '#lib/api/client.js';
	import type { Brief } from '#lib/api/types.js';
	import { refreshHistory } from '#lib/stores/history.js';
	import { toast } from '#lib/stores/toast.js';

	const FIELDS: { name: keyof Brief; label: string }[] = [
		{ name: 'product', label: 'Продукт / бренд' },
		{ name: 'goal', label: 'Цель кампании' },
		{ name: 'audience', label: 'Аудитория' },
		{ name: 'tone', label: 'Tone of voice' }
	];

	let brief = $state<Brief>({ product: '', goal: '', audience: '', tone: '' });
	let busy = $state(false);
	let serverError = $state<string | null>(null);

	// Валидации на клиенте нет: что не так — скажет API (400 {error:{...}}),
	// его сообщение показываем в форме.
	async function submit(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		serverError = null;
		try {
			const { id } = await createCampaign(brief);
			await refreshHistory();
			toast.success('Кампания создана');
			await goto(resolve('/campaigns/[id]', { id }));
		} catch (err) {
			serverError = errorMessage(err);
			toast.error(serverError);
			busy = false;
		}
	}
</script>

<form class="new-campaign" onsubmit={submit} novalidate>
	<h2>Новая кампания</h2>
	{#each FIELDS as field (field.name)}
		<label class="field">
			<span class="field-label">{field.label}</span>
			<textarea class="control" bind:value={brief[field.name]}></textarea>
		</label>
	{/each}
	{#if serverError}
		<p class="error" role="alert">{serverError}</p>
	{/if}
	<button type="submit" class="btn btn-primary btn-block" disabled={busy}>
		{busy ? 'Создаём…' : 'Сгенерировать →'}
	</button>
</form>
