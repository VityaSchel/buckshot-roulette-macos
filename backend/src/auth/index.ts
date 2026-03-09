import Elysia, { t } from 'elysia';
import { JWT_SECRET, PUBLIC_URL } from '../env';
import { CACHE_CHECK_TRUE_LIFETIME_MS } from '../license-check';
import { appAuthSteam } from './steam';

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

export const appAuth = new Elysia({
	prefix: '/auth',
})
	.macro({
		checkLicense: {
			cookie: authCookieSchema,
			afterHandle({ responseValue, cookie: { license }, redirect }) {
				if (!license) return;
				if (responseValue === true) {
					license.value = {
						expiresAt: Date.now() + CACHE_CHECK_TRUE_LIFETIME_MS,
					};
					license.httpOnly = true;
					license.maxAge = Math.floor(CACHE_CHECK_TRUE_LIFETIME_MS / 1000);
					return redirect(new URL('#success', PUBLIC_URL).href);
				} else if (responseValue === 'unknown') {
					return redirect(new URL('#error-license-unknown', PUBLIC_URL).href);
				} else if (responseValue === false) {
					return redirect(new URL('#error-no-license', PUBLIC_URL).href);
				} else {
					return responseValue;
				}
			},
		},
	})
	.use(appAuthSteam);
