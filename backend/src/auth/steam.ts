import z from 'zod';
import Elysia from 'elysia';
import { PUBLIC_API_URL } from '../env';
import { fieldsToZodObject } from '../utils';
import { checkSteamLicense } from '../license-check/steam';
import { authCookie, authCookieSchema } from '.';

export const appAuthSteam = new Elysia({
	prefix: '/steam',
	cookie: authCookie,
})
	.get('/', async ({ redirect, query }) => {
		let returnTo = new URL('auth/steam/callback', PUBLIC_API_URL).href;
		if (query.file) {
			returnTo += '?' + new URLSearchParams({ file: query.file });
		}
		const redirectUrl = `https://steamcommunity.com/openid/login?openid.ns=http://specs.openid.net/auth/2.0&openid.mode=checkid_setup&openid.return_to=${encodeURIComponent(
			returnTo,
		)}&openid.realm=${PUBLIC_API_URL}&openid.identity=http://specs.openid.net/auth/2.0/identifier_select&openid.claimed_id=http://specs.openid.net/auth/2.0/identifier_select`;
		return redirect(redirectUrl);
	})
	.get(
		'/callback',
		async ({ set, query }) => {
			let steamId: string | null;
			try {
				const STEAM_AUTH_FIELDS = [
					'openid.op_endpoint',
					'openid.claimed_id',
					'openid.identity',
					'openid.return_to',
					'openid.response_nonce',
					'openid.assoc_handle',
					'openid.signed',
					'openid.sig',
				] as const;

				const reqQuery = await z
					.object({
						'openid.ns': z.string(),
						'openid.mode': z.string(),
						...fieldsToZodObject(STEAM_AUTH_FIELDS),
						file: z.string().optional(),
					})
					.safeParseAsync(query);
				if (!reqQuery.success) {
					set.status = 400;
					return {
						ok: false,
						error:
							'Invalid Steam authorization callback, try again in 10-30 minutes',
					};
				}

				const validateParams = new URLSearchParams();
				validateParams.append('openid.ns', reqQuery.data['openid.ns']);
				validateParams.append('openid.mode', 'check_authentication');
				STEAM_AUTH_FIELDS.forEach((key) =>
					validateParams.append(key, reqQuery.data[key]),
				);

				const signedFields = reqQuery.data['openid.signed'].split(',');
				for (const field of signedFields) {
					const key = `openid.${field}`;
					if (key in reqQuery.data) {
						const value = reqQuery.data[
							key as keyof typeof reqQuery.data
						] as string;
						if (!validateParams.has(key)) {
							validateParams.append(key, value);
						}
					}
				}

				const verification = await fetch(
					'https://steamcommunity.com/openid/login',
					{
						method: 'POST',
						headers: {
							'Content-Type': 'application/x-www-form-urlencoded',
						},
						body: validateParams.toString(),
					},
				).then((res) => res.text());
				if (!verification.includes('is_valid:true')) {
					set.status = 401;
					return {
						ok: false,
						error: 'Invalid Steam authorization state',
					};
				}

				steamId =
					reqQuery.data['openid.claimed_id'].match(/\/(\d+)$/)?.[1] ?? null;
				if (!steamId) {
					set.status = 400;
					return {
						ok: false,
						error:
							'Failed to extract Steam ID from authorization callback, try again in 10-30 minutes',
					};
				}
			} catch (e) {
				console.error(e);
				set.status = 502;
				return {
					ok: false,
					error: 'Steam API server error, try again in 10-30 minutes',
				};
			}

			let licenseCheckResult: boolean | 'unknown';
			try {
				licenseCheckResult = await checkSteamLicense({ steamId });
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
		},
	);
