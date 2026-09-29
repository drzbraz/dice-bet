# Dice Bet UI

A friendly client for the [dice-bet](../README.md) backend: bet on whether the next roll is even or odd, watch it roll, collect your winnings.

Originally scaffolded with [Lovable](https://lovable.dev); it now lives as a normal part of the `dice-bet` monorepo and is no longer connected to a separate Lovable-synced repository — changes here are committed and pushed like any other part of this repo, not synced back to a Lovable project.

## Development

```sh
cd frontend
npm i
npm run dev   # http://localhost:5173
```

Requires the backend running (`docker compose up` or `make run` from the repo root — see the [main README](../README.md#how-to-run)) for anything beyond the empty shell to work; the "Game server" field in the UI points at it (`ws://localhost:8080/ws` by default, overridable per-session there or at build time via `VITE_DICE_SERVER_URL`, see `.env.example`).
