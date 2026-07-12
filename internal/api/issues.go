package api

import (
	"database/sql"
	"net/http"

	"forge.coltco.net/austin/vektor/internal/authn"
	"forge.coltco.net/austin/vektor/internal/models"

	"github.com/google/uuid"
)

func (s *Server) handleListIssues(w http.ResponseWriter, r *http.Request) {
	projectKey := r.PathValue("projectKey")

	rows, err := s.db.QueryContext(r.Context(),
		`SELECT i.id, i.project_id, i.number, i.title, i.description, i.status, i.priority,
		        i.assignee_id, i.created_by, i.created_at, i.updated_at
		 FROM issues i
		 JOIN projects p ON p.id = i.project_id
		 WHERE p.key = ?
		 ORDER BY i.updated_at DESC`, projectKey)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var issues []models.Issue
	for rows.Next() {
		var i models.Issue
		if err := rows.Scan(&i.ID, &i.ProjectID, &i.Number, &i.Title, &i.Description,
			&i.Status, &i.Priority, &i.AssigneeID, &i.CreatedBy, &i.CreatedAt, &i.UpdatedAt); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		issues = append(issues, i)
	}

	writeJSON(w, http.StatusOK, issues)
}

func (s *Server) handleCreateIssue(w http.ResponseWriter, r *http.Request) {
	projectKey := r.PathValue("projectKey")

	var input models.CreateIssueInput
	if err := readJSON(r, &input); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if input.Title == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}

	// Get project ID
	var projectID string
	err := s.db.QueryRowContext(r.Context(), "SELECT id FROM projects WHERE key = ?", projectKey).Scan(&projectID)
	if err == sql.ErrNoRows {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Get next issue number
	var maxNum int
	s.db.QueryRowContext(r.Context(), "SELECT COALESCE(MAX(number), 0) FROM issues WHERE project_id = ?", projectID).Scan(&maxNum)

	claims := authn.UserFromContext(r.Context())
	id := uuid.New().String()
	status := input.Status
	if status == "" {
		status = models.StatusBacklog
	}
	priority := input.Priority
	if priority == "" {
		priority = models.PriorityNone
	}

	_, err = s.db.ExecContext(r.Context(),
		`INSERT INTO issues (id, project_id, number, title, description, status, priority, assignee_id, created_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, projectID, maxNum+1, input.Title, input.Description, status, priority, input.AssigneeID, claims.Sub,
	)
	if err != nil {
		http.Error(w, "could not create issue", http.StatusInternalServerError)
		return
	}

	var issue models.Issue
	s.db.QueryRowContext(r.Context(),
		`SELECT id, project_id, number, title, description, status, priority, assignee_id, created_by, created_at, updated_at
		 FROM issues WHERE id = ?`, id,
	).Scan(&issue.ID, &issue.ProjectID, &issue.Number, &issue.Title, &issue.Description,
		&issue.Status, &issue.Priority, &issue.AssigneeID, &issue.CreatedBy, &issue.CreatedAt, &issue.UpdatedAt)

	writeJSON(w, http.StatusCreated, issue)
}

func (s *Server) handleUpdateIssue(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var input models.UpdateIssueInput
	if err := readJSON(r, &input); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Build dynamic update
	var sets []string
	var args []any

	if input.Title != nil {
		sets = append(sets, "title = ?")
		args = append(args, *input.Title)
	}
	if input.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *input.Description)
	}
	if input.Status != nil {
		sets = append(sets, "status = ?")
		args = append(args, *input.Status)
	}
	if input.Priority != nil {
		sets = append(sets, "priority = ?")
		args = append(args, *input.Priority)
	}
	if input.AssigneeID != nil {
		sets = append(sets, "assignee_id = ?")
		args = append(args, *input.AssigneeID)
	}

	if len(sets) == 0 {
		http.Error(w, "no fields to update", http.StatusBadRequest)
		return
	}

	sets = append(sets, "updated_at = CURRENT_TIMESTAMP")
	args = append(args, id)

	query := "UPDATE issues SET "
	for i, s := range sets {
		if i > 0 {
			query += ", "
		}
		query += s
	}
	query += " WHERE id = ?"

	result, err := s.db.ExecContext(r.Context(), query, args...)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		http.Error(w, "issue not found", http.StatusNotFound)
		return
	}

	var issue models.Issue
	s.db.QueryRowContext(r.Context(),
		`SELECT id, project_id, number, title, description, status, priority, assignee_id, created_by, created_at, updated_at
		 FROM issues WHERE id = ?`, id,
	).Scan(&issue.ID, &issue.ProjectID, &issue.Number, &issue.Title, &issue.Description,
		&issue.Status, &issue.Priority, &issue.AssigneeID, &issue.CreatedBy, &issue.CreatedAt, &issue.UpdatedAt)

	writeJSON(w, http.StatusOK, issue)
}
