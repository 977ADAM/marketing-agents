<script lang="ts">
	import { toasts } from '#lib/stores/toast.js';
</script>

<div class="toasts" role="status" aria-live="polite">
	{#each $toasts as t (t.id)}
		<div class="toast toast-{t.kind}">{t.message}</div>
	{/each}
</div>
