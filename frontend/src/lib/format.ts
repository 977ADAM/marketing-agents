// Форматирование для показа. Никаких правил и вычислений: всё, что можно
// посчитать (оценки, вердикты, сводки, разбор .docx), считает API.

/** Стоимость прогона: '$0.0123' или '—', если API её не вернул. */
export function formatCost(usd?: number | null, known?:boolean): string {
	if (known===false)return 'Оценка недоступна';
	return usd == null ? '—' : `$${usd.toFixed(4)}`;
}
