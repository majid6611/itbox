// Package api is registry-server's HTTP surface: two endpoints client
// servers call (index, bundle download) behind per-client bearer auth,
// and a small admin surface (behind a separate admin token) for issuing
// and revoking client keys. See the itbox repo's design notes on the
// module registry for the full picture — this is deliberately thin,
// GitHub's Releases/contents API is the actual storage.
package api

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"it-platform/registry/internal/github"
	"it-platform/registry/internal/store"
)

type Server struct {
	Store      *store.Store
	GitHub     *github.Client
	AdminToken string

	indexMu       sync.Mutex
	indexCache    []byte
	indexCachedAt time.Time
}

// indexCacheTTL keeps repeated polling from every installed client server
// (potentially many, all checking on their own schedule) from each
// hitting GitHub's API directly — a burst of "check for updates" clicks
// across clients collapses to one real GitHub call.
const indexCacheTTL = 30 * time.Second

func NewRouter(s *Server) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	r.Route("/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(s.requireClientAuth)
			r.Get("/index", s.handleIndex)
			r.Get("/modules/{id}/{version}/bundle", s.handleBundle)
		})

		r.Route("/admin/clients", func(r chi.Router) {
			r.Use(s.requireAdminAuth)
			r.Get("/", s.handleListClients)
			r.Post("/", s.handleCreateClient)
			r.Delete("/{id}", s.handleRevokeClient)
		})
	})

	return r
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) > len(prefix) && h[:len(prefix)] == prefix {
		return h[len(prefix):]
	}
	return ""
}

func (s *Server) requireClientAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := bearerToken(r)
		if key == "" {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		_, err := s.Store.Authenticate(key)
		switch {
		case err == nil:
			next.ServeHTTP(w, r)
		case errors.Is(err, store.ErrNotFound):
			http.Error(w, "invalid key", http.StatusUnauthorized)
		case errors.Is(err, store.ErrRevoked):
			http.Error(w, "key revoked", http.StatusForbidden)
		default:
			log.Printf("authenticate client: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
	})
}

func (s *Server) requireAdminAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if bearerToken(r) != s.AdminToken || s.AdminToken == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	if s.indexCache == nil || time.Since(s.indexCachedAt) > indexCacheTTL {
		fresh, err := s.GitHub.FetchIndex(r.Context())
		if err != nil {
			log.Printf("fetch index: %v", err)
			// A stale cache is still useful (better than a hard failure
			// for every client mid-outage); only fail if there's nothing
			// cached at all yet.
			if s.indexCache == nil {
				http.Error(w, "could not fetch module index", http.StatusBadGateway)
				return
			}
		} else {
			s.indexCache = fresh
			s.indexCachedAt = time.Now()
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(s.indexCache)
}

func (s *Server) handleBundle(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	version := chi.URLParam(r, "version")
	tag := fmt.Sprintf("%s-v%s", id, version)
	assetName := fmt.Sprintf("%s-%s.tar.gz", id, version)

	body, err := s.GitHub.FetchBundleAsset(r.Context(), tag, assetName)
	if err != nil {
		log.Printf("fetch bundle %s/%s: %v", id, version, err)
		http.Error(w, "could not fetch module bundle", http.StatusBadGateway)
		return
	}
	defer body.Close()

	w.Header().Set("Content-Type", "application/gzip")
	if _, err := io.Copy(w, body); err != nil {
		log.Printf("stream bundle %s/%s: %v", id, version, err)
	}
}

type createClientRequest struct {
	Name string `json:"name"`
}

type createClientResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Key  string `json:"key"`
}

func (s *Server) handleCreateClient(w http.ResponseWriter, r *http.Request) {
	var req createClientRequest
	if err := decodeJSON(r, &req); err != nil || req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	id, key, err := s.Store.CreateClient(req.Name)
	if err != nil {
		log.Printf("create client: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, createClientResponse{ID: id, Name: req.Name, Key: key})
}

func (s *Server) handleListClients(w http.ResponseWriter, r *http.Request) {
	clients, err := s.Store.ListClients()
	if err != nil {
		log.Printf("list clients: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, clients)
}

func (s *Server) handleRevokeClient(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := s.Store.RevokeClient(id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		log.Printf("revoke client: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
