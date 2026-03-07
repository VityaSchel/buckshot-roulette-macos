import {
	joseAlgorithmHS256,
	encodeJWT,
	parseJWT,
	createJWTSignatureMessage,
	JWSRegisteredHeaders,
	JWTRegisteredClaims,
} from '@oslojs/jwt';
import { hmac } from '@noble/hashes/hmac.js';
import { sha256 } from '@noble/hashes/sha2.js';

export function encode({
	payload,
	secret,
}: {
	payload: unknown;
	secret: Uint8Array;
}): string {
	const header = { alg: joseAlgorithmHS256, typ: 'JWT' };

	const headerJSON = JSON.stringify(header);
	const payloadJSON = JSON.stringify(payload);

	const signatureMessage = createJWTSignatureMessage(headerJSON, payloadJSON);
	const signature = hmac(sha256, secret, signatureMessage);

	return encodeJWT(headerJSON, payloadJSON, signature);
}

export function decode({
	jwt,
	secret,
}: {
	jwt: string;
	secret: Uint8Array;
}): unknown {
	const [header, payload, signature, signatureMessage] = parseJWT(jwt);

	const headerParams = new JWSRegisteredHeaders(header);
	const algorithm = headerParams.algorithm();

	if (algorithm !== joseAlgorithmHS256) {
		throw new Error('Unsupported algorithm');
	}

	const expectedSignature = hmac(sha256, secret, signatureMessage);

	if (!crypto.timingSafeEqual(expectedSignature, signature)) {
		throw new Error('Invalid signature');
	}

	const claims = new JWTRegisteredClaims(payload);
	if (!claims.verifyExpiration()) throw new Error('Token expired');
	if (claims.hasNotBefore() && !claims.verifyNotBefore())
		throw new Error('Token not valid yet');

	return payload;
}
