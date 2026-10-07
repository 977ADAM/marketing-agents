// Тесты разбора SSE-потока: разрезанные чанки, многострочный data, хвостовой
// огрызок, служебные строки и битый JSON. Парсер чистый, поэтому проверяем
// только пары «вход → кадры и остаток».

import { describe, expect, it } from 'vitest';
import { parseSSE } from './sse.js';

describe('parseSSE', () => {
	it('отдаёт кадр, пришедший целиком', () => {
		const { frames, carry } = parseSSE('data: {"type":"delta","text":"при"}\n\n', '');

		expect(frames).toEqual([{ type: 'delta', text: 'при' }]);
		expect(carry).toBe('');
	});

	it('склеивает кадр, разрезанный между чанками', () => {
		const first = parseSSE('data: {"type":"del', '');
		expect(first.frames).toEqual([]);
		expect(first.carry).toBe('data: {"type":"del');

		const second = parseSSE('ta","text":"при"}\n\n', first.carry);
		expect(second.frames).toEqual([{ type: 'delta', text: 'при' }]);
		expect(second.carry).toBe('');
	});

	it('собирает многострочный data в один кадр через перевод строки', () => {
		// По отдельности строки data не JSON — кадр получается только склейкой.
		const { frames } = parseSSE('data: {\ndata: "type":"brief",\ndata: "status":"ready"\ndata: }\n\n', '');

		expect(frames).toEqual([{ type: 'brief', status: 'ready' }]);
	});

	it('хранит хвостовой огрызок без пустой строки и дополняет его следующим чанком', () => {
		const first = parseSSE('data: {"a":1}\n\ndata: {"b"', '');

		expect(first.frames).toEqual([{ a: 1 }]);
		expect(first.carry).toBe('data: {"b"');

		const second = parseSSE(':2}\n\n', first.carry);
		expect(second.frames).toEqual([{ b: 2 }]);
		expect(second.carry).toBe('');
	});

	it('игнорирует комментарии и строки, не начинающиеся с data:', () => {
		const { frames, carry } = parseSSE(': keep-alive\nevent: ping\nid: 7\ndata: {"ok":true}\n\n', '');

		expect(frames).toEqual([{ ok: true }]);
		expect(carry).toBe('');
	});

	it('пропускает кадр с битым JSON и продолжает разбор', () => {
		const { frames, carry } = parseSSE('data: {oops\n\ndata: {"ok":1}\n\n', '');

		expect(frames).toEqual([{ ok: 1 }]);
		expect(carry).toBe('');
	});

	it('переживает переводы строк CRLF и несколько кадров в одном чанке', () => {
		const { frames } = parseSSE('data: {"n":1}\r\n\r\ndata: {"n":2}\r\n\r\n', '');

		expect(frames).toEqual([{ n: 1 }, { n: 2 }]);
	});

	it('возвращает пустой остаток, когда кадров нет вовсе', () => {
		const { frames, carry } = parseSSE('', '');

		expect(frames).toEqual([]);
		expect(carry).toBe('');
	});
});
