<script lang="ts">
	import { page } from '$app/state';
	import { resolve } from '$app/paths';
</script>

<div class="state state-error">
	<h2>Ошибка</h2>
	<p class="error">{page.error?.message ?? 'Не удалось загрузить данные'}</p>
	<a class="btn btn-primary" href={resolve('/')}>К новой кампании</a>
</div>
