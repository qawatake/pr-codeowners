package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/sourcegraph/conc/pool"
)

// PRInfo contains PR metadata
type PRInfo struct {
	Owner    string
	Repo     string
	BaseBranch string
}

// GetPRInfo fetches PR information using gh CLI
func GetPRInfo(ctx context.Context, prRef string) (*PRInfo, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prRef,
		"--json", "headRepository,headRepositoryOwner,baseRefName",
		"--jq", `"\(.headRepositoryOwner.login)/\(.headRepository.name)/\(.baseRefName)"`)

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get PR info: %w", err)
	}

	result := strings.Trim(string(out), "\"\n")
	parts := strings.Split(result, "/")
	if len(parts) < 3 {
		return nil, fmt.Errorf("unexpected PR info format: %s", result)
	}

	return &PRInfo{
		Owner:    parts[0],
		Repo:     parts[1],
		BaseBranch: strings.Join(parts[2:], "/"),
	}, nil
}

// GetPRFiles fetches the list of changed files in a PR
func GetPRFiles(ctx context.Context, prRef string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "diff", prRef, "--name-only")

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get PR files: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var files []string
	for _, line := range lines {
		if line != "" {
			files = append(files, line)
		}
	}

	return files, nil
}

// FetchCodeowners fetches CODEOWNERS content from the repository
func FetchCodeowners(ctx context.Context, info *PRInfo) (string, error) {
	paths := []string{".github/CODEOWNERS", "CODEOWNERS", "docs/CODEOWNERS"}

	p := pool.NewWithResults[string]().WithErrors()
	for _, path := range paths {
		p.Go(func() (string, error) {
			return fetchFileContent(ctx, info, path)
		})
	}

	results, _ := p.Wait()

	// Return the first non-empty result
	for _, content := range results {
		if content != "" {
			return content, nil
		}
	}

	return "", fmt.Errorf("CODEOWNERS file not found in %s/%s", info.Owner, info.Repo)
}

// fetchFileContent fetches a single file's content from GitHub
func fetchFileContent(ctx context.Context, info *PRInfo, path string) (string, error) {
	endpoint := fmt.Sprintf("repos/%s/%s/contents/%s?ref=%s", info.Owner, info.Repo, path, info.BaseBranch)

	cmd := exec.CommandContext(ctx, "gh", "api", endpoint, "--jq", ".content")

	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	// Decode base64 content
	encoded := strings.TrimSpace(string(out))
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		// Try with newlines removed
		encoded = strings.ReplaceAll(encoded, "\n", "")
		decoded, err = base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return "", fmt.Errorf("failed to decode base64: %w", err)
		}
	}

	return string(decoded), nil
}

// prData holds all fetched PR data
type prData struct {
	Info      *PRInfo
	Files     []string
	Codeowners string
	Reviewers []string
}

// FetchPRDataWithReviewers fetches PR info, files, CODEOWNERS, and reviewers all in parallel
func FetchPRDataWithReviewers(ctx context.Context, prRef string) (*PRInfo, []string, string, []string, error) {
	p := pool.New().WithErrors()

	var data prData

	p.Go(func() error {
		info, err := GetPRInfo(ctx, prRef)
		data.Info = info
		return err
	})

	p.Go(func() error {
		files, err := GetPRFiles(ctx, prRef)
		data.Files = files
		return err
	})

	p.Go(func() error {
		codeowners, err := FetchCodeownersFromPR(ctx, prRef)
		data.Codeowners = codeowners
		return err
	})

	p.Go(func() error {
		reviewers, err := GetPRReviewers(ctx, prRef)
		data.Reviewers = reviewers
		return err
	})

	if err := p.Wait(); err != nil {
		return nil, nil, "", nil, err
	}

	return data.Info, data.Files, data.Codeowners, data.Reviewers, nil
}

// FetchCodeownersFromPR fetches CODEOWNERS using PR reference directly
func FetchCodeownersFromPR(ctx context.Context, prRef string) (string, error) {
	// Get repo info from PR
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prRef,
		"--json", "headRepository,headRepositoryOwner,baseRefName",
		"--jq", `"\(.headRepositoryOwner.login)/\(.headRepository.name)/\(.baseRefName)"`)

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get PR info for CODEOWNERS: %w", err)
	}

	result := strings.Trim(string(out), "\"\n")
	parts := strings.Split(result, "/")
	if len(parts) < 3 {
		return "", fmt.Errorf("unexpected PR info format: %s", result)
	}

	info := &PRInfo{
		Owner:      parts[0],
		Repo:       parts[1],
		BaseBranch: strings.Join(parts[2:], "/"),
	}

	return FetchCodeowners(ctx, info)
}

