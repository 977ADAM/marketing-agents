import type { RequestHandler } from '@sveltejs/kit';
import { BACKEND_URL } from '$app/env/private';

// Healthcheck фронт-сервера: проверяем, что доступен API (как раньше — единый /healthz).
export const GET: RequestHandler = async () => {
	try {
		const res = await fetch(new URL('/healthz', BACKEND_URL));
		return new Response(res.body, { status: res.status, headers: res.headers });
	} catch (err) {
		const message = err instanceof Error ? err.message : String(err);
		return new Response(JSON.stringify({ error: { code: 'bad_gateway', message } }), {
			status: 502,
			headers: { 'Content-Type': 'application/json' }
		});
	}
};
