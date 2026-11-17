package main

import (
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
)

//go:embed web
var webFS embed.FS

// TaskView is the API representation of a task, including rendered markdown.
type TaskView struct {
	Task
	TitleHTML       string `json:"title_html"`
	DescriptionHTML string `json:"description_html"`
}

func view(t Task) TaskView {
	return TaskView{Task: t, TitleHTML: renderInline(t.Title), DescriptionHTML: renderMarkdown(t.Description)}
}

type Server struct {
	store  *Store
	prs    *PRClient
	broker *Broker
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/tasks", s.listTasks)
	mux.HandleFunc("POST /api/tasks", s.createTask)
	mux.HandleFunc("GET /api/tasks/{id}", s.getTask)
	mux.HandleFunc("PATCH /api/tasks/{id}", s.updateTask)
	mux.HandleFunc("DELETE /api/tasks/{id}", s.deleteTask)
	mux.HandleFunc("GET /api/prs", s.prStatus)
	mux.HandleFunc("GET /api/events", s.events)
	mux.HandleFunc("POST /api/render", s.render)
	sub, _ := fs.Sub(webFS, "web")
	mux.Handle("GET /", http.FileServerFS(sub))
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	code := http.StatusBadRequest
	if errors.Is(err, ErrNotFound) {
		code = http.StatusNotFound
	}
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func pathID(r *http.Request) (int, error) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		return 0, errors.New("invalid id")
	}
	return id, nil
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	out := []TaskView{}
	for _, t := range s.store.List() {
		if status == "" || t.Status == status {
			out = append(out, view(t))
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	t, err := s.store.Get(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view(t))
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var p TaskPatch
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, err)
		return
	}
	t, err := s.store.Create(p)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.broker.Notify()
	writeJSON(w, http.StatusCreated, view(t))
}

func (s *Server) updateTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var p TaskPatch
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, err)
		return
	}
	t, err := s.store.Update(id, p)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.broker.Notify()
	writeJSON(w, http.StatusOK, view(t))
}

func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := s.store.Delete(id); err != nil {
		writeErr(w, err)
		return
	}
	s.broker.Notify()
	w.WriteHeader(http.StatusNoContent)
}

// render: POST /api/render {"markdown": "..."} -> {"html": "..."}
func (s *Server) render(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Markdown string `json:"markdown"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"html": renderMarkdown(p.Markdown)})
}

// prStatus: GET /api/prs?ref=owner/repo/1&ref=owner/repo/2
func (s *Server) prStatus(w http.ResponseWriter, r *http.Request) {
	refs := r.URL.Query()["ref"]
	if len(refs) > 50 {
		refs = refs[:50]
	}
	for i, ref := range refs {
		refs[i] = strings.TrimSpace(ref)
	}
	writeJSON(w, http.StatusOK, s.prs.Get(r.Context(), refs))
}

func serve(addr, dbPath string) error {
	store, err := OpenStore(dbPath)
	if err != nil {
		return err
	}
	s := &Server{store: store, prs: NewPRClient(), broker: NewBroker()}
	log.Printf("senes listening on http://%s (db: %s)", displayAddr(addr), dbPath)
	return http.ListenAndServe(addr, s.Handler())
}

func displayAddr(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "localhost" + addr
	}
	return addr
}
