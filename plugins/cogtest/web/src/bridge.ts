// The panel runs in Loop's sandboxed plugin frame and talks to Loop only
// through the MessagePort bridge (JSON-RPC 2.0) handed over in the
// "loop-plugin:init" message.

const CALL_TIMEOUT_MS = 35000;

export interface Theme {
  mode?: string;
  vars?: Record<string, unknown>;
}

/** A plugin event Loop forwards: one the plugin's server published. */
export interface PluginEvent {
  type?: string;
  data?: unknown;
}

export interface ApiResponse<T = unknown> {
  status: number;
  body: T;
}

interface Pending {
  resolve: (v: unknown) => void;
  reject: (e: Error) => void;
  timer: ReturnType<typeof setTimeout>;
}

interface RpcMessage {
  jsonrpc?: string;
  id?: number | null;
  method?: string;
  params?: unknown;
  result?: unknown;
  error?: { message?: string };
}

export interface BridgeHandlers {
  onTheme: (theme: Theme) => void;
  onEvent: (ev: PluginEvent) => void;
}

export class Bridge {
  private nextId = 1;
  private pending = new Map<number, Pending>();

  constructor(
    private port: MessagePort,
    private handlers: BridgeHandlers,
  ) {
    port.onmessage = (event) => this.receive(event.data);
  }

  private receive(data: unknown) {
    let msg = data as RpcMessage | null;
    if (typeof data === "string") {
      try {
        msg = JSON.parse(data);
      } catch {
        return;
      }
    }
    if (msg?.jsonrpc !== "2.0") return;
    if (msg.id !== undefined && msg.id !== null) {
      const p = this.pending.get(msg.id);
      if (!p) return;
      this.pending.delete(msg.id);
      clearTimeout(p.timer);
      if (msg.error) p.reject(new Error(msg.error.message || "bridge error"));
      else p.resolve(msg.result);
      return;
    }
    if (msg.method === "theme") this.handlers.onTheme((msg.params as Theme) || {});
    else if (msg.method === "event") this.handlers.onEvent((msg.params as PluginEvent) || {});
  }

  call<T = unknown>(method: string, params?: unknown): Promise<T> {
    return new Promise((resolve, reject) => {
      const id = this.nextId++;
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`${method} timed out`));
      }, CALL_TIMEOUT_MS);
      this.pending.set(id, { resolve: resolve as (v: unknown) => void, reject, timer });
      const msg: RpcMessage = { jsonrpc: "2.0", id, method };
      if (params !== undefined) msg.params = params;
      this.port.postMessage(msg);
    });
  }

  /** Calls the plugin's own /x API. */
  async api<T = unknown>(method: string, path: string, body?: unknown): Promise<ApiResponse<T>> {
    const params: Record<string, unknown> = { method, path };
    if (body !== undefined) params.body = body;
    const res = await this.call<ApiResponse<T> | null>("api.request", params);
    return res || { status: 0, body: null as T };
  }
}

/** Resolves with the port Loop hands the frame. Loop posts it as the frame
 *  loads, so call this before anything renders, or the message is missed;
 *  messages on the port wait until the bridge listens. */
export function listen(): Promise<MessagePort> {
  return new Promise((resolve) => {
    const onMessage = (event: MessageEvent) => {
      const data = event.data as { type?: string } | null;
      if (data?.type !== "loop-plugin:init" || !event.ports?.[0]) return;
      window.removeEventListener("message", onMessage);
      resolve(event.ports[0]);
    };
    window.addEventListener("message", onMessage);
  });
}

/** The error a failed /x answer carries, or a generic one. */
export function errorOf(res: ApiResponse | null | undefined): string {
  const b = res?.body;
  if (b && typeof b === "object" && "error" in b && typeof b.error === "string" && b.error) return b.error;
  if (typeof b === "string" && b) return b;
  return `request failed (${res ? res.status : "no answer"})`;
}

/** Applies Loop's theme to the document: its mode and its CSS variables. */
export function applyTheme(theme: Theme, root: HTMLElement = document.documentElement) {
  if (theme.mode === "dark" || theme.mode === "light") root.dataset.mode = theme.mode;
  for (const [k, v] of Object.entries(theme.vars || {})) {
    if (k.startsWith("--") && typeof v === "string") root.style.setProperty(k, v);
  }
}
