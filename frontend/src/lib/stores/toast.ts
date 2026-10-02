// Всплывающие уведомления: события без места в разметке («кампания создана»,
// «не удалось обновить историю»). Ошибки форм показываются прямо в форме.

import { writable } from 'svelte/store';

export type ToastKind = 'success' | 'error' | 'info';

export interface Toast {
	id: number;
	kind: ToastKind;
	message: string;
}

export const toasts = writable<Toast[]>([]);

const TTL_MS = 6000;

function push(kind: ToastKind, message: string): void {
	const id = Date.now() + Math.random();
	toasts.update((items) => [...items, { id, kind, message }]);
	setTimeout(() => {
		toasts.update((items) => items.filter((t) => t.id !== id));
	}, TTL_MS);
}

export const toast = {
	success: (message: string) => push('success', message),
	error: (message: string) => push('error', message),
	info: (message: string) => push('info', message)
};
