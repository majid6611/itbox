package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
)

type elItem struct {
	Tags []string `json:"tags"`
}

type elBody struct {
	Items []elItem         `json:"items"`
	Names []string         `json:"names"`
	By    map[string][]int `json:"by"`
	Inner *elItem          `json:"inner"`
	Raw   []byte           `json:"raw,omitempty"`
}

func TestEmptyListsNeverNull(t *testing.T) {
	in := elBody{Items: []elItem{{}}, By: map[string][]int{"a": nil}, Inner: &elItem{}}
	got, err := emptyLists(nil, "200", in)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got)
	want := `{"items":[{"tags":[]}],"names":[],"by":{"a":[]},"inner":{"tags":[]}}`
	if string(b) != want {
		t.Fatalf("got %s\nwant %s", b, want)
	}
	if in.Names != nil {
		t.Fatal("input was modified")
	}
}

func TestResponsesHaveNoNullLists(t *testing.T) {
	router := chi.NewMux()
	api := humachi.New(router, apiConfig())
	huma.Get(api, "/x", func(ctx context.Context, _ *struct{}) (*struct {
		Body struct {
			Items []elItem `json:"items"`
		}
	}, error) {
		return &struct {
			Body struct {
				Items []elItem `json:"items"`
			}
		}{}, nil
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if got := strings.TrimSpace(rec.Body.String()); got != `{"items":[]}` {
		t.Fatalf("got %s", got)
	}
}
