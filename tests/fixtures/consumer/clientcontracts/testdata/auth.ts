import {
  authDescriptor,
  createPortalSession,
  signInIdentifier,
  type ClientOptions,
  type HTTPTransport,
  type PasswordCredentials,
} from "./contracts_browser_foundry.gen.js";
import { SessionCoordinationError, type TokenSession } from "./contracts_runtime_auth_foundry.gen.js";

declare const transport: HTTPTransport;
declare const clientOptions: ClientOptions;

export function portalConsumer(): void {
  const portal = createPortalSession(transport, { clientOptions: { ...clientOptions, requestTimeoutMs: 15_000 }, channel: null });
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

// doc-example:begin credential-lock
export async function lockedSignOut(portal: ReturnType<typeof createPortalSession>) {
  await portal.session.runCredentials(async () => {
    await portal.credentials.browserLogout({});
    portal.session.signOut();
  });
}
// doc-example:end credential-lock

// doc-example:begin session-version
export async function currentProfile(portal: ReturnType<typeof createPortalSession>) {
  const version = portal.session.version();
  const profile = await portal.api.browserProfile({});
  if (portal.session.version() !== version) return undefined;
  return profile;
}
// doc-example:end session-version

// doc-example:begin coordination-failure
export async function restoreProfile(portal: ReturnType<typeof createPortalSession>) {
  try {
    return { kind: "restored", profile: await portal.restoreSession() } as const;
  } catch (error) {
    if (error instanceof SessionCoordinationError) return { kind: "coordination-failed" } as const;
    throw error;
  }
}
// doc-example:end coordination-failure
