import { Elysia, redirect } from 'elysia';
import { PUBLIC_API_URL, PUBLIC_URL } from './env';
import { appAuth, authCookie, authCookieSchema } from './auth';

const appProtected = new Elysia({
	cookie: authCookie,
})
	.use(appAuth)
	.get(
		'/download',
		async ({ cookie: { license }, redirect, set, headers }) => {
			const token = license!.value;
			if (!token || token.expiresAt <= Date.now()) {
				return redirect(new URL('auth', PUBLIC_API_URL).href);
			}

			let file = Bun.file(
				new URL(
					'../../static/buckshot-roulette-macos-v1.2.zip',
					import.meta.url,
				),
			);
			if (!(await file.exists())) {
				set.status = 500;
				return { ok: false, error: 'Try again later' };
			}

			const range = headers['range'];
			let start = 0;
			let end = file.size - 1;

			if (range) {
				const rangeParts = range.replace(/bytes=/, '').split('-');
				const rangeStart = rangeParts[0];
				if (!rangeStart) {
					set.status = 400;
					return { ok: false, error: "Invalid 'Range' header" };
				}
				start = parseInt(rangeStart, 10);
				end = rangeParts[1] ? parseInt(rangeParts[1], 10) : end;

				if (start >= file.size || end >= file.size) {
					set.headers['Content-Range'] = `bytes */${file.size}`;
					set.status = 416;
					return { ok: false, error: 'Requested Range Not Satisfiable' };
				}

				set.headers['Content-Range'] = `bytes ${start}-${end}/${file.size}`;
				set.headers['Content-Length'] = String(end - start + 1);
			} else {
				set.headers['Content-Length'] = String(file.size);
			}

			// set.headers['Accept-Ranges'] = 'bytes'
			set.headers['Content-Type'] = 'application/octet-stream';
			set.headers['Content-Disposition'] =
				`attachment; filename="buckshot-roulette-macos-v1.2.zip"`;

			if (range) {
				file = file.slice(start, end + 1);
			}

			return file.stream();
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
