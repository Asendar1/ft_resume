package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return &Server{store: store}
}

func do(t *testing.T, s *Server, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	return rec
}

// Validation is the trust boundary: reject before touching the database.
func TestCreateValidation(t *testing.T) {
	cases := []struct {
		name string
		body string
		code int
	}{
		{"valid", `{"company":"Aurora","role":"Backend Engineer"}`, http.StatusCreated},
		{"empty company", `{"company":"  ","role":"Backend Engineer"}`, http.StatusUnprocessableEntity},
		{"missing role", `{"company":"Aurora"}`, http.StatusUnprocessableEntity},
		{"bad status", `{"company":"Aurora","role":"X","status":"hired"}`, http.StatusUnprocessableEntity},
		{"company too long", `{"company":"` + strings.Repeat("a", 121) + `","role":"X"}`, http.StatusUnprocessableEntity},
		{"broken json", `{"company":`, http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, newTestServer(t), http.MethodPost, "/applications", tc.body)
			if rec.Code != tc.code {
				t.Fatalf("want %d, got %d (%s)", tc.code, rec.Code, rec.Body)
			}
		})
	}
}

// The UNIQUE(company, role) constraint is what actually stops duplicates —
// the handler only translates the database error into 409.
func TestDuplicateIsRejectedBySchema(t *testing.T) {
	s := newTestServer(t)
	body := `{"company":"Aurora","role":"Backend Engineer"}`

	if rec := do(t, s, http.MethodPost, "/applications", body); rec.Code != http.StatusCreated {
		t.Fatalf("first insert: want 201, got %d (%s)", rec.Code, rec.Body)
	}
	// Same company+role, different case: COLLATE NOCASE must still collide.
	dup := `{"company":"aurora","role":"backend engineer"}`
	if rec := do(t, s, http.MethodPost, "/applications", dup); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate: want 409, got %d (%s)", rec.Code, rec.Body)
	}
}

func TestListFilterAndLimit(t *testing.T) {
	s := newTestServer(t)
	for _, b := range []string{
		`{"company":"A","role":"Dev"}`,
		`{"company":"B","role":"Dev","status":"interview"}`,
		`{"company":"C","role":"Dev","status":"offer"}`,
	} {
		if rec := do(t, s, http.MethodPost, "/applications", b); rec.Code != http.StatusCreated {
			t.Fatalf("seed: %d (%s)", rec.Code, rec.Body)
		}
	}

	rec := do(t, s, http.MethodGet, "/applications?status=interview", "")
	var got struct {
		Items []Application `json:"items"`
		Count int           `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Count != 1 || got.Items[0].Company != "B" {
		t.Fatalf("filter wrong: %+v", got)
	}

	if rec := do(t, s, http.MethodGet, "/applications?limit=0", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("limit=0: want 400, got %d", rec.Code)
	}
	if rec := do(t, s, http.MethodGet, "/applications?status=bogus", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad status filter: want 400, got %d", rec.Code)
	}
}

func TestUpdateStatusNotFound(t *testing.T) {
	rec := do(t, newTestServer(t), http.MethodPatch, "/applications/99/status", `{"status":"offer"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d (%s)", rec.Code, rec.Body)
	}
	if rec := do(t, newTestServer(t), http.MethodPatch, "/applications/99/status", `{"status":"nope"}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid status: want 422, got %d", rec.Code)
	}
}

type fakeAdvisor struct {
	text string
	err  error
}

func (f fakeAdvisor) Advice(context.Context, Application) (string, error) { return f.text, f.err }

// The upstream model is a dependency, so its failure is a 502 — not a 500.
func TestAdviceEndpoint(t *testing.T) {
	s := newTestServer(t)
	if rec := do(t, s, http.MethodPost, "/applications", `{"company":"Aurora","role":"Backend Engineer"}`); rec.Code != http.StatusCreated {
		t.Fatalf("seed: %d", rec.Code)
	}

	if rec := do(t, s, http.MethodPost, "/applications/1/advice", ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no model configured: want 503, got %d", rec.Code)
	}

	s.llm = fakeAdvisor{text: "Follow up on Tuesday."}
	rec := do(t, s, http.MethodPost, "/applications/1/advice", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "Follow up on Tuesday.") {
		t.Fatalf("advice not returned: %s", rec.Body)
	}

	s.llm = fakeAdvisor{err: errors.New("upstream 429")}
	if rec := do(t, s, http.MethodPost, "/applications/1/advice", ""); rec.Code != http.StatusBadGateway {
		t.Fatalf("upstream failure: want 502, got %d", rec.Code)
	}
}
