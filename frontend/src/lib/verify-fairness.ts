/**
 * Independent, client-side reproduction of the backend's roll derivation
 * (internal/domain/fairness.go ComputeRoll): HMAC-SHA256(key=serverSeed,
 * message="clientSeed:nonce"), first 4 bytes as a big-endian uint32,
 * reduced mod 6, plus one. Runs entirely via the browser's native Web
 * Crypto API -- no network call, no trust in this server's own code.
 *
 * This only works once a seed has been revealed (via seed.rotate): while
 * active, the server withholds serverSeed on purpose (see README
 * "Provably fair rolls").
 */
export async function verifyRoll(
  serverSeed: string,
  clientSeed: string,
  nonce: number,
): Promise<number> {
  const key = await crypto.subtle.importKey(
    "raw",
    new TextEncoder().encode(serverSeed),
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"],
  );
  const signature = await crypto.subtle.sign(
    "HMAC",
    key,
    new TextEncoder().encode(`${clientSeed}:${nonce}`),
  );
  const bytes = new Uint8Array(signature);
  const n = ((bytes[0] << 24) | (bytes[1] << 16) | (bytes[2] << 8) | bytes[3]) >>> 0;
  return (n % 6) + 1;
}

/** SHA-256(serverSeed) hex-encoded, for confirming a revealed seed matches its previously published commitment. */
export async function hashServerSeed(serverSeed: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(serverSeed));
  return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, "0")).join("");
}
