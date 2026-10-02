<script lang="ts">
	let { width = '100%', height = 16 }: { width?: string | number; height?: number } = $props();

	const cssWidth = $derived(typeof width === 'number' ? `${width}px` : width);
</script>

<span class="skeleton" style="width: {cssWidth}; height: {height}px" aria-hidden="true"></span>
