<script lang="ts">
	import { errorMessage } from '#lib/api/client.js';
	import { getTrajectoryEvent } from '#lib/api/trajectory.js';
	import type { TrajectoryEvent } from '#lib/api/types.js';
	import { onDestroy } from 'svelte';
	import { createTrajectoryStore } from '#lib/stores/trajectory.js';
	import { TRACE_KIND_LABELS, TRACE_STATUS_LABELS } from '#lib/labels.js';
	import { buildTraceBody, type TraceBody } from '#lib/trace-payload.js';

	let {
		kind,
		id,
		done = false
	}: { kind: 'campaign' | 'review'; id: string; done?: boolean } = $props();

	// svelte-ignore state_referenced_locally
	const stream = createTrajectoryStore(kind, id);
	const { data: trajectory, error: streamError, loading } = stream;
	let error = $state<string | null>(null);
	let opened = $state(false);
	let alive = true;
	let bodies = $state<Record<number, TraceBody>>({});
	let openSeq = $state<number | null>(null);
	onDestroy(() => { alive = false; stream.destroy(); });

	async function toggle(ev: TrajectoryEvent) {
		if (!ev.has_payload) return;
		if (openSeq === ev.seq) {
			openSeq = null;
			return;
		}
		openSeq = ev.seq;
		if (bodies[ev.seq]) return;
		try {
			const full = await getTrajectoryEvent(kind, id, ev.seq);
			if (alive) bodies = { ...bodies, [ev.seq]: buildTraceBody(full.payload) };
		} catch (err) {
			if (alive) error = errorMessage(err);
		}
	}

	$effect(() => {
		stream.setActive(opened && !done);
		if (opened) void stream.refresh();
	});

	function time(at: string): string {
		const date = new Date(at);
		return Number.isNaN(date.getTime()) ? at : date.toLocaleTimeString('ru-RU');
	}
</script>

<details class="trajectory" bind:open={opened}>
	<summary>
		Трасса прогона{#if  $trajectory && $trajectory.total > 0}: {$trajectory.total} событий{/if}
	</summary>

	{#if $loading && !$trajectory}
		<p class="muted">Загружаем трассу…</p>
	{:else if error || $streamError}
		<p class="error" role="alert">{error ?? $streamError}</p>
		<button type="button" disabled={$loading} onclick={() => { error = null; void stream.refresh(); }}>Повторить</button>
	{:else if !$trajectory || $trajectory.events.length === 0}
		<p class="muted">
			Событий нет: трасса выключена (<code>TRACE_MODE=off</code>) или прогон шёл до её появления.
		</p>
	{:else}
		<ol class="trace">
			{#each $trajectory.events as ev (ev.seq)}
				<li class="trace-event trace-{ev.kind}" class:trace-failed={ev.status === 'error'}>
					<button
						type="button"
						class="trace-head"
						onclick={() => void toggle(ev)}
						disabled={!ev.has_payload}
						title={ev.has_payload ? 'Показать детали' : 'Тело события не сохранено (TRACE_MODE=summary)'}
					>
						<span class="trace-time">{time(ev.at)}</span>
						<span class="trace-kind">{TRACE_KIND_LABELS[ev.kind]}</span>
						<span class="trace-name">{ev.name}</span>
						<span class="trace-summary">{ev.summary}</span>
						{#if ev.duration_ms > 0}
							<span class="trace-meta">{ev.duration_ms} мс</span>
						{/if}
						{#if ev.prompt_tokens > 0 || ev.completion_tokens > 0}
							<span class="trace-meta">{ev.prompt_tokens}→{ev.completion_tokens} ток.</span>
						{/if}
						{#if ev.status === 'error'}
							<span class="trace-meta trace-bad">{TRACE_STATUS_LABELS.error}</span>
						{/if}
					</button>

					{#if ev.error}
						<p class="error">{ev.error}</p>
					{/if}

					{#if openSeq === ev.seq}
						{#if bodies[ev.seq]}
							{@const body = bodies[ev.seq]}
							<div class="trace-body">
								{#if body.meta.model || body.meta.finishReason || body.meta.reasoningTokens}
									<p class="trace-chips">
										{#if body.meta.model}<span class="trace-chip">{body.meta.model}</span>{/if}
										{#if body.meta.finishReason}<span class="trace-chip">финал: {body.meta.finishReason}</span>{/if}
										{#if body.meta.reasoningTokens}<span class="trace-chip">{body.meta.reasoningTokens} токенов размышлений</span>{/if}
									</p>
								{/if}

								{#if body.envelopeTruncated}
									<p class="trace-warn">Тело обрезано бюджетом трассы (TRACE_MAX_PAYLOAD_BYTES) — виден только его фрагмент.</p>
								{/if}

								{#if body.kind === 'sections'}
									{#each body.sections as section (section.key)}
										<details class="trace-section" open={section.open}>
											<summary>
												{section.title}
												{#if section.truncated}<span class="trace-warn"> · обрезано</span>{/if}
											</summary>
											<pre class="trace-payload">{section.text}</pre>
										</details>
									{/each}
								{:else}
									<pre class="trace-payload">{body.json}</pre>
								{/if}
							</div>
						{:else}
							<p class="muted">Загружаем…</p>
						{/if}
					{/if}
				</li>
			{/each}
		</ol>
	{/if}
	{#if $trajectory?.has_more}
		<button type="button" disabled={$loading} onclick={() => void stream.refresh()}>Загрузить ещё</button>
	{/if}
</details>
