package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	store *Store
	llm   Advisor // nil when no API key is configured
}

func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /applications", s.create)
	mux.HandleFunc("GET /applications", s.list)
	mux.HandleFunc("PATCH /applications/{id}/status", s.updateStatus)
	mux.HandleFunc("POST /applications/{id}/advice", s.advice)
	return mux
}

// ---------- validation (trust boundary) ----------

type createInput struct {
	Company string `json:"company"`
	Role    string `json:"role"`
	Status  Status `json:"status"`
	Notes   string `json:"notes"`
}

func (in *createInput) normalize() map[string]string {
	in.Company = strings.TrimSpace(in.Company)
	in.Role = strings.TrimSpace(in.Role)
	in.Notes = strings.TrimSpace(in.Notes)
	if in.Status == "" {
		in.Status = StatusApplied
	}

	errs := map[string]string{}
	switch {
	case in.Company == "":
		errs["company"] = "required"
	case len(in.Company) > 120:
		errs["company"] = "too long (max 120)"
	}
	switch {
	case in.Role == "":
		errs["role"] = "required"
	case len(in.Role) > 120:
		errs["role"] = "too long (max 120)"
	}
	if !in.Status.Valid() {
		errs["status"] = "must be one of applied, screen, interview, offer, rejected"
	}
	if len(in.Notes) > 2000 {
		errs["notes"] = "too long (max 2000)"
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var in createInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if errs := in.normalize(); errs != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"errors": errs})
		return
	}

	a, err := s.store.Create(r.Context(), Application{
		Company: in.Company, Role: in.Role, Status: in.Status, Notes: in.Notes,
	})
	if errors.Is(err, ErrDuplicate) {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not store application")
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit := 50
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			writeErr(w, http.StatusBadRequest, "limit must be an integer between 1 and 200")
			return
		}
		limit = n
	}

	status := Status(q.Get("status"))
	if status != "" && !status.Valid() {
		writeErr(w, http.StatusBadRequest, "unknown status filter")
		return
	}

	items, err := s.store.List(r.Context(), status, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not read applications")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (s *Server) updateStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	var in struct {
		Status Status `json:"status"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if !in.Status.Valid() {
		writeErr(w, http.StatusUnprocessableEntity, "status must be one of applied, screen, interview, offer, rejected")
		return
	}

	a, err := s.store.UpdateStatus(r.Context(), id, in.Status)
	if errors.Is(err, ErrNotFound) {
		writeErr(w, http.StatusNotFound, "no application with that id")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not update application")
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) advice(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	a, err := s.store.Get(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		writeErr(w, http.StatusNotFound, "no application with that id")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not read application")
		return
	}
	if s.llm == nil {
		writeErr(w, http.StatusServiceUnavailable, "no model configured")
		return
	}

	// Hard ceiling on the upstream call: the request budget is ours, not theirs.
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()

	text, err := s.llm.Advice(ctx, a)
	if err != nil {
		slog.Error("advisor failed", "application_id", a.ID, "err", err)
		writeErr(w, http.StatusBadGateway, "model call failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"application_id": a.ID, "advice": text})
}

// ---------- helpers ----------

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeErr(w, http.StatusBadRequest, "id must be a positive integer")
		return 0, false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
