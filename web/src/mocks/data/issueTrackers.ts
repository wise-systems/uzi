import type {
  IssueTrackerConfig,
  IssueTrackerConnection,
  IssueTrackerProject,
  IssueTrackerState,
  IssueTrackerTag,
  Repo,
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

// One tag a link can use (the account can apply it and so can everyone), one a link
// must refuse because the account cannot apply it (another user owns it and shares it
// with nobody), and one it must refuse because nobody else can (the account owns it,
// and its only group, a named one with one member, does not count).
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
  {
    id: "6-12",
    name: "bot-only",
    owner: { id: "1-17", login: "uzi-bot" },
    read: { groups: [{ id: "3-4", name: "Release crew", all_users: false, users_count: 1 }], users: [] },
    use: { groups: [{ id: "3-4", name: "Release crew", all_users: false, users_count: 1 }], users: [] },
    account_can_apply: true,
    others_can_apply: false,
  },
];

// The never-enabled repo the default mock offers "Link to an issue tracker" on. It is
// listed by the default forge connection's project list (as ListProjects lists a
// never-enabled repo) and, until it is linked or enabled, is linkable.
export const mockLinkableRepo: Repo = {
  id: "repo-atlas-web",
  connection_id: "conn-1",
  forge_project_id: 702,
  path_with_namespace: "vtmocanu/atlas-web",
  web_url: "https://gitlab.example.com/vtmocanu/atlas-web",
  default_branch: "main",
  enabled: false,
  repo_skills_enabled: false,
  repo_claudemd_enabled: false,
  repo_devbox_opt_in: false,
  repo_fold_improve_uzi_backlog: false,
  pipeline: null,
  guardrail_override: null,
  guardrail_blocked: false,
  docker_allowlisted: false,
  docker_blocked: false,
  issue_tracker_link: null,
  issue_tracker_linkable: true,
};

// The board columns a repo gets in mock mode once it is linked.
export const mockLinkedBoardColumns = ["Ready", "In progress", "Human Review"];

// The linked project's States. Only Fixed means done, so only it may be the closed
// State; Won't fix and Duplicate are resolved but never closable. Four unresolved
// States, so the open State and three columns can each take a different one.
export const mockIssueTrackerStates: IssueTrackerState[] = [
  { name: "Submitted", resolved: false, archived: false, closable: false, openable: true },
  { name: "Open", resolved: false, archived: false, closable: false, openable: true },
  { name: "In Progress", resolved: false, archived: false, closable: false, openable: true },
  { name: "Review", resolved: false, archived: false, closable: false, openable: true },
  { name: "Fixed", resolved: true, archived: false, closable: true, openable: false },
  { name: "Won't fix", resolved: true, archived: false, closable: false, openable: false },
  { name: "Duplicate", resolved: true, archived: false, closable: false, openable: false },
];
