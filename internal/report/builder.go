package report

import (
	"context"
	"link-checker/internal/model"
	"link-checker/internal/service"
)

type Config struct {
	// TODO
}

type Builder struct {
	// TODO
}

func (b *Builder) Build(ctx context.Context, linkSets []model.LinkSet) model.Report {
	panic("unimplemented")
}

func NewBuilder(cfg Config) *Builder {
	return &Builder{}
}

var _ service.ReportBuilder = &Builder{}
