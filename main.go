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
)

func main() {
	configBytes, err := ioutil.ReadFile("config.json")
	checkError(err)

	err = json.Unmarshal(configBytes, &config)
	checkError(err)

	client = github.NewClient(oauth2.NewClient(ctx, oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: config.Token},
	)))

	verifyIfLabelExists("manual review required for merge")
	verifyIfLabelExists("pending-reverify")

	go func() {
		for {
			log.Printf("polling github...\n")

			log.Printf("getting pull requests...\n")

			pulls, _, err := client.PullRequests.List(ctx, config.Owner, config.Repo, nil)
			checkError(err)

			for _, pull := range pulls {
				if prHasLabel("manual review required for merge", pull) || prHasLabel("pending-reverify", pull) {
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

						createComment(*pull.Number, fmt.Sprintf("%d:%d, merging...\n", yes, no))

						_, _, err = client.PullRequests.Merge(ctx, config.Owner, config.Repo, *pull.Number, *pull.Title, nil)
						checkError(err)
					} else if yes < no {
						log.Printf("Closing...\n")

						createComment(*pull.Number, fmt.Sprintf("%d:%d, closing...\n", yes, no))

						setPRState(*pull.Number, "closed")
					} else {
						var body string

						if yes == 0 && no == 0 {
							body = "No votes, closing..."
						} else {
							body = fmt.Sprintf("Tie (%d:%d), closing...", yes, no)
						}

						createComment(*pull.Number, body)

						setPRState(*pull.Number, "closed")
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
					addLabels(*event.PullRequest.Number, "manual review required for merge")
					addAssignees(*event.PullRequest.Number, config.MetaAssignees...)
				} else {
					err = validatePR(event.PullRequest)

					if err == nil {
						addVoteReactions(*event.PullRequest.Number)
						createComment(*event.PullRequest.Number, fmt.Sprintf("This issue will be in voting until (roughly) ``%s``.", time.Now().Add(time.Hour*time.Duration(config.VotingPeriod)).Format(time.RFC1123)))
					} else {
						if err.Error() == "meta" {
							addLabels(*event.PullRequest.Number, "manual review required for merge")
							addAssignees(*event.PullRequest.Number, config.MetaAssignees...)
						} else {
							createComment(*event.PullRequest.Number, fmt.Sprintf("Hello!\n\nYour PR has failed verification for the following reasons:\n```\n%s\n```\nDon't worry though, if you fix the issue(s), you can make me reverify your PR by commenting ``reverify``.", err))
							addLabels(*event.PullRequest.Number, "pending-reverify")
						}
					}
				}
			} else if *event.Action == "synchronize" {
				_, err := client.Issues.RemoveLabelsForIssue(ctx, config.Owner, config.Repo, *event.PullRequest.Number)
				checkError(err)

				addLabels(*event.PullRequest.Number, "manual review required for merge")
			}
		case *github.IssueCommentEvent:
			if hasLabel("pending-reverify", event.Issue) && *event.Comment.User.ID == *event.Issue.User.ID && strings.ToLower(*event.Comment.Body) == "reverify" {
				pull, _, err := client.PullRequests.Get(ctx, config.Owner, config.Repo, *event.Issue.Number)
				checkError(err)

				err = validatePR(pull)

				if err == nil {
					removeLabels(*pull.Number, "pending-reverify")
					addVoteReactions(*pull.Number)

					createComment(*pull.Number, fmt.Sprintf("This issue will be in voting until (roughly) ``%s``.", time.Now().Add(time.Hour*time.Duration(config.VotingPeriod)).Format(time.RFC1123)))
				} else {
					createComment(*pull.Number, fmt.Sprintf("Hello!\n\nYour PR has failed reverification for the following reasons:\n```\n%s\n```\nDue to the fact that you've already opened a PR with issue(s), and issue(s) are still present, I have closed and locked this PR. Feel free to open another, though!", err))

					removeLabels(*pull.Number, "pending-reverify")

					setPRState(*pull.Number, "closed")

					_, err = client.Issues.Lock(ctx, config.Owner, config.Repo, *pull.Number, nil)
					checkError(err)
				}
			}
		}
	}))
}
