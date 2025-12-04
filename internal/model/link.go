package model

type Link struct {
	Name      string
	URL       string
	Available bool
	Reason    string
}

type LinkSet struct {
	ID    uint64
	Links []Link
}
