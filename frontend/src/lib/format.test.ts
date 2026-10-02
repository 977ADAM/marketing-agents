import { describe, expect, it } from 'vitest';
import { formatCost } from './format.js';

describe('formatCost', () => {
	it('показывает стоимость с четырьмя знаками', () => {
		expect(formatCost(0)).toBe('$0.0000');
		expect(formatCost(0.0123)).toBe('$0.0123');
	});

	it('на отсутствующей стоимости отдаёт прочерк', () => {
		expect(formatCost(undefined)).toBe('—');
		expect(formatCost(null)).toBe('—');
	});
});
