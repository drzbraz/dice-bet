// @lovable.dev/vite-tanstack-config already includes the following — do NOT add them manually
// or the app will break with duplicate plugins:
//   - TanStack devtools (dev-only, first), tanstackStart, viteReact, tailwindcss, tsConfigPaths,
//     nitro (build-only using cloudflare as a default target), VITE_* env injection, @ path alias,
//     React/TanStack dedupe, error logger plugins, and sandbox detection (port/host/strictPort).
// You can pass additional config via defineConfig({ vite: { ... }, etc... }) if needed.
import { defineConfig } from "@lovable.dev/vite-tanstack-config";

export default defineConfig({
  tanstackStart: {
    // Redirect TanStack Start's bundled server entry to src/server.ts (our SSR error wrapper).
    // nitro/vite builds from this
    server: { entry: "server" },
  },
  // Outside Lovable's own sandbox, the base config defaults this dev
  // server to :8080 too — the same port the Go backend listens on by
  // default (see ../README.md). Pin it to Vite's own conventional :5173
  // so running both locally at once doesn't have them fight over the
  // port (inside the sandbox this is ignored; port is forced to 8080
  // with strictPort there regardless of this setting).
  vite: { server: { port: 5173 } },
});
