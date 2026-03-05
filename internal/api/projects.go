package api

import (
	"net/http"

	"forge.coltco.net/austin/vektor/internal/auth"
	"forge.coltco.net/austin/vektor/internal/models"

	"github.com/google/uuid"
)

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(),
		"SELECT id, key, name, description, created_by, created_at, updated_at FROM projects ORDER BY updated_at DESC")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var projects []models.Project
	for rows.Next() {
		var p models.Project
		if err := rows.Scan(&p.ID, &p.Key, &p.Name, &p.Description, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		projects = append(projects, p)
	}

	writeJSON(w, http.StatusOK, projects)
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Key         string `json:"key"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := readJSON(r, &input); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if input.Key == "" || input.Name == "" {
		http.Error(w, "key and name are required", http.StatusBadRequest)
		return
	}

	claims := auth.UserFromContext(r.Context())
	id := uuid.New().String()

	_, err := s.db.ExecContext(r.Context(),
		"INSERT INTO projects (id, key, name, description, created_by) VALUES (?, ?, ?, ?, ?)",
		id, input.Key, input.Name, input.Description, claims.Sub,
	)
	if err != nil {
		http.Error(w, "could not create project", http.StatusInternalServerError)
		return
	}

	var p models.Project
	s.db.QueryRowContext(r.Context(),
		"SELECT id, key, name, description, created_by, created_at, updated_at FROM projects WHERE id = ?", id,
	).Scan(&p.ID, &p.Key, &p.Name, &p.Description, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt)

	writeJSON(w, http.StatusCreated, p)
}
