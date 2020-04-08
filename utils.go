package main

import (
	"log"

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
