package model

type Link struct {
	Name       string
	URL        string
	Available  bool
	StatusCode int
	Reason     string
}

type LinkSet struct {
	ID    uint64
	Links []Link
}
