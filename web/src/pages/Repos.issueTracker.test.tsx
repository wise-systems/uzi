// @vitest-environment jsdom
// The Repos page's issue tracker surface (PRD #4173 M6): the link control on a
// never-enabled repo, the issue source line on a linked one, and the States panel.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { Repos } from "./Repos";
import { api, ApiError, type ForgeConnection, type IssueTrackerConnection, type Repo } from "../lib/api";
import { useAuth } from "../auth/AuthContext";

vi.mock("../lib/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../lib/api")>();
  return {
    ...actual,
    api: {
      listConnections: vi.fn(),
      listProjects: vi.fn(),
      setRepoEnabled: vi.fn(),
      getRepoToolProfile: vi.fn(),
      listToolAllowlist: vi.fn(),
      listIssueTrackerConnections: vi.fn(),
      listIssueTrackerProjects: vi.fn(),
      listIssueTrackerTags: vi.fn(),
      linkRepoToIssueTracker: vi.fn(),
      getIssueTrackerStates: vi.fn(),
      putIssueTrackerStates: vi.fn(),
      getBoard: vi.fn(),
    },
  };
});

vi.mock("../auth/AuthContext", () => ({ useAuth: vi.fn() }));

const mockApi = vi.mocked(api);

const CONN: ForgeConnection = {
  id: "conn-1",
  forge_type: "gitlab",
  base_url: "https://gitlab.example.com",
  bot_username: "uzi-bot",
  bot_forge_user_id: 1,
  human_username: null,
  created_at: "2026-01-01T00:00:00Z",
  last_verified_at: "2026-07-05T12:00:00Z",
  privilege_status: null,
  privilege_checked_at: null,
  privilege_report: null,
};

const IT_CONN: IssueTrackerConnection = {
  id: "itc-1",
  kind: "youtrack",
  base_url: "https://youtrack.example.com",
  account_id: "1-17",
  account_login: "uzi-bot",
  created_at: "2026-11-01T09:00:00Z",
  last_verified_at: "2026-11-18T12:00:00Z",
};

const LINK = {
  connection_id: "itc-1",
  project_id: "0-4",
  project_short_name: "ATL",
  tag_id: "6-2",
  tag_name: "uzi",
  linked_at: "2026-11-19T10:00:00Z",
};

function repo(over: Partial<Repo> & Pick<Repo, "id" | "path_with_namespace">): Repo {
  return {
    connection_id: "conn-1",
    forge_project_id: 1,
    web_url: `https://gitlab.example.com/${over.path_with_namespace}`,
    default_branch: "main",
    enabled: true,
    repo_skills_enabled: false,
    repo_claudemd_enabled: false,
    repo_devbox_opt_in: false,
    repo_fold_improve_uzi_backlog: false,
    pipeline: null,
    guardrail_blocked: false,
    docker_allowlisted: false,
    docker_blocked: false,
    ...over,
  };
}

const PLAIN = repo({ id: "repo-uzi", path_with_namespace: "acme/uzi" });
const LINKABLE = repo({
  id: "repo-new",
  path_with_namespace: "acme/atlas-web",
  enabled: false,
  issue_tracker_link: null,
  issue_tracker_linkable: true,
});
const LINKED = repo({ id: "repo-linked", path_with_namespace: "acme/atlas-api", issue_tracker_link: LINK });

function rowFor(name: string): HTMLElement {
  // The States panel repeats the path below the table: take the table row's copy.
  const tr = screen
    .getAllByText(name)
    .map((el) => el.closest("tr"))
    .find((el) => el !== null);
  if (!tr) throw new Error(`no row for ${name}`);
  return tr;
}

