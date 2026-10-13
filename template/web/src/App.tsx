import type { Bridge, BridgeStorage, PanelContext } from "@radutopala/plugin-ui";
import { useCallback, useEffect, useState } from "react";
import { ago, plural } from "./format";
import { parseRoute, type Route, routeHash } from "./route";
import type { Stats } from "./types";

const ROUTE_KEY = "route";

function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/** The route in the location hash, starting from the one storage kept. */
function useRoute(storage: BridgeStorage): Route {
  const [route, setRoute] = useState<Route>(() => parseRoute(location.hash || routeHash(parseRoute(storage.getItem(ROUTE_KEY) ?? ""))));
  useEffect(() => {
    const onHash = () => {
      const next = parseRoute(location.hash);
      storage.setItem(ROUTE_KEY, next);
      setRoute(next);
    };
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, [storage]);
  return route;
}

export function App({ bridge, storage }: { bridge: Bridge; storage: BridgeStorage }) {
  const route = useRoute(storage);
  return (
    <main>
      <nav>
        <a href={routeHash("count")} aria-current={route === "count"}>
          Count
        </a>
        <a href={routeHash("about")} aria-current={route === "about"}>
          About
        </a>
      </nav>
      {route === "count" ? <CountView bridge={bridge} /> : <AboutView bridge={bridge} />}
    </main>
  );
}

function CountView({ bridge }: { bridge: Bridge }) {
  const [stats, setStats] = useState<Stats | null>(null);
  const [glob, setGlob] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    bridge.api.get<Stats>("stats").then(setStats, (err) => setError(message(err)));
    const read = (s: Record<string, unknown>) => setGlob(typeof s.glob === "string" ? s.glob : "");
    bridge.settings.get().then(read, () => {});
    return bridge.settings.onChange(read);
  }, [bridge]);

  const count = useCallback(async () => {
    setBusy(true);
    setError("");
    try {
      setStats(await bridge.api.post<Stats>("count"));
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  }, [bridge]);

  const last = stats?.last;
  return (
    <section>
      <p className="muted">Counting {glob ? <code>{glob}</code> : "every file"}; change it in Settings → Plugins.</p>
      {last ? (
        <p className="big">
          {plural(last.files, "file")}
          {last.truncated ? "+" : ""} <span className="muted">{ago(last.at)}</span>
        </p>
      ) : (
        <p className="muted">{stats ? "Not counted yet." : "Loading…"}</p>
      )}
      <button type="button" onClick={count} disabled={busy}>
        {busy ? "Counting…" : "Count now"}
      </button>
      {error && <p className="error">{error}</p>}
    </section>
  );
}

function AboutView({ bridge }: { bridge: Bridge }) {
  const [ctx, setCtx] = useState<PanelContext | null>(null);
  useEffect(() => {
    bridge.context.get().then(setCtx, () => {});
  }, [bridge]);
  return (
    <section>
      <p>
        Plugin <code>{ctx?.pluginId}</code> in channel <strong>{ctx?.channelName}</strong>.
      </p>
      <p className="muted">The count action runs from Tasks and workflows too, and leaves a notification.</p>
    </section>
  );
}
