import { createFileRoute } from "@tanstack/react-router";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { Die } from "@/components/Die";
import {
  DiceClient,
  DiceError,
  fetchClients,
  formatMoney,
  type BetType,
  type ConnectionState,
  type PlayStartData,
} from "@/lib/dice-client";

export const Route = createFileRoute("/")({
  head: () => ({
    meta: [
      { title: "Even or Odd — Dice Bet" },
      {
        name: "description",
        content:
          "Bet on whether the next dice roll lands even or odd, then collect your winnings. A friendly client for the dice-bet game server.",
      },
      { property: "og:title", content: "Even or Odd — Dice Bet" },
      {
        property: "og:description",
        content: "Bet even or odd, roll the die, collect your winnings.",
      },
    ],
  }),
  component: GamePage,
});

const CHIPS = [100, 500, 1000, 2500];
// Overridable at build time via VITE_DICE_SERVER_URL so a deployed build of
// this frontend can point at a deployed backend by default; the "Game
// server" field below still lets a player override it per-session.
const DEFAULT_URL = import.meta.env["VITE_DICE_SERVER_URL"] ?? "ws://localhost:8080/ws";

type HistoryEntry = {
  id: string;
  betType: BetType;
  betAmount: number;
  rolledNumber: number;
  result: "WIN" | "LOSE";
  credited: number;
};

