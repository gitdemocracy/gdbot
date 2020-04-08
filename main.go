package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/go-github/v30/github"
	"golang.org/x/oauth2"
)

var (
	config struct {
		Token                     string   `json:"token"`
		Owner                     string   `json:"owner"`
		Repo                      string   `json:"repo"`
		VotingPeriod              int64    `json:"voting_period"` // in hours
		PollInterval              int64    `json:"poll_interval"` // in minutes
		ListenAddress             string   `json:"listen_address"`
		WebhookSecret             string   `json:"webhook_secret"`
		BlacklistedFiles          []string `json:"blacklisted_files"`
		WhitelistedFileExtensions []string `json:"whitelisted_file_extensions"`
		MetaAssignees             []string `json:"meta_assignees"`
	}
	client *github.Client
	ctx    = context.Background()
	opened = "opened"
	closed = "closed"
)

func main() {
	configBytes, err := ioutil.ReadFile("config.json")
	checkError(err)

	err = json.Unmarshal(configBytes, &config)
	checkError(err)

	client = github.NewClient(oauth2.NewClient(ctx, oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: config.Token},
	)))

	verifyIfLabelExists("meta")
	verifyIfLabelExists("pending-reverify")

	go func() {
		for {
			log.Printf("polling github...\n")

			log.Printf("getting pull requests...\n")

			pulls, _, err := client.PullRequests.List(ctx, config.Owner, config.Repo, nil)
			checkError(err)

			for _, pull := range pulls {
				if prHasLabel("meta", pull) {
					continue
				}

				if time.Since(*pull.CreatedAt) >= time.Hour*time.Duration(config.VotingPeriod) {
					reactions, _, err := client.Reactions.ListIssueReactions(ctx, config.Owner, config.Repo, *pull.Number, nil)
					checkError(err)

					yes := countReactions(reactions, "+1") - 1
					no := countReactions(reactions, "-1") - 1

					// TODO: verify counts (look for people who've voted both yes and no, etc.)

					log.Printf("There are %d votes for %s, and %d against.\n", yes, *pull.Title, no)

					if yes > no {
						log.Printf("Merging...\n")

						body := fmt.Sprintf("%d:%d, merging...\n", yes, no)

						_, _, err = client.Issues.CreateComment(ctx, config.Owner, config.Repo, *pull.Number, &github.IssueComment{
							Body: &body,
						})
						checkError(err)

						_, _, err = client.PullRequests.Merge(ctx, config.Owner, config.Repo, *pull.Number, *pull.Title, nil)
						checkError(err)
					} else if yes < no {
						log.Printf("Closing...\n")

						body := fmt.Sprintf("%d:%d, closing...\n", yes, no)

						_, _, err = client.Issues.CreateComment(ctx, config.Owner, config.Repo, *pull.Number, &github.IssueComment{
							Body: &body,
						})
						checkError(err)

						_, _, err = client.PullRequests.Edit(ctx, config.Owner, config.Repo, *pull.Number, &github.PullRequest{
							State: &closed,
						})
						checkError(err)
					} else {
						var body string

						if yes == 0 && no == 0 {
							body = "No votes, closing..."
						} else {
							body = fmt.Sprintf("Tie (%d:%d), closing...", yes, no)
						}

						_, _, err = client.Issues.CreateComment(ctx, config.Owner, config.Repo, *pull.Number, &github.IssueComment{
							Body: &body,
						})
						checkError(err)

						_, _, err = client.PullRequests.Edit(ctx, config.Owner, config.Repo, *pull.Number, &github.PullRequest{
							State: &closed,
						})
						checkError(err)
					}
				}
			}

			time.Sleep(time.Minute * time.Duration(config.PollInterval))
		}
	}()

	http.ListenAndServe(config.ListenAddress, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, err := github.ValidatePayload(r, []byte(config.WebhookSecret))
		if err != nil {
			log.Printf("signature verification failed!\n")
			return
		}

		event, err := github.ParseWebHook(github.WebHookType(r), payload)
		checkError(err)

		switch event := event.(type) {
		case *github.PullRequestEvent:
			if *event.Action == "opened" {
				if strings.HasPrefix(strings.ToLower(*event.PullRequest.Title), "meta") {
					_, _, err = client.Issues.AddLabelsToIssue(ctx, config.Owner, config.Repo, *event.PullRequest.Number, []string{"meta"})
					checkError(err)

					_, _, err = client.Issues.AddAssignees(ctx, config.Owner, config.Repo, *event.PullRequest.Number, config.MetaAssignees)
					checkError(err)
				} else {
					err = validatePR(event.PullRequest)

					if err == nil {
						_, _, err = client.Reactions.CreateIssueReaction(ctx, config.Owner, config.Repo, *event.PullRequest.Number, "+1")
						checkError(err)

						_, _, err = client.Reactions.CreateIssueReaction(ctx, config.Owner, config.Repo, *event.PullRequest.Number, "-1")
						checkError(err)

						body := fmt.Sprintf("This issue will be in voting until (roughly) ``%s``.", time.Now().Add(time.Hour*time.Duration(config.VotingPeriod)).Format(time.RFC1123))

						_, _, err = client.Issues.CreateComment(ctx, config.Owner, config.Repo, *event.PullRequest.Number, &github.IssueComment{
							Body: &body,
						})
						checkError(err)
					} else {
						if err.Error() == "meta" {
							_, _, err = client.Issues.AddLabelsToIssue(ctx, config.Owner, config.Repo, *event.PullRequest.Number, []string{"meta"})
							checkError(err)

							_, _, err = client.Issues.AddAssignees(ctx, config.Owner, config.Repo, *event.PullRequest.Number, config.MetaAssignees)
							checkError(err)
						} else {
							body := fmt.Sprintf("Hello!\n\nYour PR has failed verification for the following reasons:\n```\n%s\n```\nDon't worry though, if you fix the issue(s), you can make me reverify your PR by commenting ``reverify``.", err)

							_, _, err = client.Issues.CreateComment(ctx, config.Owner, config.Repo, *event.PullRequest.Number, &github.IssueComment{
								Body: &body,
							})
							checkError(err)

							_, _, err = client.Issues.AddLabelsToIssue(ctx, config.Owner, config.Repo, *event.PullRequest.Number, []string{"pending-reverify"})
							checkError(err)
						}
					}
				}
			}
		case *github.IssueCommentEvent:
			if hasLabel("pending-reverify", event.Issue) && *event.Comment.User.ID == *event.Issue.User.ID && strings.ToLower(*event.Comment.Body) == "reverify" {
				pull, _, err := client.PullRequests.Get(ctx, config.Owner, config.Repo, *event.Issue.Number)
				checkError(err)

				err = validatePR(pull)

				if err == nil {
					_, err = client.Issues.RemoveLabelForIssue(ctx, config.Owner, config.Repo, *pull.Number, "pending-reverify")
					checkError(err)

					_, _, err = client.Reactions.CreateIssueReaction(ctx, config.Owner, config.Repo, *pull.Number, "+1")
					checkError(err)

					_, _, err = client.Reactions.CreateIssueReaction(ctx, config.Owner, config.Repo, *pull.Number, "-1")
					checkError(err)

					body := fmt.Sprintf("This issue will be in voting until (roughly) ``%s``.", time.Now().Add(time.Hour*time.Duration(config.VotingPeriod)).Format(time.RFC1123))

					_, _, err = client.Issues.CreateComment(ctx, config.Owner, config.Repo, *pull.Number, &github.IssueComment{
						Body: &body,
					})
					checkError(err)
				} else {
					body := fmt.Sprintf("Hello!\n\nYour PR has failed reverification for the following reasons:\n```\n%s\n```\nDue to the fact that you've already opened a PR with issue(s), and issue(s) are still present, I have closed and locked this PR. Feel free to open another, though!", err)

					_, _, err = client.Issues.CreateComment(ctx, config.Owner, config.Repo, *pull.Number, &github.IssueComment{
						Body: &body,
					})
					checkError(err)

					_, err = client.Issues.RemoveLabelForIssue(ctx, config.Owner, config.Repo, *pull.Number, "pending-reverify")
					checkError(err)

					_, _, err = client.PullRequests.Edit(ctx, config.Owner, config.Repo, *pull.Number, &github.PullRequest{
						State: &closed,
					})
					checkError(err)

					_, err = client.Issues.Lock(ctx, config.Owner, config.Repo, *pull.Number, nil)
					checkError(err)
				}
			}
		}
	}))
}
