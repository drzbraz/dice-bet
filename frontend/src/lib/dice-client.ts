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
      socket.onclose = () => {
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
}

/** Money is int64 minor units (cents) on the backend. */
export const formatMoney = (cents: number, currency = "EUR") =>
  new Intl.NumberFormat("en-IE", { style: "currency", currency }).format(cents / 100);
