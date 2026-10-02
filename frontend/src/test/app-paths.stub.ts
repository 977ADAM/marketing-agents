// Заглушка виртуального модуля SvelteKit `$app/paths` для юнит-тестов.
// Повторяет контракт resolve(route, params): подстановка параметров маршрута
// (обычных `[id]` и rest-формы `[...path]`). Базовый путь приложения в тестах
// пустой — как при standalone-деплое.

export function resolve(route: string, params: Record<string, string> = {}): string {
	return route
		.replace(/\[\.\.\.(\w+)\]/g, (_, key: string) => params[key] ?? '')
		.replace(/\[(\w+)\]/g, (_, key: string) => params[key] ?? '');
}
