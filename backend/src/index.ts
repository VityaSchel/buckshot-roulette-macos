import { Elysia, redirect } from 'elysia';
import { PUBLIC_URL } from './env';
import { appAuth, authCookie, authCookieSchema } from './auth';

const appProtected = new Elysia({
	cookie: authCookie,
})
	.use(appAuth)
	.get(
		'/download',
		async ({ cookie: { license }, redirect }) => {
			const token = license!.value;
			if (!token || token.expiresAt <= Date.now()) {
				return redirect(new URL('#auth', PUBLIC_URL).href);
			}

			return Bun.file(
				new URL(
					'../../static/buckshot-roulette-macos-v1.2.zip',
					import.meta.url,
				),
			);
		},
		{
			cookie: authCookieSchema,
		},
	);

const app = new Elysia()
	.onError(({ error, code, set }) => {
		if (code === 'NOT_FOUND') {
			set.status = 404;
			return { ok: false, error: 'Not found' };
		} else if (code === 'INVALID_COOKIE_SIGNATURE') {
			return redirect('/logout');
		} else {
			set.status = 500;
			console.error(error);
			return { ok: false, error: 'Internal server error' };
		}
	})
	.get('/logout', ({ cookie }) => {
		cookie.license?.remove();
		return redirect(new URL('#try-again', PUBLIC_URL).href);
	})
	.use(appProtected);

app.listen(Bun.env.PORT || 3001, ({ hostname, port, protocol }) => {
	console.log(`Server running on ${protocol}://${hostname}:${port}`);
});
