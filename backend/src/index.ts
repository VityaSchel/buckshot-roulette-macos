import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { Elysia, redirect } from 'elysia';
import { PUBLIC_URL } from './env';
import { appAuth } from './auth';
import { authCookie, authCookieSchema } from './auth/cookie';

const downloadsDir =
	Bun.env.DOWNLOADS_DIR ||
	fileURLToPath(new URL('../../static', import.meta.url));

// One byte range (start-end, start- or -suffix) lets a browser resume a paused download; any other Range header gets the whole file
function byteRange(header: string | undefined, size: number) {
	const match = header?.match(/^bytes=(\d*)-(\d*)$/);
	if (!match || (match[1] === '' && match[2] === '')) return undefined;
	if (match[1] === '') {
		const suffix = Number(match[2]);
		return suffix === 0 ? 'unsatisfiable' : { start: Math.max(0, size - suffix), end: size - 1 };
	}
	const start = Number(match[1]);
	const end = match[2] === '' ? size - 1 : Math.min(Number(match[2]), size - 1);
	if (start >= size) return 'unsatisfiable';
	return end < start ? undefined : { start, end };
}

const appProtected = new Elysia({
	cookie: authCookie,
})
	.use(appAuth)
	.get(
		'/download',
		async ({ cookie: { license }, redirect, headers, set }) => {
			const token = license!.value;
			if (!token || token.expiresAt <= Date.now()) {
				return redirect(new URL('#auth', PUBLIC_URL).href);
			}

			const file = Bun.file(join(downloadsDir, 'buckshot-roulette-macos-v1.2.zip'));
			const range = byteRange(headers.range, file.size);
			set.headers['accept-ranges'] = 'bytes';
			if (range === 'unsatisfiable') {
				set.status = 416;
				set.headers['content-range'] = `bytes */${file.size}`;
				return '';
			}
			if (range) {
				set.status = 206;
				set.headers['content-range'] = `bytes ${range.start}-${range.end}/${file.size}`;
				return file.slice(range.start, range.end + 1, file.type);
			}
			return file;
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

app.listen(
	{ hostname: Bun.env.HOST || '127.0.0.1', port: Bun.env.PORT || 3001 },
	({ hostname, port, protocol }) => {
		console.log(`Server running on ${protocol}://${hostname}:${port}`);
	},
);
