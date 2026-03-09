import Elysia from 'elysia';
import { appAuthSteam } from './steam';
import { appAuthItchIo } from './itch.io';

export const appAuth = new Elysia({
	prefix: '/auth',
})
	.use(appAuthSteam)
	.use(appAuthItchIo);
