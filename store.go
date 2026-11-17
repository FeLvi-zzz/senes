package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var Statuses = []string{"doing", "todo", "waiting", "backlog", "done"}

func validStatus(s string) bool {
	for _, v := range Statuses {
		if v == s {
			return true
		}
	}
	return false
}

type Task struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TaskPatch holds optional fields for create/update.
type TaskPatch struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Status      *string `json:"status"`
	// Reordering (at most one): place before/after another task, or at "top"/"bottom".
	Before   *int    `json:"before,omitempty"`
	After    *int    `json:"after,omitempty"`
	Position *string `json:"position,omitempty"`
}

var ErrNotFound = errors.New("task not found")

type Store struct {
	mu     sync.Mutex
	path   string
	NextID int     `json:"next_id"`
	Tasks  []*Task `json:"tasks"`
}

func OpenStore(path string) (*Store, error) {
	s := &Store{path: path, NextID: 1}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return s, nil
}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) List() []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Task, 0, len(s.Tasks))
	for _, t := range s.Tasks {
		out = append(out, *t)
	}
	return out
}

func (s *Store) find(id int) (int, *Task) {
	for i, t := range s.Tasks {
		if t.ID == id {
			return i, t
		}
	}
	return -1, nil
}

func (s *Store) Get(id int) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, t := s.find(id)
	if t == nil {
		return Task{}, ErrNotFound
	}
	return *t, nil
}

func applyPatch(t *Task, p TaskPatch) error {
	if p.Title != nil {
		t.Title = *p.Title
	}
	if p.Description != nil {
		t.Description = *p.Description
	}
	if p.Status != nil {
		if !validStatus(*p.Status) {
			return fmt.Errorf("invalid status %q (want one of %v)", *p.Status, Statuses)
		}
		t.Status = *p.Status
	}
	if t.Title == "" {
		return errors.New("title is required")
	}
	return nil
}

func (s *Store) Create(p TaskPatch) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	t := &Task{Status: "todo", CreatedAt: now, UpdatedAt: now}
	if err := applyPatch(t, p); err != nil {
		return Task{}, err
	}
	t.ID = s.NextID
	s.NextID++
	s.Tasks = append(s.Tasks, t)
	if err := s.save(); err != nil {
		return Task{}, err
	}
	return *t, nil
}

func (s *Store) Update(id int, p TaskPatch) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, t := s.find(id)
	if t == nil {
		return Task{}, ErrNotFound
	}
	next := *t
	if err := applyPatch(&next, p); err != nil {
		return Task{}, err
	}
	if p.Title != nil || p.Description != nil || p.Status != nil {
		next.UpdatedAt = time.Now().UTC()
	}
	if err := s.reorder(t, p); err != nil {
		return Task{}, err
	}
	*t = next
	if err := s.save(); err != nil {
		return Task{}, err
	}
	return *t, nil
}

func (s *Store) Delete(id int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, _ := s.find(id)
	if i < 0 {
		return ErrNotFound
	}
	s.Tasks = append(s.Tasks[:i], s.Tasks[i+1:]...)
	return s.save()
}

// reorder moves t within s.Tasks according to p.Before / p.After / p.Position.
// The slice order is the display order (statuses are filtered from it).
func (s *Store) reorder(t *Task, p TaskPatch) error {
	n := 0
	for _, set := range []bool{p.Before != nil, p.After != nil, p.Position != nil} {
		if set {
			n++
		}
	}
	if n == 0 {
		return nil
	}
	if n > 1 {
		return errors.New("specify only one of before, after, position")
	}
	var anchor int
	switch {
	case p.Before != nil:
		anchor = *p.Before
	case p.After != nil:
		anchor = *p.After
	default:
		if *p.Position != "top" && *p.Position != "bottom" {
			return fmt.Errorf("invalid position %q (want top or bottom)", *p.Position)
		}
	}
	if (p.Before != nil || p.After != nil) && anchor == t.ID {
		return errors.New("cannot position a task relative to itself")
	}
	if p.Before != nil || p.After != nil {
		if _, a := s.find(anchor); a == nil {
			return fmt.Errorf("task #%d: %w", anchor, ErrNotFound)
		}
	}
	i, _ := s.find(t.ID)
	rest := append(append([]*Task{}, s.Tasks[:i]...), s.Tasks[i+1:]...)
	var at int
	switch {
	case p.Position != nil && *p.Position == "top":
		at = 0
	case p.Position != nil:
		at = len(rest)
	default:
		for j, x := range rest {
			if x.ID == anchor {
				at = j
			}
		}
		if p.After != nil {
			at++
		}
	}
	s.Tasks = append(rest[:at], append([]*Task{t}, rest[at:]...)...)
	return nil
}