function GamePage() {
  const [serverUrl, setServerUrl] = useState(DEFAULT_URL);
  const [clientId, setClientId] = useState("alice");
  const [clients, setClients] = useState<string[]>([]);
  const [clientsError, setClientsError] = useState<string | null>(null);
  const [state, setState] = useState<ConnectionState>("idle");
  const [balance, setBalance] = useState<number | null>(null);
  const [currency, setCurrency] = useState("EUR");
  const [betAmount, setBetAmount] = useState(500);
  const [betType, setBetType] = useState<BetType>("EVEN");
  const [play, setPlay] = useState<PlayStartData | null>(null);
  const [rolling, setRolling] = useState(false);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<{ tone: "error" | "info"; text: string } | null>(null);
  const [history, setHistory] = useState<HistoryEntry[]>([]);
  const clientRef = useRef<DiceClient | null>(null);
  // The player actually sat at the table by the last successful connect,
  // as opposed to `clientId`, which tracks the select's current value and
  // can change (e.g. switching players) without a new connect happening
  // yet. Compared against `clientId` below to tell "Reconnect" (same
  // player, dropped connection) apart from "Sit at the table" (a
  // different player was picked, or there's no connection at all).
  const [seatedClientId, setSeatedClientId] = useState<string | null>(null);

  const connected = state === "open";

  const fail = useCallback((err: unknown) => {
    const text =
      err instanceof DiceError ? err.message : ((err as Error)?.message ?? "Something went wrong.");
    setMessage({ tone: "error", text });
  }, []);

  // Loads the player picker from the backend, debounced so editing the
  // "Game server" field doesn't fire a request per keystroke. Clients are
  // seeded via migration, not created here -- see the README.
  useEffect(() => {
    const timer = setTimeout(() => {
      fetchClients(serverUrl)
        .then((ids) => {
          setClients(ids);
          setClientsError(null);
          setClientId((current) => (ids.includes(current) ? current : (ids[0] ?? "")));
        })
        .catch(() => {
          setClients([]);
          setClientsError("Couldn't load the player list from that server.");
        });
    }, 400);
    return () => clearTimeout(timer);
  }, [serverUrl]);

  const connect = useCallback(async () => {
    setBusy(true);
    setMessage(null);
    try {
      clientRef.current?.disconnect();
      const client = new DiceClient(serverUrl);
      client.onStateChange = setState;
      clientRef.current = client;
      await client.connect();
      const trimmedClientId = clientId.trim();
      const wallet = await client.getWallet(trimmedClientId);
      if (seatedClientId !== null && seatedClientId !== trimmedClientId) {
        setHistory([]);
      }
      setSeatedClientId(trimmedClientId);
      setBalance(wallet.balance);
      setCurrency(wallet.currency || "EUR");
      setPlay(null);
      setMessage({ tone: "info", text: `Welcome back, ${wallet.clientId}! Place your bet.` });
    } catch (err) {
      fail(err);
    } finally {
      setBusy(false);
    }
  }, [clientId, fail, seatedClientId, serverUrl]);

  useEffect(() => () => clientRef.current?.disconnect(), []);

  const roll = async () => {
    const client = clientRef.current;
    if (!client) return;
    setBusy(true);
    setRolling(true);
    setMessage(null);
    const started = Date.now();
    try {
      const result = await client.startPlay(clientId.trim(), betAmount, betType);
      const wait = Math.max(0, 750 - (Date.now() - started));
      await new Promise((r) => setTimeout(r, wait));
      setPlay(result);
      setBalance(result.balance);
    } catch (err) {
      fail(err);
    } finally {
      setRolling(false);
      setBusy(false);
    }
  };

  const collect = async () => {
    const client = clientRef.current;
    if (!client || !play) return;
    setBusy(true);
    setMessage(null);
    try {
      const ended = await client.endPlay(clientId.trim());
      setBalance(ended.balance);
      setHistory((h) =>
        [
          {
            id: ended.playId,
            betType: play.betType,
            betAmount: play.betAmount,
            rolledNumber: play.rolledNumber,
            result: ended.result,
            credited: ended.creditedAmount,
          },
          ...h,
        ].slice(0, 8),
      );
      setPlay(null);
      setMessage({
        tone: "info",
        text:
          ended.result === "WIN"
            ? `${formatMoney(ended.creditedAmount, currency)} added to your wallet!`
            : "Round closed. Better luck next roll!",
      });
    } catch (err) {
      fail(err);
    } finally {
      setBusy(false);
    }
  };

  const canBet = useMemo(
    () => connected && !busy && !play && betAmount > 0 && (balance ?? 0) >= betAmount,
    [balance, betAmount, busy, connected, play],
  );

  return (
    <main className="mx-auto flex min-h-screen w-full max-w-5xl flex-col gap-6 px-4 py-8 sm:px-6">
      <header className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-4xl font-bold">Even or Odd</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            Bet on the next roll. Guess right and you double up.
          </p>
        </div>
        <div className="flex items-center gap-2 rounded-full bg-card px-4 py-2 text-sm shadow-[var(--shadow-soft)]">
          <span
            className={`size-2.5 rounded-full ${
              connected
                ? "bg-success"
                : state === "connecting"
                  ? "bg-accent"
                  : "bg-muted-foreground/50"
            }`}
          />
          {connected ? "Connected" : state === "connecting" ? "Connecting…" : "Not connected"}
        </div>
      </header>

      {/* Table setup */}
      <section className="card-soft grid gap-4 p-5 sm:grid-cols-[1fr_1fr_auto] sm:items-end">
        <label className="flex flex-col gap-1.5 text-sm font-semibold">
          Player
          <select
            value={clientId}
            onChange={(e) => setClientId(e.target.value)}
            disabled={clients.length === 0}
            className="rounded-xl border border-input bg-background px-3 py-2 font-normal outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
          >
            {clients.length === 0 ? (
              <option value="">{clientsError ?? "Loading players…"}</option>
            ) : (
              clients.map((id) => (
                <option key={id} value={id}>
                  {id}
                </option>
              ))
            )}
          </select>
        </label>
        <label className="flex flex-col gap-1.5 text-sm font-semibold">
          Game server
          <input
            value={serverUrl}
            onChange={(e) => setServerUrl(e.target.value)}
            placeholder={DEFAULT_URL}
            className="rounded-xl border border-input bg-background px-3 py-2 font-mono text-xs font-normal outline-none focus-visible:ring-2 focus-visible:ring-ring"
          />
        </label>
        <button
          onClick={connect}
          disabled={busy || !clientId.trim()}
          className="h-10 rounded-xl bg-accent px-5 text-sm font-bold text-accent-foreground transition hover:brightness-105 disabled:opacity-50"
        >
          {connected && seatedClientId === clientId ? "Reconnect" : "Sit at the table"}
        </button>
      </section>

      {message && (
        <p
          className={`animate-pop-in rounded-2xl px-4 py-3 text-sm font-semibold ${
            message.tone === "error"
              ? "bg-destructive/10 text-destructive"
              : "bg-accent/10 text-accent"
          }`}
        >
          {message.text}
        </p>
      )}

      <div className="grid gap-6 lg:grid-cols-[1.4fr_1fr]">
        {/* Table */}
        <section className="card-soft flex flex-col items-center gap-6 p-6 sm:p-8">
          <div className="w-full rounded-2xl bg-gradient-warm p-5 text-primary-foreground shadow-[var(--shadow-lift)]">
            <p className="text-xs font-bold tracking-widest uppercase opacity-80">Your wallet</p>
            <p className="mt-1 text-4xl font-bold">
              {balance === null ? "—" : formatMoney(balance, currency)}
            </p>
          </div>

          <Die value={play?.rolledNumber ?? null} rolling={rolling} won={play?.result === "WIN"} />

          {play ? (
            <div
              className={`text-center ${play.result === "WIN" ? "animate-win-pop" : "animate-pop-in"}`}
            >
              <p
                className={`text-2xl font-bold ${play.result === "WIN" ? "text-success" : "text-destructive"}`}
              >
                {play.result === "WIN" ? "You won!" : "No luck this time"}
              </p>
              <p className="mt-1 text-sm text-muted-foreground">
                Rolled {play.rolledNumber} ({play.rolledNumber % 2 === 0 ? "even" : "odd"}) · you
                picked {play.betType.toLowerCase()}
              </p>
              <button
                onClick={collect}
                disabled={busy}
                className="mt-4 rounded-full bg-primary px-8 py-3 font-bold text-primary-foreground shadow-[var(--shadow-soft)] transition hover:brightness-105 disabled:opacity-50"
              >
                {play.result === "WIN"
                  ? `Collect ${formatMoney(play.payout, currency)}`
                  : "Close round"}
              </button>
            </div>
          ) : (
            <div className="w-full space-y-5">
              <div>
                <p className="mb-2 text-sm font-bold">Pick a side</p>
                <div className="grid grid-cols-2 gap-3">
                  {(["EVEN", "ODD"] as BetType[]).map((type) => (
                    <button
                      key={type}
                      onClick={() => setBetType(type)}
                      className={`rounded-2xl px-4 py-4 text-lg font-bold transition ${
                        betType === type
                          ? "bg-gradient-warm text-primary-foreground shadow-[var(--shadow-soft)]"
                          : "bg-secondary text-secondary-foreground hover:bg-muted"
                      }`}
                    >
                      {type === "EVEN" ? "Even" : "Odd"}
                      <span className="block text-xs font-semibold opacity-70">
                        {type === "EVEN" ? "2 · 4 · 6" : "1 · 3 · 5"}
                      </span>
                    </button>
                  ))}
                </div>
              </div>

              <div>
                <p className="mb-2 text-sm font-bold">Bet amount</p>
                <div className="flex flex-wrap gap-2">
                  {CHIPS.map((chip) => (
                    <button
                      key={chip}
                      onClick={() => setBetAmount(chip)}
                      className={`rounded-full px-4 py-2 text-sm font-bold transition ${
                        betAmount === chip
                          ? "bg-accent text-accent-foreground"
                          : "bg-secondary text-secondary-foreground hover:bg-muted"
                      }`}
                    >
                      {formatMoney(chip, currency)}
                    </button>
                  ))}
                  <input
                    type="number"
                    min={1}
                    value={betAmount}
                    onChange={(e) => setBetAmount(Number(e.target.value))}
                    className="w-28 rounded-full border border-input bg-background px-4 py-2 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    aria-label="Custom bet in cents"
                  />
                </div>
                <p className="mt-1.5 text-xs text-muted-foreground">
                  Amounts are in cents, matching the game server.
                </p>
              </div>

              <button
                onClick={roll}
                disabled={!canBet}
                className="w-full rounded-full bg-primary px-8 py-4 text-lg font-bold text-primary-foreground shadow-[var(--shadow-lift)] transition hover:brightness-105 disabled:opacity-50"
              >
                {rolling
                  ? "Rolling…"
                  : `Roll for ${formatMoney(betAmount || 0, currency)} on ${betType.toLowerCase()}`}
              </button>
              {connected && balance !== null && balance < betAmount && (
                <p className="text-center text-xs font-semibold text-destructive">
                  That bet is bigger than your balance.
                </p>
              )}
            </div>
          )}
        </section>

        {/* Side panel */}
        <aside className="flex flex-col gap-6">
          <section className="card-soft p-5">
            <h2 className="text-lg font-bold">How it works</h2>
            <ol className="mt-3 space-y-2 text-sm text-muted-foreground">
              <li>1. Sit at the table to load your wallet.</li>
              <li>2. Pick even or odd and a bet — the stake leaves your wallet right away.</li>
              <li>3. Collect to close the round; a win pays back double.</li>
            </ol>
            <p className="mt-3 text-xs text-muted-foreground">
              One round at a time: you must close the current round before rolling again.
            </p>
          </section>

          <section className="card-soft p-5">
            <h2 className="text-lg font-bold">Recent rounds</h2>
            {history.length === 0 ? (
              <p className="mt-3 text-sm text-muted-foreground">No rounds played yet.</p>
            ) : (
              <ul className="mt-3 space-y-2">
                {history.map((h) => (
                  <li
                    key={h.id}
                    className="flex items-center justify-between rounded-xl bg-secondary px-3 py-2 text-sm"
                  >
                    <span className="font-semibold">
                      {h.rolledNumber} · {h.betType.toLowerCase()}
                    </span>
                    <span
                      className={`font-bold ${h.result === "WIN" ? "text-success" : "text-destructive"}`}
                    >
                      {h.result === "WIN"
                        ? `+${formatMoney(h.credited - h.betAmount, currency)}`
                        : `-${formatMoney(h.betAmount, currency)}`}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </section>
        </aside>
      </div>
    </main>
  );
}
