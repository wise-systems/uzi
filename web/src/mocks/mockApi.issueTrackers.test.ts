// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ApiError } from "../lib/apiError";

// The mock half of Settings → Issue trackers (PRD #4173 M6). Each case imports a fresh
// module graph so the in-memory connection list starts from the seed.

// Assembled at runtime so no token-shaped literal sits in the source.
const TOKEN = ["perm", "bW9jaw", "not-a-real-token"].join(":");

beforeEach(() => vi.resetModules());
afterEach(() => vi.resetModules());

async function load() {
  const { mockApi } = await import("./mockApi");
  const { repos } = await import("./mockApi/forge");
  return { mockApi, repos };
}

describe("mock issue tracker connections", () => {
  it("serves the default allowlist, kind and one connection", async () => {
    const { mockApi } = await load();
    expect(await mockApi.issueTrackerConfig()).toEqual({
      allowed_base_urls: ["https://youtrack.example.com"],
      kinds: ["youtrack"],
    });
    const { connections } = await mockApi.listIssueTrackerConnections();
    expect(connections).toHaveLength(1);
    expect(connections[0]).toMatchObject({ kind: "youtrack", base_url: "https://youtrack.example.com", account_login: "uzi-bot" });
    expect(Object.keys(connections[0]).sort()).toEqual(
      ["id", "kind", "base_url", "account_id", "account_login", "created_at", "last_verified_at"].sort(),
    );
  });

  it("never stores or returns the token it was given", async () => {
    const { mockApi } = await load();
    const created = await mockApi.createIssueTrackerConnection("youtrack", "https://youtrack.example.com", TOKEN);
    expect(JSON.stringify(created)).not.toContain(TOKEN);
    expect(JSON.stringify(await mockApi.listIssueTrackerConnections())).not.toContain(TOKEN);
    // Reconnecting with the same base URL replaces the token on the same row.
    const { connections } = await mockApi.listIssueTrackerConnections();
    expect(connections).toHaveLength(1);
    expect(created.connection.id).toBe(connections[0].id);
  });

  it.each([
    ["youtrack", "https://other.example.com", TOKEN, 400, "base URL is not in the allowed issue tracker list"],
    ["youtrack", "https://youtrack.example.com", "", 400, "a token is required"],
    ["jira", "https://youtrack.example.com", TOKEN, 400, "unsupported issue tracker kind"],
  ])("refuses %s %s", async (kind, baseUrl, token, status, message) => {
    const { mockApi } = await load();
    const err = await mockApi.createIssueTrackerConnection(kind, baseUrl, token).catch((e: unknown) => e);
    // A fresh module graph has its own ApiError class, so match it by name.
    expect((err as Error).name).toBe("ApiError");
    expect((err as ApiError).status).toBe(status);
    expect((err as ApiError).message).toContain(message);
  });

  it("deletes an unlinked connection and refuses one a repo is linked to", async () => {
    const { mockApi, repos } = await load();
    const [conn] = (await mockApi.listIssueTrackerConnections()).connections;
    repos[0].issue_tracker_link = {
      connection_id: conn.id,
      project_id: "0-4",
      project_short_name: "ATL",
      tag_id: "6-2",
      tag_name: "uzi",
      linked_at: new Date().toISOString(),
    };
    const err = await mockApi.deleteIssueTrackerConnection(conn.id).catch((e: unknown) => e);
    expect((err as ApiError).status).toBe(409);
    expect((err as ApiError).message).toBe(
      "this connection is linked to a repo and cannot be deleted while it is linked; a link cannot be removed",
    );
    repos[0].issue_tracker_link = null;
    await mockApi.deleteIssueTrackerConnection(conn.id);
    expect((await mockApi.listIssueTrackerConnections()).connections).toEqual([]);
    const gone = await mockApi.deleteIssueTrackerConnection(conn.id).catch((e: unknown) => e);
    expect((gone as ApiError).status).toBe(404);
  });

  it("lists projects and tags for a known connection", async () => {
    const { mockApi } = await load();
    const [conn] = (await mockApi.listIssueTrackerConnections()).connections;
    expect((await mockApi.listIssueTrackerProjects(conn.id)).projects.length).toBeGreaterThan(0);
    const { tags } = await mockApi.listIssueTrackerTags(conn.id);
    expect(tags.some((t) => t.account_can_apply && t.others_can_apply)).toBe(true);
    expect(tags.some((t) => !t.account_can_apply)).toBe(true);
  });
});
