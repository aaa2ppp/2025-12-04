package checker

import (
	"context"

	"link-checker/internal/model"
	"link-checker/internal/service"
)

type Config struct {
	// TODO
}

type Checker struct {
	// TODO
}

func New(cfg Config) *Checker {
	return &Checker{}
}

func (c *Checker) Check(ctx context.Context, links []string) ([]model.Link, error) {
	// TODO
	return nil, nil
}

var _ service.URLChecker = &Checker{}
