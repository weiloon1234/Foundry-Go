import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { pathToFileURL } from "node:url";

const { accessTokenFrom, cookieRefresh } = await import(pathToFileURL(process.argv[2]).href);
const { APIError } = await import(pathToFileURL(process.argv[3]).href);

const issued = (expiresIn) => ({
  tokens: { access_token: "a1", expires_in: expiresIn },
});

describe("cookieRefresh", () => {
  it("renews with the access token and its expiry", async () => {
    const outcome = await cookieRefresh(async () => issued("900"));
    assert.equal(outcome.kind, "renewed");
    assert.deepEqual(accessTokenFrom(issued("900"), () => 1_000), {
      value: "a1",
      expiresAt: 901_000,
    });
  });

  it("treats a 401 as signed out: no valid refresh cookie", async () => {
    assert.deepEqual(
      await cookieRefresh(async () => {
        throw new APIError(401, "unauthenticated", undefined);
      }),
      { kind: "signed-out" },
    );
  });

  it("rethrows anything else as an unknown outcome", async () => {
    const offline = new TypeError("network");
    await assert.rejects(
      cookieRefresh(async () => {
        throw offline;
      }),
      error => error === offline,
    );
    await assert.rejects(
      cookieRefresh(async () => {
        throw new APIError(503, "unavailable", undefined);
      }),
      APIError,
    );
  });

  it("rejects a malformed lifetime", () => {
    assert.throws(() => accessTokenFrom(issued("soon")), TypeError);
  });
});
