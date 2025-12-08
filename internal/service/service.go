package service

import (
	"context"
	"errors"

	"link-checker/internal/api"
	"link-checker/internal/model"
)

type LinkStorage interface {
	Save(ctx context.Context, links []model.Link) (uint64, error)
	Load(ctx context.Context, id uint64) (model.LinkSet, error)
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
	linkSets := make([]model.LinkSet, 0, len(linkSetIDs))

	for _, id := range linkSetIDs {
		linkSet, err := s.storage.Load(ctx, id)
		if err != nil {
			if errors.Is(err, model.ErrNotFound) {
				linkSets = append(linkSets, model.LinkSet{
					ID:    id,
					Links: []model.Link{{Name: "unknown", Reason: err.Error()}}, // TODO: maybe add LinkSet.Err field?
				})
				continue
			}

			return model.Report{}, err
		}
		
		linkSets = append(linkSets, linkSet)
	}

	return s.builder.Build(ctx, linkSets)
}

var (
	_ api.LinkChecker = &Service{}
	_ api.Reporter    = &Service{}
)
