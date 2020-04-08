package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/google/go-github/v30/github"
	"golang.org/x/oauth2"
)

var (
	config struct {
		Token         string `json:"token"`
		Owner         string `json:"owner"`
		Repo          string `json:"repo"`
		VotingPeriod  int64  `json:"voting_period"` // in hours
		PollInterval  int64  `json:"poll_interval"` // in minutes
		ListenAddress string `json:"listen_address"`
		WebhookSecret string `json:"webhook_secret"`
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

	go func() {
		for {
			log.Printf("polling github...\n")

			log.Printf("getting pull requests...\n")

			pulls, _, err := client.PullRequests.List(ctx, config.Owner, config.Repo, nil)
			checkError(err)

			for _, pull := range pulls {
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
				_, _, err = client.Reactions.CreateIssueReaction(ctx, config.Owner, config.Repo, *event.PullRequest.Number, "+1")
				checkError(err)

				_, _, err = client.Reactions.CreateIssueReaction(ctx, config.Owner, config.Repo, *event.PullRequest.Number, "-1")
				checkError(err)

				body := fmt.Sprintf("This issue will be in voting until (roughly) ``%s``.", time.Now().Format(time.RFC1123))

				_, _, err = client.Issues.CreateComment(ctx, config.Owner, config.Repo, *event.PullRequest.Number, &github.IssueComment{
					Body: &body,
				})
				checkError(err)
			}
		}
	}))
}
