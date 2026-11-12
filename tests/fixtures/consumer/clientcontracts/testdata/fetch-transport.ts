import { createClient, fetchTransport, type HTTPTransport } from "./contracts_foundry.gen.js";
import { fetchTransport as surfaceTransport } from "./contracts_browser_foundry.gen.js";
import { fetchTransport as standaloneTransport } from "./legacy.js";

export function fetchClients(): void {
// doc-example:begin fetch-client
  const transport: HTTPTransport = fetchTransport;
  createClient(transport, { credentials: "include" });
// doc-example:end fetch-client
  createClient(surfaceTransport, { requestTimeoutMs: 15_000 });
  createClient(standaloneTransport, { requestTimeoutMs: 0 });
  // Custom transport injection keeps the same request and response contract.
  createClient(async request => transport(request));
}
