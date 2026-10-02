import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vitest/config';

// Юнит-тесты модулей фронта (api/stores/labels/format) — без DOM и без
// SvelteKit-плагина, поэтому виртуальный модуль $app/paths подменяется
// заглушкой с тем же контрактом resolve(route, params).
export default defineConfig({
	resolve: {
		alias: {
			'$app/paths': fileURLToPath(new URL('./src/test/app-paths.stub.ts', import.meta.url))
		}
	},
	test: {
		environment: 'node',
		include: ['src/**/*.test.ts']
	}
});
