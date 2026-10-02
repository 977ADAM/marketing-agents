<script lang="ts">
	import { PHASE_LABELS, TOPIC_LABELS } from '#lib/labels.js';
	import type { Phase, Snapshot, TopicState } from '#lib/api/types.js';

	let {
		title,
		snapshot,
		phaseLabels,
		topicLabels,
		showIter = true
	}: {
		title: string;
		snapshot: Snapshot | null;
		phaseLabels?: Partial<Record<Phase, string>>;
		topicLabels?: Partial<Record<TopicState, string>>;
		showIter?: boolean;
	} = $props();

	const percent = $derived(snapshot?.percent ?? 0);
	const phaseText = $derived(
		snapshot ? (phaseLabels?.[snapshot.phase] ?? PHASE_LABELS[snapshot.phase]) : 'Подключение…'
	);
</script>

<div class="progress">
	<h2>{title}</h2>
	<p>{phaseText}</p>
	<div
		class="bar"
		role="progressbar"
		aria-valuenow={percent}
		aria-valuemin="0"
		aria-valuemax="100"
		aria-label="Прогресс прогона"
	>
		<div class="bar-fill" style="width: {percent}%"></div>
	</div>
	<ul class="topics">
		{#each snapshot?.topics ?? [] as t (t.index)}
			<li class="topic topic-{t.state}">
				<span class="topic-title">{t.title}</span>
				<span class="topic-state">
					{topicLabels?.[t.state] ?? TOPIC_LABELS[t.state]}{showIter && t.iter ? ` · итер. ${t.iter}` : ''}
				</span>
			</li>
		{/each}
	</ul>
</div>
