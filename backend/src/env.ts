function getEnv(name: string) {
	const value = Bun.env[name];
	if (!value) {
		console.error(`Missing ${name} environment variable`);
		process.exit(1);
	}
	return value;
}

export const STEAM_API_KEY = getEnv('STEAM_API_KEY');
export const JWT_SECRET = getEnv('JWT_SECRET');
export const PUBLIC_URL = getEnv('PUBLIC_URL');
export const PUBLIC_API_URL = getEnv('PUBLIC_API_URL');
