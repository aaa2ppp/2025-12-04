package model

import "slices"

type Link struct {
	Name       string `json:"name,omitempty"`
	URL        string `json:"url,omitempty"`
	Available  bool   `json:"available,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

func (l Link) Clone() Link {
	return l
}

func CloneLinks(ls []Link) []Link {
	return slices.Clone(ls)
}

type LinkSet struct {
	ID    uint64 `json:"id,omitempty"`
	Links []Link `json:"links,omitempty"`
}

func (ls LinkSet) Clone() LinkSet {
	return LinkSet{
		ID:    ls.ID,
		Links: CloneLinks(ls.Links),
	}
}
