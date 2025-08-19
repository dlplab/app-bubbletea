package main

import (
	"os/exec"
	"strings"
)

// Utility: check git dirty state and branch
func getGitStatus(repoPath string) (branch string, dirty bool, err error) {
	cmd := exec.Command("git", "-C", repoPath, "status", "--porcelain", "--branch")
	out, err := cmd.Output()
	if err != nil {
		return "", false, err
	}
	lines := strings.Split(string(out), "\n")
	branch = "main"
	if len(lines) > 0 && strings.HasPrefix(lines[0], "## ") {
		branchLine := lines[0][3:]
		if idx := strings.Index(branchLine, "..."); idx > 0 {
			branch = branchLine[:idx]
		} else if idx := strings.Index(branchLine, " "); idx > 0 {
			branch = branchLine[:idx]
		} else {
			branch = branchLine
		}
	}
	for _, l := range lines[1:] {
		if len(strings.TrimSpace(l)) > 0 {
			return branch, true, nil
		}
	}
	return branch, false, nil
}
