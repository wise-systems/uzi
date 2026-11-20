package issuetracker

import "strconv"

// IssueKey is the human issue key of issue number under a tracker project short name, the
// short name, a dash and the number ("ABC-12"). It is the one place the key is assembled:
// the claim assembly, the API payloads, the YouTrack driver and its fake all use it.
func IssueKey(projectShort string, number int64) string {
	return projectShort + "-" + strconv.FormatInt(number, 10)
}
