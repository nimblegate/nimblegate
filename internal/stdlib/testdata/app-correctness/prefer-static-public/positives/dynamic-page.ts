// Positive: dynamic env import in TypeScript. Same pattern, different
// file type — frame catches both.
import { env } from '$env/dynamic/public';
export const apiBase: string = env.PUBLIC_API_URL;
