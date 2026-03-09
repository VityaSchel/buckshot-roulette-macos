import Elysia from 'elysia';
import { appAuthSteam } from './steam';

export const appAuth = new Elysia({
	prefix: '/auth',
}).use(appAuthSteam);
