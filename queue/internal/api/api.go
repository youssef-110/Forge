// Package api is the HTTP front end for submitting and inspecting jobs.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/youssef-110/forge/queue/internal/jobs"
	"github.com/youssef-110/forge/queue/internal/store"
	"github.com/youssef-110/forge/queue/internal/telemetry"
)

type API struct{ st *store.Store }

// New builds the router. A non-empty token makes every /v1 route require
// "Authorization: Bearer <token>"; public deploys set it, local compose does not.
func New(st *store.Store, token string) http.Handler {
	a := &API{st: st}
	mux := http.NewServeMux()
	route := func(pattern string, h http.HandlerFunc) {
		mux.Handle(pattern, otelhttp.NewHandler(instrument(pattern, auth(token, h)), pattern))
	}
	route("POST /v1/jobs", a.create)
	route("GET /v1/jobs", a.list)
	route("GET /v1/jobs/{id}", a.get)
	route("POST /v1/jobs/{id}/cancel", a.cancel)
	route("POST /v1/jobs/{id}/retry", a.retry)
	route("GET /v1/stats", a.stats)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := st.Ping(r.Context()); err != nil {
			writeErr(w, http.StatusServiceUnavailable, "redis: "+err.Error())
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("GET /metrics", promhttp.Handler())
	return mux
}

type createReq struct {
	Type        string          `json:"type"`
	Payload     json.RawMessage `json:"payload"`
	Priority    *int            `json:"priority"`
	MaxAttempts *int            `json:"max_attempts"`
}

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	var req createReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := jobs.Validate(req.Type, req.Payload); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	j := &store.Job{Type: req.Type, Payload: req.Payload, Priority: 5, MaxAttempts: 3}
	if req.Priority != nil {
		j.Priority = *req.Priority
	}
	if req.MaxAttempts != nil {
		j.MaxAttempts = *req.MaxAttempts
	}
	if j.Priority < 0 || j.Priority > 9 {
		writeErr(w, http.StatusBadRequest, "priority must be between 0 and 9")
		return
	}
	if j.MaxAttempts < 1 || j.MaxAttempts > 10 {
		writeErr(w, http.StatusBadRequest, "max_attempts must be between 1 and 10")
		return
	}
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(r.Context(), carrier)
	j.TraceParent = carrier["traceparent"]
	if err := a.st.Create(r.Context(), j); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	telemetry.JobsEnqueued.WithLabelValues(j.Type).Inc()
	writeJSON(w, http.StatusCreated, j)
}

func (a *API) get(w http.ResponseWriter, r *http.Request) {
	j, err := a.st.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, j)
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	if state != "" && !slices.Contains(store.States, state) {
		writeErr(w, http.StatusBadRequest, "unknown state")
		return
	}
	limit := 20
	if s := r.URL.Query().Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 100 {
			writeErr(w, http.StatusBadRequest, "limit must be between 1 and 100")
			return
		}
		limit = n
	}
	js, err := a.st.List(r.Context(), state, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": js})
}

func (a *API) cancel(w http.ResponseWriter, r *http.Request) {
	a.change(w, r, a.st.Cancel)
}

func (a *API) retry(w http.ResponseWriter, r *http.Request) {
	a.change(w, r, a.st.Retry)
}

func (a *API) change(w http.ResponseWriter, r *http.Request, op func(context.Context, string) error) {
	id := r.PathValue("id")
	if err := op(r.Context(), id); err != nil {
		storeErr(w, err)
		return
	}
	a.get(w, r)
}

func (a *API) stats(w http.ResponseWriter, r *http.Request) {
	m, err := a.st.Stats(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func storeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrConflict):
		writeErr(w, http.StatusConflict, err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func auth(token string, h http.HandlerFunc) http.HandlerFunc {
	if token == "" {
		return h
	}
	want := []byte("Bearer " + token)
	return func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			writeErr(w, http.StatusUnauthorized, "missing or invalid bearer token")
			return
		}
		h(w, r)
	}
}

type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (s *statusRecorder) WriteHeader(code int) { s.code = code; s.ResponseWriter.WriteHeader(code) }

func instrument(route string, h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, code: http.StatusOK}
		start := time.Now()
		h(rec, r)
		telemetry.HTTPDuration.WithLabelValues(route, strconv.Itoa(rec.code)).Observe(time.Since(start).Seconds())
	})
}
