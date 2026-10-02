import { defineEnvVars } from '@sveltejs/kit/env';

// Переменные окружения фронт-сервера. Приватные (по умолчанию) не попадают
// в браузерный бандл; читаются в рантайме из окружения процесса.
export const variables = defineEnvVars({
	BACKEND_URL: {
		description: 'Адрес Go-API, куда проксируются /api/* и /healthz',
		// Значение по умолчанию — имя сервиса в docker-compose.
		schema: (value) => value ?? 'http://backend:8080'
	}
});
