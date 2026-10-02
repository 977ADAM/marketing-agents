// Подмена EventSource для тестов SSE-прогресса: тест сам «присылает» сообщение,
// терминальное событие или ошибку и проверяет реакцию стора. После close()
// события не доставляются — как у настоящего EventSource.

export class FakeEventSource {
	static instances: FakeEventSource[] = [];

	readonly url: string;
	closed = false;
	onmessage: ((event: MessageEvent<string>) => void) | null = null;
	onerror: ((event: Event) => void) | null = null;

	private listeners = new Map<string, ((event: Event) => void)[]>();

	constructor(url: string) {
		this.url = url;
		FakeEventSource.instances.push(this);
	}

	static reset(): void {
		FakeEventSource.instances = [];
	}

	/** Последнее созданное соединение — то, с которым работает тест. */
	static get last(): FakeEventSource {
		return FakeEventSource.instances[FakeEventSource.instances.length - 1];
	}

	addEventListener(type: string, listener: (event: Event) => void): void {
		const list = this.listeners.get(type) ?? [];
		list.push(listener);
		this.listeners.set(type, list);
	}

	close(): void {
		this.closed = true;
	}

	// --- управление из теста ---

	emitMessage(data: string): void {
		if (this.closed) return;
		this.onmessage?.({ data } as MessageEvent<string>);
	}

	emitDone(data: string): void {
		if (this.closed) return;
		for (const listener of this.listeners.get('done') ?? []) {
			listener({ data } as unknown as Event);
		}
	}

	emitError(): void {
		if (this.closed) return;
		this.onerror?.(new Event('error'));
	}
}
