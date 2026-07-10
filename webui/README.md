# EVMI Web UI

A [Next.js](https://nextjs.org) app (App Router) for EVMI, built as a **static
export** and served by the Go server as plain files.

After signing in, the sidebar exposes full CRUD for every EVMI entity —
**blockchains, ABIs, log stores, pipelines, log sources, and exporters** — plus
start/stop for sources and exporters.

## Structure

- **Routes** (App Router, one per resource): `/blockchains`, `/abis`, `/stores`,
  `/pipelines`, `/sources`, `/exporters`, plus `/login`. `/` redirects to the
  first resource (or `/login`). The authenticated routes live under the
  `app/(app)/` route group, whose `layout.tsx` guards auth and renders the
  sidebar nav. Every route is statically prerendered (`output: export`), and the
  Go server serves them with an index.html SPA fallback so deep links work.
- **Resource definitions** are split one-per-file under `lib/resources/`
  (`blockchains.ts`, `abis.ts`, …), each declaring its fields, columns, and typed
  client calls; shared types/helpers are in `lib/resources/types.ts` and relation
  dropdown loaders in `lib/resources/options.ts`.
- **`components/ResourceManager.tsx`** is the generic list + create/edit modal +
  delete component that renders any resource.

## Develop

```bash
npm install
# Point the UI at a running EVMI server (the API requires a bearer token):
NEXT_PUBLIC_API_BASE=http://localhost:8080 npm run dev
```

Open http://localhost:3000 and sign in (default `admin` / `admin`).

## Build

```bash
npm install
npm run build      # emits static files to ./out
```

The Go server serves the built output from the directory in `EVMI_WEBUI_DIR`
(default `public`). To serve a local build:

```bash
cp -r out/* ../public/
# then run the server; visit http://localhost:8080
```

In Docker this is automated — a Node stage runs `npm run build` and the output is
copied into the image at `/public` (see the repo `Dockerfile`).

## How it talks to the API

The UI uses a **generated, fully-typed Connect client** ([Connect-ES v2](https://connectrpc.com/docs/web/generating-code/)).

- `buf.gen.yaml` runs `protoc-gen-es` (`target=ts`) over
  `../internal/grpc/proto` and writes the client to `gen/`.
- `lib/client.ts` creates the client with a `connect-web` transport and an
  interceptor that attaches the stored bearer token to every call. Components
  then call typed methods, e.g. `client.login({ username, password })`,
  `client.listEvmiInstances({ pagination: { limit: 100, offset: 0 } })`.

Regenerate after changing the proto:

```bash
npm run generate     # buf generate -> gen/
```

The generated `gen/` directory **is committed** — the Docker image builds the UI
without the proto in its context, so it relies on the checked-in output. Rerun
`npm run generate` and commit whenever `internal/grpc/proto/**` changes.

When the UI is served from the same origin as the API (the normal case),
`NEXT_PUBLIC_API_BASE` can be left unset; for `next dev` point it at the server.
