<script lang="ts">
	import { errorMessage } from '#lib/api/client.js';
	import { getTrajectory, getTrajectoryEvent } from '#lib/api/trajectory.js';
	import type { Trajectory, TrajectoryEvent } from '#lib/api/types.js';
	import { TRACE_KIND_LABELS, TRACE_STATUS_LABELS } from '#lib/labels.js';

	let {
		kind,
		id,
		done = false
	}: { kind: 'campaign' | 'review'; id: string; done?: boolean } = $props();

	let trajectory = $state<Trajectory | null>(null);
	let error = $state<string | null>(null);
	let loading = $state(false);
	// Тела событий подгружаются лениво, по одному: в ленте их нет.
	let details = $state<Record<number, TrajectoryEvent>>({});
	let openSeq = $state<number | null>(null);

	async function load(runID: string) {
		loading = true;
		try {
			trajectory = await getTrajectory(kind, runID);
			error = null;
		} catch (err) {
			error = errorMessage(err);
		} finally {
			loading = false;
		}
	}

	async function toggle(ev: TrajectoryEvent) {
		if (!ev.has_payload) return;
		if (openSeq === ev.seq) {
			openSeq = null;
			return;
		}
		openSeq = ev.seq;
		if (details[ev.seq]) return;
		try {
			const full = await getTrajectoryEvent(kind, id, ev.seq);
			details = { ...details, [ev.seq]: full };
		} catch (err) {
			error = errorMessage(err);
		}
	}

	// Перечитываем ленту при смене прогона и когда прогон завершился: события
	// копятся по ходу, а окончательный состав появляется в конце.
	$effect(() => {
		void load(id);
	});
	$effect(() => {
		if (done) void load(id);
	});

	function time(at: string): string {
		const date = new Date(at);
		return Number.isNaN(date.getTime()) ? at : date.toLocaleTimeString('ru-RU');
	}
</script>

<details class="trajectory">
	<summary>
		Трасса прогона{#if trajectory && trajectory.total > 0}: {trajectory.total} событий{/if}
	</summary>

	{#if loading && !trajectory}
		<p class="muted">Загружаем трассу…</p>
	{:else if error}
		<p class="error" role="alert">{error}</p>
	{:else if !trajectory || trajectory.events.length === 0}
		<p class="muted">
			Событий нет: трасса выключена (<code>TRACE_MODE=off</code>) или прогон шёл до её появления.
		</p>
	{:else}
		<ol class="trace">
			{#each trajectory.events as ev (ev.seq)}
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
						<pre class="trace-payload">{details[ev.seq]
								? JSON.stringify(details[ev.seq].payload, null, 2)
								: 'Загружаем…'}</pre>
					{/if}
				</li>
			{/each}
		</ol>
	{/if}
</details>
