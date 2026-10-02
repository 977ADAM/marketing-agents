import type { PageLoad } from './$types';

// Данные забирает стор страницы (#lib/stores/run.js): нужен и live-прогресс,
// и перезапрос по завершении прогона, поэтому load отдаёт только id.
export const load: PageLoad = ({ params }) => ({ id: params.id });
