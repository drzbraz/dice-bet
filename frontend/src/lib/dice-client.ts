/**
 * Browser WebSocket client for the dice-bet Go backend.
 *
 * Envelope: { type, requestId, payload }
 * Success:  { type: "<type>.result", requestId, success: true, data }
 * Error:    { type: "error", requestId, success: false, error: { code, message } }
 */

export type BetType = "EVEN" | "ODD";

export type WalletData = {
  clientId: string;
  balance: number;
  currency: string;
};

/** Present on PlayStartData only when the server has PROVABLY_FAIR_ENABLED=true. */
export type PlayFairness = {
  serverSeedHash: string;
  clientSeed: string;
  nonce: number;
};

export type PlayStartData = {
  playId: string;
  clientId: string;
  betAmount: number;
  betType: BetType;
  rolledNumber: number;
  result: "WIN" | "LOSE";
  payout: number;
  status: string;
  balance: number;
  fairness?: PlayFairness;
};

export type SeedData = {
  clientId: string;
  serverSeedHash: string;
  clientSeed: string;
  nonce: number;
};

export type RetiredSeed = {
  clientId: string;
  serverSeed: string;
  serverSeedHash: string;
  clientSeed: string;
  finalNonce: number;
  retiredAt: string;
};

export type SeedRotateData = {
  retired?: RetiredSeed;
  active: SeedData;
};

export type PlayEndData = {
  playId: string;
  clientId: string;
  result: "WIN" | "LOSE";
  creditedAmount: number;
  status: string;
  balance: number;
};

export class DiceError extends Error {
  code: string;
  constructor(code: string, message: string) {
    super(message);
    this.code = code;
  }
}

type Pending = {
  resolve: (data: unknown) => void;
  reject: (err: Error) => void;
  timer: ReturnType<typeof setTimeout>;
};

export type ConnectionState = "idle" | "connecting" | "open" | "closed";

const uuid = () =>
  typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(16).slice(2)}`;

export class DiceClient {
  private socket: WebSocket | null = null;
  private pending = new Map<string, Pending>();
  private url: string;

  onStateChange: (state: ConnectionState) => void = () => {};

  constructor(url: string) {
    this.url = url;
  }

  connect(): Promise<void> {
    if (this.socket && this.socket.readyState === WebSocket.OPEN) {
      return Promise.resolve();
    }
    this.onStateChange("connecting");
    return new Promise((resolve, reject) => {
      let socket: WebSocket;
      try {
        socket = new WebSocket(this.url);
      } catch (err) {
        this.onStateChange("closed");
        reject(new DiceError("CONNECTION_FAILED", (err as Error).message));
        return;
      }
      this.socket = socket;

      socket.onopen = () => {
        this.onStateChange("open");
        resolve();
      };
      socket.onerror = () => {
        reject(
          new DiceError(
            "CONNECTION_FAILED",
            `Could not reach the game server at ${this.url}. Is it running?`,
          ),
        );
      };
      socket.onclose = (event) => {
        // wasClean=false / code 1006 means the connection dropped without a
        // WS close handshake (e.g. a network blip or a server-side
        // deadline), as opposed to a normal close from disconnect() or a
        // server-initiated close -- worth telling apart when diagnosing an
        // unexpected disconnect.
        if (!event.wasClean) {
          console.info(`[DiceClient] connection dropped (code ${event.code})`);
        }
        this.onStateChange("closed");
        this.failAll(new DiceError("CONNECTION_CLOSED", "Connection to the game server was lost."));
      };
      socket.onmessage = (event) => this.handleMessage(event.data as string);
    });
  }

  disconnect() {
    this.socket?.close();
    this.socket = null;
  }

  private failAll(err: Error) {
    for (const [, p] of this.pending) {
      clearTimeout(p.timer);
      p.reject(err);
    }
    this.pending.clear();
  }

  private handleMessage(raw: string) {
    let msg: {
      requestId?: string;
      success?: boolean;
      data?: unknown;
      error?: { code?: string; message?: string };
    };
    try {
      msg = JSON.parse(raw);
    } catch {
      return;
    }
    const id = msg.requestId;
    if (!id) return;
    const entry = this.pending.get(id);
    if (!entry) return;
    this.pending.delete(id);
    clearTimeout(entry.timer);
    if (msg.success) {
      entry.resolve(msg.data);
    } else {
      entry.reject(
        new DiceError(
          msg.error?.code ?? "INTERNAL_ERROR",
          msg.error?.message ?? "Something went wrong.",
        ),
      );
    }
  }

  private send<T>(type: string, payload: Record<string, unknown>): Promise<T> {
    const socket = this.socket;
    if (!socket || socket.readyState !== WebSocket.OPEN) {
      return Promise.reject(
        new DiceError("CONNECTION_CLOSED", "Not connected to the game server."),
      );
    }
    const requestId = uuid();
    return new Promise<T>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(requestId);
        reject(new DiceError("TIMEOUT", "The game server did not answer in time."));
      }, 10_000);
      this.pending.set(requestId, {
        resolve: resolve as (data: unknown) => void,
        reject,
        timer,
      });
      socket.send(JSON.stringify({ type, requestId, payload }));
    });
  }

  getWallet(clientId: string) {
    return this.send<WalletData>("wallet.get", { clientId });
  }

  startPlay(clientId: string, betAmount: number, betType: BetType) {
    return this.send<PlayStartData>("play.start", { clientId, betAmount, betType });
  }

  endPlay(clientId: string) {
    return this.send<PlayEndData>("play.end", { clientId });
  }

  /** Current, safe-to-publish commitment. Throws DiceError("FAIRNESS_DISABLED", ...) if the server has the feature off. */
  getSeed(clientId: string) {
    return this.send<SeedData>("seed.get", { clientId });
  }

  /** Retires the active seed (revealing it) and activates a new one. clientSeed is optional. */
  rotateSeed(clientId: string, clientSeed?: string) {
    return this.send<SeedRotateData>(
      "seed.rotate",
      clientSeed ? { clientId, clientSeed } : { clientId },
    );
  }

  /** Past, fully revealed seed epochs, newest first. */
  getSeedHistory(clientId: string) {
    return this.send<{ seeds: RetiredSeed[] }>("seed.history", { clientId });
  }
}

/** Money is int64 minor units (cents) on the backend. */
export const formatMoney = (cents: number, currency = "EUR") =>
  new Intl.NumberFormat("en-IE", { style: "currency", currency }).format(cents / 100);

/**
 * Derives the backend's HTTP base URL from its WebSocket URL
 * (ws(s)://host[:port]/ws -> http(s)://host[:port]), since both transports
 * are served by the same Go process on the same port (see README "HTTP
 * mirror"). Used only to fetch the player list for the picker below; the
 * game itself is played entirely over the WebSocket in DiceClient.
 */
function httpBaseFromWsUrl(wsUrl: string): string {
  return wsUrl.replace(/^ws/, "http").replace(/\/ws\/?$/, "");
}

/**
 * Fetches every known client ID via GET /api/v1/clients, to populate a
 * player picker. There is no "create client" endpoint in this project --
 * see the README's "Frontend" section for how to add one.
 */
export async function fetchClients(wsUrl: string): Promise<string[]> {
  const res = await fetch(`${httpBaseFromWsUrl(wsUrl)}/api/v1/clients`);
  if (!res.ok) {
    throw new DiceError(
      "CONNECTION_FAILED",
      `Could not load the player list (HTTP ${res.status}).`,
    );
  }
  const body = (await res.json()) as { data?: { clients?: string[] } };
  return body.data?.clients ?? [];
}
