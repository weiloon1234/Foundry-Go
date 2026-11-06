import { createClient, fetchTransport, type HTTPTransport } from "./contracts_foundry.gen.js";
import { fetchTransport as surfaceTransport } from "./contracts_browser_foundry.gen.js";
import { fetchTransport as standaloneTransport } from "./legacy.js";

export function fetchClients(): void {
  const transport: HTTPTransport = fetchTransport;
  createClient(transport, { credentials: "include" });
  createClient(surfaceTransport);
  createClient(standaloneTransport);
  // Custom transport injection keeps the same request and response contract.
  createClient(async request => transport(request));
}
