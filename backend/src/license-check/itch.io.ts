import z from 'zod';
import { CACHE_CHECK_FALSE_LIFETIME_MS, CACHE_CHECK_TRUE_LIFETIME_MS } from '.';

const BUCKSHOT_ROULETTE_ITCH_IO_GAME_ID = 2443623;

const itchIoLicenseCheckCache = new Map<
	number,
	{ expiresAt: number; result: boolean }
>();

export async function checkItchIoLicense({
	userId,
	token,
}: {
	userId: number;
	token: string;
}): Promise<boolean | 'unknown'> {
	// if (userId === 16665266) return true; // for testing purposes

	const cachedResponse = itchIoLicenseCheckCache.get(userId);
	if (cachedResponse && cachedResponse.expiresAt > Date.now()) {
		return cachedResponse.result;
	}
	const result = await fetchItchIoLicense({ userId, token });
	if (result !== 'unknown') {
		const cacheCheckLifetimeMs =
			result === true
				? CACHE_CHECK_TRUE_LIFETIME_MS
				: CACHE_CHECK_FALSE_LIFETIME_MS;
		itchIoLicenseCheckCache.set(userId, {
			expiresAt: Date.now() + cacheCheckLifetimeMs,
			result,
		});
	}
	return result;
}

async function fetchItchIoLicense({
	userId,
	token,
}: {
	userId: number;
	token: string;
}): Promise<boolean | 'unknown'> {
	try {
		async function getPage(page: number) {
			return await fetch(
				'https://api.itch.io/profile/owned-keys?' +
					new URLSearchParams({
						page: String(page),
					}),
				{
					headers: {
						Authorization: 'Bearer ' + token,
					},
				},
			)
				.then((res) => res.json())
				.then((res) =>
					z
						.object({
							owned_keys: z.object().or(
								z.array(
									z.object({
										gameId: z.number().int().nonnegative(),
										ownerId: z.number().int().nonnegative(),
									}),
								),
							),
						})
						.parse(res),
				);
		}
		const MAX_PAGES_SCAN = 50;
		for (let i = 1; i <= MAX_PAGES_SCAN; i++) {
			const page = await getPage(i);
			if (Array.isArray(page.owned_keys) && page.owned_keys.length > 0) {
				if (
					page.owned_keys.some(
						(k) =>
							k.gameId === BUCKSHOT_ROULETTE_ITCH_IO_GAME_ID &&
							k.ownerId === userId,
					)
				) {
					return true;
				}
			} else {
				return false;
			}
		}
		return false;
	} catch (e) {
		console.error(e);
		return 'unknown';
	}
}
