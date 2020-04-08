package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"time"

	"github.com/google/go-github/v30/github"
	"golang.org/x/oauth2"
)

var (
	client *github.Client
	ctx    = context.Background()
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

	// for {
	log.Printf("polling github...\n")

	log.Printf("getting pull requests...\n")

	pulls, _, err := client.PullRequests.List(ctx, config.Owner, config.Repo, nil)
	checkError(err)

	for _, pull := range pulls {
		if time.Since(*pull.CreatedAt) >= time.Hour*time.Duration(config.VotingPeriod) {
			reactions, _, err := client.Reactions.ListIssueReactions(ctx, config.Owner, config.Repo, *pull.Number, nil)
			checkError(err)

			yes := countReactions(reactions, "+1")
			no := countReactions(reactions, "-1")

			// TODO: verify counts (look for people who've voted both yes and no, etc.)

			log.Printf("There are %d votes for %s, and %d against.\n", yes, *pull.Title, no)

			if yes > no {
				log.Printf("Merging...\n")

				body := fmt.Sprintf("%d:%d, merging...\n", yes, no)

				_, _, err = client.Issues.CreateComment(ctx, config.Owner, config.Repo, *pull.Number, &github.IssueComment{
					Body: &body,
				})
				checkError(err)

				_, _, err = client.PullRequests.Merge(ctx, config.Owner, config.Repo, *pull.Number, fmt.Sprintf(`Merge "%s"`, *pull.Title), nil)
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
				// tie
			}
		}
	}
	// }
}
