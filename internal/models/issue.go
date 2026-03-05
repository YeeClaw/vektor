package models

import "time"

type IssueStatus string

const (
	StatusBacklog    IssueStatus = "backlog"
	StatusTodo       IssueStatus = "todo"
	StatusInProgress IssueStatus = "in_progress"
	StatusDone       IssueStatus = "done"
	StatusCancelled  IssueStatus = "cancelled"
)

type IssuePriority string

const (
	PriorityNone   IssuePriority = "none"
	PriorityUrgent IssuePriority = "urgent"
	PriorityHigh   IssuePriority = "high"
	PriorityMedium IssuePriority = "medium"
	PriorityLow    IssuePriority = "low"
)

type Issue struct {
	ID          string        `json:"id"`
	ProjectID   string        `json:"projectId"`
	Number      int           `json:"number"`
	Title       string        `json:"title"`
	Description string        `json:"description,omitempty"`
	Status      IssueStatus   `json:"status"`
	Priority    IssuePriority `json:"priority"`
	AssigneeID  *string       `json:"assigneeId,omitempty"`
	CreatedBy   string        `json:"createdBy"`
	CreatedAt   time.Time     `json:"createdAt"`
	UpdatedAt   time.Time     `json:"updatedAt"`
	Labels      []Label       `json:"labels,omitempty"`
}

type CreateIssueInput struct {
	Title       string        `json:"title"`
	Description string        `json:"description,omitempty"`
	Status      IssueStatus   `json:"status,omitempty"`
	Priority    IssuePriority `json:"priority,omitempty"`
	AssigneeID  *string       `json:"assigneeId,omitempty"`
	LabelIDs    []string      `json:"labelIds,omitempty"`
}

type UpdateIssueInput struct {
	Title       *string        `json:"title,omitempty"`
	Description *string        `json:"description,omitempty"`
	Status      *IssueStatus   `json:"status,omitempty"`
	Priority    *IssuePriority `json:"priority,omitempty"`
	AssigneeID  *string        `json:"assigneeId,omitempty"`
	LabelIDs    []string       `json:"labelIds,omitempty"`
}