function renderPage() {
  return render(
    <MemoryRouter>
      <Repos />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  vi.mocked(useAuth).mockReturnValue({ user: { id: "u1", is_admin: true } } as unknown as ReturnType<typeof useAuth>);
  mockApi.listConnections.mockResolvedValue({ connections: [CONN] });
  mockApi.listProjects.mockResolvedValue({ repos: [PLAIN, LINKABLE, LINKED].map((r) => ({ ...r })) });
  mockApi.getRepoToolProfile.mockResolvedValue({ packages: [] });
  mockApi.listToolAllowlist.mockResolvedValue({ allowlist: [] });
  mockApi.listIssueTrackerConnections.mockResolvedValue({ connections: [IT_CONN] });
  mockApi.listIssueTrackerProjects.mockResolvedValue({ projects: [{ id: "0-4", short_name: "ATL", name: "Atlas" }] });
  mockApi.listIssueTrackerTags.mockResolvedValue({
    tags: [
      {
        id: "6-2",
        name: "uzi",
        owner: { id: "1-17", login: "uzi-bot" },
        read: { groups: [{ id: "3-0", name: "All Users", all_users: true, users_count: 0 }], users: [] },
        use: { groups: [{ id: "3-0", name: "All Users", all_users: true, users_count: 0 }], users: [] },
        account_can_apply: true,
        others_can_apply: true,
      },
    ],
  });
  mockApi.getIssueTrackerStates.mockResolvedValue({
    mapping: null,
    states: [
      { name: "Open", resolved: false, archived: false, closable: false, openable: true },
      { name: "Fixed", resolved: true, archived: false, closable: true, openable: false },
    ],
  });
  mockApi.getBoard.mockRejectedValue(new ApiError(409, "the repo has never been enabled; enable it first"));
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("Repos: issue trackers", () => {
  it("shows a linked repo's issue source and offers no unlink or relink", async () => {
    renderPage();
    await screen.findByText("acme/atlas-api");
    const row = rowFor("acme/atlas-api");
    await waitFor(() =>
      expect(within(row).getByTestId("repo-issue-source").textContent).toBe("Issues: YouTrack project ATL, tag uzi"),
    );
    expect(within(row).getByRole("button", { name: "Map issue tracker States for acme/atlas-api" })).toBeTruthy();
    expect(within(row).queryByRole("button", { name: /unlink|relink|link to an issue tracker/i })).toBeNull();
  });

  it("names the issue tracker generically when the connection list cannot be read", async () => {
    mockApi.listIssueTrackerConnections.mockRejectedValue(new ApiError(500, "boom"));
    renderPage();
    await screen.findByText("acme/atlas-api");
    await waitFor(() => expect(mockApi.listIssueTrackerConnections).toHaveBeenCalled());
    expect(within(rowFor("acme/atlas-api")).getByTestId("repo-issue-source").textContent).toBe(
      "Issues: issue tracker project ATL, tag uzi",
    );
  });

  it("leaves a repo that is neither linked nor linkable unchanged", async () => {
    mockApi.listProjects.mockResolvedValue({ repos: [{ ...PLAIN }] });
    renderPage();
    await screen.findByText("acme/uzi");
    const row = rowFor("acme/uzi");
    expect(within(row).queryByTestId("repo-issue-source")).toBeNull();
    expect(row.textContent).not.toMatch(/issue tracker|Issues:|States/);
    expect(within(row).queryByRole("button", { name: /issue tracker|States/ })).toBeNull();
    // Nothing on the page asks for issue tracker data.
    expect(mockApi.listIssueTrackerConnections).not.toHaveBeenCalled();
  });

  it("links a never-enabled repo, updates its row and opens its State mapping", async () => {
    mockApi.linkRepoToIssueTracker.mockResolvedValue({ link: { repo_id: "repo-new", ...LINK } });
    renderPage();
    await screen.findByText("acme/atlas-web");
    const row = rowFor("acme/atlas-web");
    expect(within(row).queryByTestId("repo-issue-source")).toBeNull();
    fireEvent.click(within(row).getByRole("button", { name: "Link to an issue tracker" }));

    const dialog = await screen.findByRole("dialog", { name: "Link acme/atlas-web to an issue tracker" });
    const project = (await within(dialog).findByLabelText("Project")) as HTMLSelectElement;
    await waitFor(() => expect(project.disabled).toBe(false));
    fireEvent.change(project, { target: { value: "0-4" } });
    const tagSelect = within(dialog).getByLabelText("Eligibility tag") as HTMLSelectElement;
    await waitFor(() => expect(tagSelect.disabled).toBe(false));
    fireEvent.change(tagSelect, { target: { value: "6-2" } });
    fireEvent.click(within(dialog).getByRole("checkbox", { name: "They can edit issues in this project" }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Link" }));

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(mockApi.linkRepoToIssueTracker).toHaveBeenCalledWith("repo-new", {
      connection_id: "itc-1",
      project_id: "0-4",
      tag_id: "6-2",
    });
    const linkedRow = rowFor("acme/atlas-web");
    expect(within(linkedRow).getByTestId("repo-issue-source").textContent).toBe("Issues: YouTrack project ATL, tag uzi");
    expect(within(linkedRow).queryByRole("button", { name: "Link to an issue tracker" })).toBeNull();
    // The States panel opens for the new link; a never-enabled repo has no board yet.
    await screen.findByRole("heading", { name: "Issue tracker States" });
    expect(await screen.findByTestId("states-board-note")).toBeTruthy();
    expect(mockApi.getIssueTrackerStates).toHaveBeenCalledWith("repo-new");
  });

  it("opens the States panel from a linked row and returns focus to it on close", async () => {
    renderPage();
    await screen.findByText("acme/atlas-api");
    const trigger = within(rowFor("acme/atlas-api")).getByRole("button", {
      name: "Map issue tracker States for acme/atlas-api",
    });
    fireEvent.click(trigger);
    expect(trigger.getAttribute("aria-expanded")).toBe("true");
    const heading = await screen.findByRole("heading", { name: "Issue tracker States" });
    const panel = heading.closest("section") as HTMLElement;
    await within(panel).findByLabelText("Closed");
    expect(mockApi.getIssueTrackerStates).toHaveBeenCalledWith("repo-linked");
    fireEvent.click(within(panel).getByRole("button", { name: "Close" }));
    expect(screen.queryByRole("heading", { name: "Issue tracker States" })).toBeNull();
    expect(document.activeElement).toBe(trigger);
  });
});
