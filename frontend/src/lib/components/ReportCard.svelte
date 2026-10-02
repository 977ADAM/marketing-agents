<script lang="ts">
	import ScoreBadge from './ScoreBadge.svelte';
	import { REPORT_VERDICT_LABELS } from '#lib/labels.js';
	import type { CheckScore, TextReport } from '#lib/api/types.js';

	let { report }: { report: TextReport } = $props();
</script>

{#snippet checkBlock(label: string, check: CheckScore)}
	<div class="check-block">
		<div class="check-head">
			<span>{label}</span>
			<ScoreBadge score={check.score} severity={check.severity} />
		</div>
		{#if (check.issues ?? []).length > 0}
			<ul class="issues">
				{#each check.issues ?? [] as issue, i (i)}
					<li>{issue}</li>
				{/each}
			</ul>
		{:else}
			<p class="muted">Замечаний нет</p>
		{/if}
	</div>
{/snippet}

<div class="report-card {report.verdict === 'pass' ? 'report-pass' : 'report-fix'}">
	<div class="report-head">
		<span class="report-title">{report.title || 'Без заголовка'}</span>
		<span class="report-verdict">{REPORT_VERDICT_LABELS[report.verdict]}</span>
	</div>
	<div class="report-overall">
		Итог: <ScoreBadge score={report.overall} severity={report.severity} />
	</div>
	{@render checkBlock('Соответствие брифу', report.compliance)}
	{@render checkBlock('Корректность текста', report.quality)}
</div>
