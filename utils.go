package main

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/google/go-github/v30/github"
)

func checkError(err error) {
	if err != nil {
		log.Fatalf("error: %s\n", err.Error())
		return
	}
}

func countReactions(reactions []*github.Reaction, content string) int {
	var count int

	for _, reaction := range reactions {
		if *reaction.Content == content {
			count++
		}
	}

	return count
}

func validatePR(pr *github.PullRequest) error {
	files, _, err := client.PullRequests.ListFiles(ctx, config.Owner, config.Repo, *pr.Number, nil)
	checkError(err)

	var reasons []string

	meta := false

	for _, file := range files {
		for _, bad := range config.BlacklistedFiles {
			if strings.ToLower(*file.Filename) == strings.ToLower(bad) {
				reasons = append(reasons, fmt.Sprintf("- Changes a blacklisted file: %s", bad))
			}
		}

		for _, good := range config.WhitelistedFileExtensions {
			if strings.HasSuffix(strings.ToLower(*file.Filename), strings.ToLower(good)) {
				meta = true
			}
		}
	}

	if meta {
		return errors.New("meta")
	} else if len(reasons) > 0 {
		return errors.New(strings.Join(reasons, "\n"))
	}

	return nil
}

func verifyIfLabelExists(name string) {
	labels, _, err := client.Issues.ListLabels(ctx, config.Owner, config.Repo, nil)
	checkError(err)

	var l *github.Label

	for _, label := range labels {
		if *label.Name == name {
			l = label
		}
	}

	if l == nil {
		log.Fatalf("You don't have a label named `%s` in your configured repo. Please create one.\n", name)
		return
	}
}

func hasLabel(name string, issue *github.Issue) bool {
	for _, label := range issue.Labels {
		if *label.Name == name {
			return true
		}
	}

	return false
}

func prHasLabel(name string, pull *github.PullRequest) bool {
	for _, label := range pull.Labels {
		if *label.Name == name {
			return true
		}
	}

	return false
}
