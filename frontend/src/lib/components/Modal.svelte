<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		open,
		title,
		onClose,
		children
	}: { open: boolean; title: string; onClose: () => void; children: Snippet } = $props();

	function onKey(e: KeyboardEvent) {
		if (open && e.key === 'Escape') onClose();
	}
</script>

<svelte:window onkeydown={onKey} />

{#if open}
	<div class="modal">
		<!-- фон — настоящая кнопка, а не div с onclick (требование a11y) -->
		<button type="button" class="modal-backdrop" aria-label="Закрыть" onclick={onClose}></button>
		<div class="modal-body" role="dialog" aria-modal="true" aria-label={title} tabindex="-1">
			<div class="modal-head">
				<h3>{title}</h3>
				<button type="button" class="btn btn-ghost" onclick={onClose} aria-label="Закрыть">✕</button>
			</div>
			{@render children()}
		</div>
	</div>
{/if}
