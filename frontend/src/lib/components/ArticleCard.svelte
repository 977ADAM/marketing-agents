<script lang="ts">
	import Modal from './Modal.svelte';
	import ScoreBadge from './ScoreBadge.svelte';
	import { ARTICLE_VERDICT_LABELS } from '#lib/labels.js';
	import type { Deliverable } from '#lib/api/types.js';

	let { deliverable }: { deliverable: Deliverable } = $props();

	let open = $state(false);
</script>

<div class="article-card">
	<div class="article-head">
		<h3>{deliverable.title}</h3>
		{#if deliverable.review}<ScoreBadge score={deliverable.review.score} severity={deliverable.review.severity} />{:else}<span>Не проверено</span>{/if}
	</div>
	{#if deliverable.review}<p class="verdict">{ARTICLE_VERDICT_LABELS[deliverable.review.verdict] ?? deliverable.review.verdict}</p>{/if}
	<p class="cta">{deliverable.cta}</p>
	<button type="button" class="btn btn-secondary" onclick={() => (open = true)}>Читать ▸</button>

	<Modal {open} title={deliverable.title} onClose={() => (open = false)}>
		<p class="article-body">{deliverable.body}</p>
		{#if deliverable.review?.issues && deliverable.review.issues.length > 0}
			<h4>Замечания критика</h4>
			<ul class="issues">
				{#each deliverable.review.issues as issue, i (i)}
					<li>{issue}</li>
				{/each}
			</ul>
		{/if}
		<p class="cta">{deliverable.cta}</p>
	</Modal>
</div>
