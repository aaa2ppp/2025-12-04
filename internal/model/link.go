package model

import "slices"

type Link struct {
	Name       string
	URL        string
	Available  bool
	StatusCode int
	Reason     string
}

func (l Link) Clone() Link {
	return l
}

func CloneLinks(ls []Link) []Link {
	return slices.Clone(ls)
}

type LinkSet struct {
	ID    uint64
	Links []Link
}

func (ls LinkSet) Clone() LinkSet {
	return LinkSet{
		ID:    ls.ID,
		Links: CloneLinks(ls.Links),
	}
}
