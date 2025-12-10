package service

import (
	"context"

	"link-checker/internal/api"
	"link-checker/internal/model"
)

type LinkSetBatch struct {
	LinkSets []model.LinkSet
	Found    []bool
}

type LinkStorage interface {
	Save(ctx context.Context, links []model.Link) (uint64, error)
	Load(ctx context.Context, id uint64) (model.LinkSet, error)
	LoadBatch(ctx context.Context, ids []uint64) (LinkSetBatch, error)
}

type ReportBuilder interface {
	Build(ctx context.Context, linkSets []model.LinkSet) (model.Report, error)
}

type URLChecker interface {
	Check(ctx context.Context, links []string) ([]model.Link, error)
}

type Service struct {
	storage LinkStorage
	checker URLChecker
	builder ReportBuilder
}

func New(storage LinkStorage, checker URLChecker, builder ReportBuilder) *Service {
	return &Service{
		storage: storage,
		checker: checker,
		builder: builder,
	}
}

func (s *Service) CheckLinks(ctx context.Context, rawLinks []string) (model.LinkSet, error) {
	links, err := s.checker.Check(ctx, rawLinks)
	if err != nil {
		return model.LinkSet{}, err
	}

	id, err := s.storage.Save(ctx, links)
	if err != nil {
		return model.LinkSet{}, err
	}

	return model.LinkSet{
		ID:    id,
		Links: links,
	}, nil
}

func (s *Service) Report(ctx context.Context, linkSetIDs []uint64) (model.Report, error) {
	batch, err := s.storage.LoadBatch(ctx, linkSetIDs)
	if err != nil {
		return model.Report{}, err
	}

	linkSets, found := batch.LinkSets, batch.Found

	for i, id := range linkSetIDs {
		if !found[i] {
			linkSets[i].ID = id
			// XXX чтобы в отчете хотя бы что-то говорилось о ненайденом linkset
			linkSets[i].Links = []model.Link{{Name: "linkset not found"}}
		}
	}

	return s.builder.Build(ctx, linkSets)
}

var (
	_ api.LinkChecker = &Service{}
	_ api.Reporter    = &Service{}
)
