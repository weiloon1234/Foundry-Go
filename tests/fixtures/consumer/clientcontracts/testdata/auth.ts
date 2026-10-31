import {
  authDescriptor,
  createPortalSession,
  signInIdentifier,
  type ClientOptions,
  type HTTPTransport,
  type PasswordCredentials,
} from "./contracts_browser_foundry.gen.js";
import type { TokenSession } from "./contracts_runtime_auth_foundry.gen.js";

declare const transport: HTTPTransport;
declare const clientOptions: ClientOptions;

export function portalConsumer(): void {
  const portal = createPortalSession(transport, { clientOptions, channel: null });
  const credentials: PasswordCredentials = { login_id: "member@example.test", password: "fixture-password" };
  const identifier: keyof PasswordCredentials = signInIdentifier;
  const session: TokenSession = portal.session;
  void [identifier, session, authDescriptor];
  void portal.signInWithPassword(credentials).then(profile => {
    const display: string = profile.display;
    void display;
  });
  void portal.restoreSession();
  void portal.signOut();
  // @ts-expect-error Credentials use the login DTO's JSON keys and require a password.
  void portal.signInWithPassword({ email: "member@example.test" });
  // @ts-expect-error Client options remain concrete through the portal wrapper.
  createPortalSession(transport, { clientOptions: { strictResponses: "yes" } });
}
