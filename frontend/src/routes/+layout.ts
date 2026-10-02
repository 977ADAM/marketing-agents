// SPA-режим: рендер только в браузере, данные забирает клиент.
// Прод-сервер (adapter-node) отдаёт оболочку и проксирует /api (hooks.server.ts).
export const ssr = false;
export const prerender = false;
