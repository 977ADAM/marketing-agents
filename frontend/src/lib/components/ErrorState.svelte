<script lang="ts">
	let { message, onRetry }: { message: string; onRetry?: () => void } = $props();
</script>

<div class="state state-error">
	<p class="error">{message}</p>
	{#if onRetry}
		<button type="button" class="btn btn-secondary" onclick={onRetry}>Повторить</button>
	{/if}
</div>
