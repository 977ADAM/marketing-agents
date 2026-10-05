export function createUploadCoordinator() {
	const active = new Map<string, symbol>();
	let destroyed = false;
	function start(id: string) {
		const token = Symbol(id);
		active.set(id, token);
		const current = () => !destroyed && active.get(id) === token;
		return { current, finish: () => { if (current()) active.delete(id); } };
	}
	return { start, busy: () => active.size > 0, remove: (id: string) => active.delete(id), destroy: () => { destroyed = true; active.clear(); } };
}
