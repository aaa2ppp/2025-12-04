package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

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

type checkLinksRequest struct {
	Links []string `json:"links,omitempty"`
}

type checkLinksResponse struct {
	Links    map[string]string `json:"links,omitempty"`
	LinksNum uint64            `json:"links_num,omitempty"`
}

func checkLinks(checker LinkChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "content type must be application/json", http.StatusBadRequest)
			return
		}

		var req checkLinksRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "can't parse request body", http.StatusBadRequest)
			return
		}

		if len(req.Links) == 0 {
			http.Error(w, "links list is empty", http.StatusBadRequest)
			return
		}

		linkSet, err := checker.CheckLinks(r.Context(), req.Links)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		respLinks := make(map[string]string, len(linkSet.Links))
		for _, l := range linkSet.Links {
			var state string
			if l.Available {
				state = "available"
			} else {
				state = "not available"
			}
			respLinks[l.Name] = state
		}

		resp := checkLinksResponse{
			Links:    respLinks,
			LinksNum: linkSet.ID,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

type getReportRequest struct {
	LinksList []uint64 `json:"links_list,omitempty"`
}

func getReport(reporter Reporter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "content type must be application/json", http.StatusBadRequest)
			return
		}

		var req getReportRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "can't parse request body", http.StatusBadRequest)
			return
		}

		if len(req.LinksList) == 0 {
			http.Error(w, "links list is empty", http.StatusBadRequest)
			return
		}

		report, err := reporter.Report(r.Context(), req.LinksList)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", report.ContentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(report.Body)))
		w.Write(report.Body)
	}
}
