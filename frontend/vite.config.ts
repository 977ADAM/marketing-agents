import adapter from '@sveltejs/adapter-node';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

// Базовый путь приложения: '' для standalone, '/marketing' под interpool.
// Задаётся build-arg PUBLIC_BASE (см. frontend/Dockerfile); от него зависят
// ссылки на ассеты, клиентские роуты и префикс /api.
const base = ((process.env.PUBLIC_BASE || '/').replace(/\/+$/, '') || '') as '' | `/${string}`;

export default defineConfig({
	plugins: [
		sveltekit({
			compilerOptions: {
				// Runes-режим для всего проекта (как в сгенерированном скелете).
				runes: ({ filename }) =>
					filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},

			// Прод-рантайм: Node-сервер SvelteKit (frontend/Dockerfile).
			adapter: adapter(),

			paths: { base }
		})
	],
	server: {
		// Локальный dev: API живёт на Go-бэкенде (:8080). /healthz проксируем по
		// той же причине, что и /api: его роут (routes/healthz/+server.ts) ходит
		// по BACKEND_URL, а дефолт там — имя сервиса из docker-compose
		// (backend:8080), которое вне compose не резолвится.
		proxy: {
			'/api': 'http://localhost:8080',
			'/healthz': 'http://localhost:8080'
		}
	}
});
