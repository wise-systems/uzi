import type {
  IssueTrackerConfig,
  IssueTrackerConnection,
  IssueTrackerProject,
  IssueTrackerTag,
} from "../../lib/api";
import { daysAgo, minsAgo } from "./time";

// ── Issue trackers (PRD #4173 M6) ─────────────────────────────────────────────
// Seed data for Settings → Issue trackers. No token anywhere: a connection has no
// token field, and the create mock never stores the one it is given.

export const mockIssueTrackerConfig: IssueTrackerConfig = {
  allowed_base_urls: ["https://youtrack.example.com"],
  kinds: ["youtrack"],
};

export const mockIssueTrackerConnection: IssueTrackerConnection = {
  id: "itc-1",
  kind: "youtrack",
  base_url: "https://youtrack.example.com",
  account_id: "1-17",
  account_login: "uzi-bot",
  created_at: daysAgo(12),
  last_verified_at: minsAgo(35),
};

export const mockIssueTrackerProjects: IssueTrackerProject[] = [
  { id: "0-4", short_name: "ATL", name: "Atlas" },
  { id: "0-7", short_name: "OPS", name: "Operations" },
];

// One tag a link can use (the account can apply it and so can everyone), and one a
// link must refuse (another user owns it and shares it with nobody else).
export const mockIssueTrackerTags: IssueTrackerTag[] = [
  {
    id: "6-2",
    name: "uzi",
    owner: { id: "1-17", login: "uzi-bot" },
    read: { groups: [{ id: "3-0", name: "All Users", all_users: true, users_count: 0 }], users: [] },
    use: { groups: [{ id: "3-0", name: "All Users", all_users: true, users_count: 0 }], users: [] },
    account_can_apply: true,
    others_can_apply: true,
  },
  {
    id: "6-9",
    name: "triage",
    owner: { id: "1-3", login: "dana" },
    read: { groups: [], users: [{ id: "1-17", login: "uzi-bot" }] },
    use: { groups: [], users: [] },
    account_can_apply: false,
    others_can_apply: false,
  },
];
