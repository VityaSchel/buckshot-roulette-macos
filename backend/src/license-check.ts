import z from 'zod';
import { STEAM_API_KEY } from './env';

const BUCKSHOT_ROULETTE_APP_ID = 2835570;

const licenseCheckCache = new Map<
	string,
	{ expiresAt: number; result: boolean }
>();

export const CACHE_CHECK_TRUE_LIFETIME_MS = 1000 * 60 * 60 * 24 * 7; // 7 days
export const CACHE_CHECK_FALSE_LIFETIME_MS = 1000 * 60; // 1 minute

export async function checkSteamLicense({
	steamId,
}: {
	steamId: string;
}): Promise<boolean | 'unknown'> {
	const cachedResponse = licenseCheckCache.get(steamId);
	if (cachedResponse && cachedResponse.expiresAt > Date.now()) {
		return cachedResponse.result;
	}

	if (!STEAM_API_KEY) throw new Error('Missing Steam API key');
	const responseSerialized = await fetch(
		'https://api.steampowered.com/IPlayerService/GetOwnedGames/v1/?' +
			new URLSearchParams({
				key: STEAM_API_KEY,
				input_json: JSON.stringify({
					steamid: steamId,
					appids_filter: [BUCKSHOT_ROULETTE_APP_ID],
					include_appinfo: false,
					include_played_free_games: false,
				}),
			}),
	).then((res) => res.text());

	let responseDeserialized: unknown;
	try {
		responseDeserialized = JSON.parse(responseSerialized);
	} catch {
		console.error(responseSerialized);
		throw new Error('Failed to parse Steam API response');
	}

	try {
		const response = await z
			.object({
				response: z.object({ games: z.array(z.object({ appid: z.number() })) }),
			})
			.parseAsync(responseDeserialized)
			.then((data) => data.response);
		const result = response.games.some(
			(game) => game.appid === BUCKSHOT_ROULETTE_APP_ID,
		);

		const cacheCheckLifetimeMs =
			result === true
				? CACHE_CHECK_TRUE_LIFETIME_MS
				: CACHE_CHECK_FALSE_LIFETIME_MS;
		licenseCheckCache.set(steamId, {
			expiresAt: Date.now() + cacheCheckLifetimeMs,
			result,
		});

		return result;
	} catch {
		return 'unknown';
	}
}
