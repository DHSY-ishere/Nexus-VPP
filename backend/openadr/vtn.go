package openadr

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// BasePath is where the VTN API is mounted.
const BasePath = "/openadr3/3.0.1"

// VTN is an in-memory Virtual Top Node.
type VTN struct {
	mu       sync.RWMutex
	programs map[string]Program
	events   map[string]Event
	seq      int
	token    string
	Now      func() time.Time
}

// NewVTN creates a VTN. If token is non-empty every request must carry
// "Authorization: Bearer <token>".
func NewVTN(token string) *VTN {
	return &VTN{programs: map[string]Program{}, events: map[string]Event{}, token: token, Now: time.Now}
}

func (v *VTN) id(prefix string) string {
	v.seq++
	return fmt.Sprintf("%s-%04d", prefix, v.seq)
}

// Events returns a snapshot of every event (used by an in-process VEN).
func (v *VTN) Events() []Event {
	v.mu.RLock()
	defer v.mu.RUnlock()
	out := make([]Event, 0, len(v.events))
	for _, e := range v.events {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Register mounts the API on mux.
func (v *VTN) Register(mux *http.ServeMux) {
	h := func(f http.HandlerFunc) http.HandlerFunc { return v.auth(f) }
	mux.HandleFunc("GET "+BasePath+"/programs", h(v.listPrograms))
	mux.HandleFunc("POST "+BasePath+"/programs", h(v.createProgram))
	mux.HandleFunc("GET "+BasePath+"/events", h(v.listEvents))
	mux.HandleFunc("POST "+BasePath+"/events", h(v.createEvent))
	mux.HandleFunc("GET "+BasePath+"/events/{id}", h(v.getEvent))
	mux.HandleFunc("PUT "+BasePath+"/events/{id}", h(v.updateEvent))
	mux.HandleFunc("DELETE "+BasePath+"/events/{id}", h(v.deleteEvent))
}

func (v *VTN) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if v.token != "" {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if subtle.ConstantTimeCompare([]byte(got), []byte(v.token)) != 1 {
				problem(w, http.StatusUnauthorized, "missing or invalid bearer token")
				return
			}
		}
		next(w, r)
	}
}

// problem writes an OpenADR-style problem response.
func problem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "detail": detail})
}

func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (v *VTN) listPrograms(w http.ResponseWriter, r *http.Request) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	out := make([]Program, 0, len(v.programs))
	for _, p := range v.programs {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	write(w, http.StatusOK, out)
}

func (v *VTN) createProgram(w http.ResponseWriter, r *http.Request) {
	var p Program
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&p); err != nil || strings.TrimSpace(p.ProgramName) == "" {
		problem(w, http.StatusBadRequest, "programName is required")
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	p.ID, p.CreatedAt = v.id("prog"), v.Now().UTC().Format(time.RFC3339Nano)
	v.programs[p.ID] = p
	write(w, http.StatusCreated, p)
}

func (v *VTN) listEvents(w http.ResponseWriter, r *http.Request) {
	prog := r.URL.Query().Get("programID")
	var out []Event
	for _, e := range v.Events() {
		if prog == "" || e.ProgramID == prog {
			out = append(out, e)
		}
	}
	if out == nil {
		out = []Event{}
	}
	write(w, http.StatusOK, out)
}

func (v *VTN) decodeEvent(w http.ResponseWriter, r *http.Request) (Event, bool) {
	var e Event
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<18)).Decode(&e); err != nil {
		problem(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return e, false
	}
	if err := e.Validate(); err != nil {
		problem(w, http.StatusBadRequest, err.Error())
		return e, false
	}
	return e, true
}

func (v *VTN) createEvent(w http.ResponseWriter, r *http.Request) {
	e, ok := v.decodeEvent(w, r)
	if !ok {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, ok := v.programs[e.ProgramID]; !ok {
		problem(w, http.StatusBadRequest, "unknown programID "+e.ProgramID)
		return
	}
	e.ID, e.CreatedAt = v.id("evt"), v.Now().UTC().Format(time.RFC3339Nano)
	v.events[e.ID] = e
	write(w, http.StatusCreated, e)
}

func (v *VTN) getEvent(w http.ResponseWriter, r *http.Request) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	e, ok := v.events[r.PathValue("id")]
	if !ok {
		problem(w, http.StatusNotFound, "no such event")
		return
	}
	write(w, http.StatusOK, e)
}

func (v *VTN) updateEvent(w http.ResponseWriter, r *http.Request) {
	e, ok := v.decodeEvent(w, r)
	if !ok {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	old, found := v.events[r.PathValue("id")]
	if !found {
		problem(w, http.StatusNotFound, "no such event")
		return
	}
	e.ID, e.CreatedAt = old.ID, old.CreatedAt
	v.events[e.ID] = e
	write(w, http.StatusOK, e)
}

func (v *VTN) deleteEvent(w http.ResponseWriter, r *http.Request) {
	v.mu.Lock()
	defer v.mu.Unlock()
	e, ok := v.events[r.PathValue("id")]
	if !ok {
		problem(w, http.StatusNotFound, "no such event")
		return
	}
	delete(v.events, e.ID)
	write(w, http.StatusOK, e)
}
