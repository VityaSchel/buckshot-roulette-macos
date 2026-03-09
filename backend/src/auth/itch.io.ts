import z from 'zod';
import Elysia, { t } from 'elysia';
import { PUBLIC_API_URL } from '../env';
import { authCookie, authCookieSchema } from './cookie';
import { licenseMacro } from '../license-check';
import { checkItchIoLicense } from '../license-check/itch.io';

export const appAuthItchIo = new Elysia({
	prefix: '/itch.io',
	cookie: authCookie,
})
	.use(licenseMacro)
	.get('/', async ({ redirect }) => {
		const returnTo = new URL('auth/itch.io/callback', PUBLIC_API_URL).href;
		const redirectUrl = `https://itch.io/user/oauth?client_id=3ebb49fdae8199285348d6bbbbf77af1&scope=profile:owned&response_type=token&redirect_uri=${encodeURIComponent(
			returnTo,
		)}`;
		return redirect(redirectUrl);
	})
	.get('/callback', async () => {
		return Bun.file(
			new URL('../static/itch.io-callback.html', import.meta.url),
		);
	})
	.post(
		'/callback',
		async ({ body }) => {
			const userId = await fetch('https://api.itch.io/profile', {
				headers: {
					Authorization: `Bearer ` + body.token,
				},
			})
				.then((res) => res.json())
				.then(
					(res) =>
						z
							.object({
								user: z.object({
									id: z.number().int().nonnegative(),
								}),
							})
							.parse(res).user.id,
				);

			let licenseCheckResult: boolean | 'unknown';
			try {
				licenseCheckResult = await checkItchIoLicense({ userId });
			} catch (e) {
				console.error(e);
				set.status = 502;
				return {
					ok: false,
					error:
						'Failed to verify game license in your Steam library, ensure your profile and your games library are public',
				};
			}

			return licenseCheckResult;
		},
		{
			cookie: authCookieSchema,
			checkLicense: true,
			body: t.Object({
				token: t.String({
					minLength: 1,
				}),
			}),
		},
	);
