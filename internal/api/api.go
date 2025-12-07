package api

import (
	"context"
	"net/http"

	"link-checker/internal/model"
)

type LinkChecker interface {
	CheckLinks(ctx context.Context, links []string) (model.LinkSet, error)
}

type Reporter interface {
	Report(ctx context.Context, linkSetIDs []uint64) (model.Report, error)
}

type Service interface {
	LinkChecker
	Reporter
}

func New(service Service) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("POST /check-links", checkLinks(service))
	mux.Handle("POST /get-report", getReport(service))
	return mux
}

func unimplemented(w http.ResponseWriter) {
	code := http.StatusNotImplemented
	http.Error(w, http.StatusText(code), code)
}

func checkLinks(LinkChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// TODO
		unimplemented(w)
	}
}

func getReport(Reporter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// TODO
		unimplemented(w)
	}
}
