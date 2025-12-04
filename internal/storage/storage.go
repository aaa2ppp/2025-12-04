package storage

import (
	"context"

	"link-checker/internal/model"
	"link-checker/internal/service"
)

type Config struct {
	// TODO
}

type Storage struct {
	// TODO
}

func (s *Storage) Load(ctx context.Context, id uint64) (model.LinkSet, error) {
	panic("unimplemented")
}

func (s *Storage) Save(ctx context.Context, links []model.Link) (uint64, error) {
	panic("unimplemented")
}

func Open(cfg Config) (*Storage, error) {
	// TODO
	return &Storage{}, nil
}

func (s *Storage) Close() error {
	// TODO
	return nil
}

var _ service.LinkStorage = &Storage{}
