package server

import (
	"context"
	"net/http"
)

type Config struct {
	Addr string
	// TODO
}

type Server struct {
	server *http.Server
	// TODO
}

func New(cfg Config, handler http.Handler) Server {
	// TODO
	server := &http.Server{
		Addr:    cfg.Addr,
		Handler: handler,
	}
	return Server{
		server: server,
	}
}

func (s *Server) Shutdown(ctx context.Context) error {
	// TODO
	return s.server.Shutdown(ctx)
}

func (s *Server) ListenAndServe() error {
	// TODO
	return s.server.ListenAndServe()
}
