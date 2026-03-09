import { t } from 'elysia';
import { JWT_SECRET } from '../env';

export const authCookie = {
	secrets: JWT_SECRET,
	sign: ['license'],
};

export const authCookieSchema = t.Cookie(
	{
		license: t.Optional(
			t.Object({
				expiresAt: t.Numeric(),
			}),
		),
	},
	authCookie,
);
