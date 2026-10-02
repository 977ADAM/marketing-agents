<script lang="ts">
	import { onMount } from 'svelte';
	import { resolve } from '$app/paths';
	import favicon from '#lib/assets/favicon.svg';
	import CampaignHistoryList from '#lib/components/CampaignHistoryList.svelte';
	import ReviewHistoryList from '#lib/components/ReviewHistoryList.svelte';
	import Toaster from '#lib/components/Toaster.svelte';
	import { refreshHistory } from '#lib/stores/history.js';
	import '#lib/styles/tokens.css';
	import '#lib/styles/base.css';
	import '#lib/styles/components.css';

	let { children } = $props();

	onMount(() => {
		void refreshHistory();
	});
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
</svelte:head>

<div class="app">
	<aside class="sidebar">
		<div class="sidebar-actions">
			<a class="btn btn-primary btn-block" href={resolve('/')}>+ Новая кампания</a>
			<a class="btn btn-secondary btn-block" href={resolve('/reviews')}>✎ Проверить тексты</a>
		</div>
		<div class="history-label">Кампании</div>
		<CampaignHistoryList />
		<div class="history-label">Проверки текстов</div>
		<ReviewHistoryList />
	</aside>
	<main class="main">
		{@render children()}
	</main>
</div>

<Toaster />
