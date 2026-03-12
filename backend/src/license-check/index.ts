import Elysia from 'elysia';
import { PUBLIC_URL } from '../env';
import { authCookieSchema } from '../auth/cookie';

export const CACHE_CHECK_TRUE_LIFETIME_MS = 1000 * 60 * 60 * 24 * 7; // 7 days
export const CACHE_CHECK_FALSE_LIFETIME_MS = 1000 * 60; // 1 minute

export const licenseMacro = (service: 'steam' | 'itch.io') =>
	new Elysia({ name: 'license-macro' }).macro({
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
					return redirect(
						new URL(`#error-${service}-license-unknown`, PUBLIC_URL).href,
					);
				} else if (responseValue === false) {
					return redirect(
						new URL(`#error-${service}-no-license`, PUBLIC_URL).href,
					);
				} else {
					return responseValue;
				}
			},
		},
	});
