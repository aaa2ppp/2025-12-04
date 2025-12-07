package service

import (
	"context"

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

func (s *Service) CheckLinks(ctx context.Context, links []string) (model.LinkSet, error) {
	// TODO
	return model.LinkSet{}, nil
}

func (s *Service) Report(ctx context.Context, linkSetIDs []uint64) (model.Report, error) {
	// TODO
	return model.Report{
		ContentType: "plain/text",
		Body:        []byte("unimplemented"),
	}, nil
}

var (
	_ api.LinkChecker = &Service{}
	_ api.Reporter    = &Service{}
)
