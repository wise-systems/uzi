// Issue tracker connections in mock mode (PRD #4173 M6): Settings → Issue trackers.
// Mirrors the server's refusals closely enough to drive the screen: the allowlist,
// a missing token, an unsupported kind, delete while a repo is linked, and an unknown
// id. The token argument is checked for presence and then dropped: nothing here
// stores, returns or logs it.

import type { IssueTrackerConnection } from "../../lib/api";
import { ApiError } from "../../lib/apiError";
import {
  mockIssueTrackerConfig,
  mockIssueTrackerConnection,
  mockIssueTrackerProjects,
  mockIssueTrackerTags,
} from "../data";
import { repos } from "./forge";
import { delay, requireSession } from "./shared";

let connections: IssueTrackerConnection[] = [{ ...mockIssueTrackerConnection }];

function findConnection(id: string): IssueTrackerConnection {
  const c = connections.find((x) => x.id === id);
  if (!c) throw new ApiError(404, "connection not found");
  return c;
}

export const issueTrackersApi = {
  issueTrackerConfig: async () => {
    requireSession();
    return delay({
      allowed_base_urls: [...mockIssueTrackerConfig.allowed_base_urls],
      kinds: [...mockIssueTrackerConfig.kinds],
    });
  },
  listIssueTrackerConnections: async () => {
    requireSession();
    return delay({ connections: connections.map((c) => ({ ...c })) });
  },
  createIssueTrackerConnection: async (kind: string, baseUrl: string, token: string) => {
    requireSession();
    if (!mockIssueTrackerConfig.kinds.includes(kind)) {
      throw new ApiError(400, `unsupported issue tracker kind ${JSON.stringify(kind)}`);
    }
    if (token.trim() === "") throw new ApiError(400, "a token is required");
    if (!mockIssueTrackerConfig.allowed_base_urls.includes(baseUrl)) {
      throw new ApiError(400, "base URL is not in the allowed issue tracker list");
    }
    const now = new Date().toISOString();
    // Reconnecting with the same base URL replaces the token on the existing row.
    const existing = connections.find((c) => c.kind === kind && c.base_url === baseUrl);
    if (existing) {
      existing.last_verified_at = now;
      return delay({ connection: { ...existing } }, 500);
    }
    const conn: IssueTrackerConnection = {
      ...mockIssueTrackerConnection,
      id: `itc-${Date.now()}`,
      kind,
      base_url: baseUrl,
      created_at: now,
      last_verified_at: now,
    };
    connections = [...connections, conn];
    return delay({ connection: { ...conn } }, 500);
  },
  deleteIssueTrackerConnection: async (id: string) => {
    requireSession();
    findConnection(id);
    if (repos.some((r) => r.issue_tracker_link?.connection_id === id)) {
      throw new ApiError(
        409,
        "this connection is linked to a repo and cannot be deleted while it is linked; a link cannot be removed",
      );
    }
    connections = connections.filter((c) => c.id !== id);
    return delay(null);
  },
  listIssueTrackerProjects: async (id: string) => {
    requireSession();
    findConnection(id);
    return delay({ projects: mockIssueTrackerProjects.map((p) => ({ ...p })) }, 350);
  },
  listIssueTrackerTags: async (id: string) => {
    requireSession();
    findConnection(id);
    return delay({ tags: structuredClone(mockIssueTrackerTags) }, 350);
  },
};