// BuildOwnerFilesMap builds a map from owner to files
func BuildOwnerFilesMap(matcher *Matcher, files []string) map[string][]string {
	result := make(map[string][]string)

	for _, file := range files {
		owners := matcher.Match(file)
		if len(owners) == 0 {
			result["(no owner)"] = append(result["(no owner)"], file)
		} else {
			for _, owner := range owners {
				result[owner] = append(result[owner], file)
			}
		}
	}

	return result
}

// ToJSON converts the result map to JSON
func ToJSON(data map[string][]string) (string, error) {
	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// GetPRReviewers fetches the list of reviewers for a PR (both requested and those who approved)
func GetPRReviewers(ctx context.Context, prRef string) ([]string, error) {
	// Get reviewRequests (pending) and reviews with APPROVED state
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prRef,
		"--json", "reviewRequests,reviews",
		"--jq", "([.reviewRequests[].login] + [.reviews[] | select(.state == \"APPROVED\") | .author.login]) | unique | .[]")

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get PR reviewers: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var reviewers []string
	for _, line := range lines {
		if line != "" {
			reviewers = append(reviewers, line)
		}
	}

	return reviewers, nil
}

// GetTeamMembers fetches the members of a GitHub team
func GetTeamMembers(ctx context.Context, owner, teamSlug string) ([]string, error) {
	endpoint := fmt.Sprintf("orgs/%s/teams/%s/members", owner, teamSlug)
	cmd := exec.CommandContext(ctx, "gh", "api", endpoint, "--jq", ".[].login")

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get team members for %s: %w", teamSlug, err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var members []string
	for _, line := range lines {
		if line != "" {
			members = append(members, line)
		}
	}

	return members, nil
}

// OwnerInfo represents an owner with their files and assigned reviewers
type OwnerInfo struct {
	Files     []string `json:"files"`
	Reviewers []string `json:"reviewers,omitempty"`
}

// teamMemberResult holds the result of fetching team members
type teamMemberResult struct {
	Owner   string
	Members []string
}

// BuildOwnerFilesMapWithReviewers builds a map from owner to files and matching reviewers
func BuildOwnerFilesMapWithReviewers(ctx context.Context, matcher *Matcher, files []string, reviewers []string, repoOwner string) map[string]*OwnerInfo {
	// First, build the basic owner -> files map
	ownerFiles := make(map[string][]string)

	for _, file := range files {
		owners := matcher.Match(file)
		if len(owners) == 0 {
			ownerFiles["(no owner)"] = append(ownerFiles["(no owner)"], file)
		} else {
			for _, owner := range owners {
				ownerFiles[owner] = append(ownerFiles[owner], file)
			}
		}
	}

	// Collect team owners
	var teamOwners []string
	for owner := range ownerFiles {
		if strings.HasPrefix(owner, "@") && strings.Contains(owner, "/") {
			teamOwners = append(teamOwners, owner)
		}
	}

	// Get team members for each team owner in parallel
	p := pool.NewWithResults[teamMemberResult]()
	for _, owner := range teamOwners {
		p.Go(func() teamMemberResult {
			parts := strings.SplitN(strings.TrimPrefix(owner, "@"), "/", 2)
			if len(parts) == 2 {
				members, err := GetTeamMembers(ctx, parts[0], parts[1])
				if err == nil {
					return teamMemberResult{Owner: owner, Members: members}
				}
			}
			return teamMemberResult{Owner: owner}
		})
	}

	// Build team members map from results
	teamMembers := make(map[string][]string)
	for _, r := range p.Wait() {
		if len(r.Members) > 0 {
			teamMembers[r.Owner] = r.Members
		}
	}

	// Build the result with reviewers
	result := make(map[string]*OwnerInfo)
	reviewerSet := make(map[string]bool)
	for _, r := range reviewers {
		reviewerSet[r] = true
	}

	for owner, ownerFileList := range ownerFiles {
		info := &OwnerInfo{
			Files: ownerFileList,
		}

		// Find reviewers that belong to this team
		if members, ok := teamMembers[owner]; ok {
			var matchingReviewers []string
			for _, member := range members {
				if reviewerSet[member] {
					matchingReviewers = append(matchingReviewers, member)
				}
			}
			if len(matchingReviewers) > 0 {
				info.Reviewers = matchingReviewers
			}
		} else if strings.HasPrefix(owner, "@") && !strings.Contains(owner, "/") {
			// Individual user owner
			username := strings.TrimPrefix(owner, "@")
			if reviewerSet[username] {
				info.Reviewers = []string{username}
			}
		}

		result[owner] = info
	}

	return result
}

// ToJSONWithReviewers converts the result map to JSON
func ToJSONWithReviewers(data map[string]*OwnerInfo) (string, error) {
	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}
