package main

var config struct {
	Token        string `json:"token"`
	Owner        string `json:"owner"`
	Repo         string `json:"repo"`
	VotingPeriod int64  `json:"voting_period"` // (in hours, for now.)
}
