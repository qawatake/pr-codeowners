package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/sourcegraph/conc"
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

	type result struct {
		content string
		path    string
	}

	resultCh := make(chan result, len(paths))

	var wg conc.WaitGroup
	for _, path := range paths {
		path := path
		wg.Go(func() {
			content, err := fetchFileContent(ctx, info, path)
			if err == nil && content != "" {
				resultCh <- result{content: content, path: path}
			}
		})
	}

	// Wait in a separate goroutine and close channel when done
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Return the first successful result
	for r := range resultCh {
		return r.content, nil
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

// FetchPRData fetches PR info, files, and CODEOWNERS all in parallel
func FetchPRData(ctx context.Context, prRef string) (*PRInfo, []string, string, error) {
	var info *PRInfo
	var infoErr error

	var files []string
	var filesErr error

	var codeowners string
	var codeownersErr error

	// All 3 requests in parallel
	var wg conc.WaitGroup

	// 1. PR info
	wg.Go(func() {
		info, infoErr = GetPRInfo(ctx, prRef)
	})

	// 2. Changed files
	wg.Go(func() {
		files, filesErr = GetPRFiles(ctx, prRef)
	})

	// 3. CODEOWNERS - get repo info from PR URL directly
	wg.Go(func() {
		codeowners, codeownersErr = FetchCodeownersFromPR(ctx, prRef)
	})

	wg.Wait()

	if infoErr != nil {
		return nil, nil, "", infoErr
	}
	if filesErr != nil {
		return nil, nil, "", filesErr
	}
	if codeownersErr != nil {
		return nil, nil, "", codeownersErr
	}

	return info, files, codeowners, nil
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
