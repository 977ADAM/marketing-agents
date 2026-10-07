<script lang="ts">
	// Главная страница — лента брифа: диалог с интервьюером и прогон кампании
	// прямо в ленте. Форма создания кампании убрана: бриф собирает интервьюер,
	// а лимит тем из /api/limits нужен только сводке брифа.
	import type { PageData } from './$types';
	import ChatThread from '#lib/components/ChatThread.svelte';

	let { data }: { data: PageData } = $props();
</script>

<ChatThread maxTopics={data.limits.max_topics} />
