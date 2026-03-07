import { z } from 'zod';

export function fieldsToZodObject<T extends readonly string[]>(fields: T) {
	return Object.fromEntries(fields.map((key) => [key, z.string()])) as {
		[K in T[number]]: z.ZodString;
	};
}
