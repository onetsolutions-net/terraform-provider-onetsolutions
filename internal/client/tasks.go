package client

import (
	"context"
	"encoding/json"
	"net/http"
)

// Task statuses shared by instance and domain tasks.
const (
	TaskQueued    = "queued"
	TaskPending   = "pending"
	TaskRunning   = "running"
	TaskCompleted = "completed"
	TaskResolved  = "resolved"
	TaskFailed    = "failed"
	TaskCancelled = "cancelled"
)

// InstanceTask is an asynchronous operation on an instance (InstanceTask in the schema).
type InstanceTask struct {
	ID          string          `json:"id"`
	InstanceID  string          `json:"instance_id"`
	TaskType    string          `json:"task_type"`
	Status      string          `json:"status"`
	Progress    int             `json:"progress"`
	Result      json.RawMessage `json:"result"`
	StartedAt   *string         `json:"started_at"`
	CompletedAt *string         `json:"completed_at"`
	Created     string          `json:"created"`
}

// FailureMessage returns what the task's result says about a failure, if anything.
func (t *InstanceTask) FailureMessage() string { return resultMessage(t.Result) }

// DomainTask is an asynchronous operation on a domain (DomainTask in the schema).
type DomainTask struct {
	ID          string  `json:"id"`
	TaskType    string  `json:"task_type"`
	Status      string  `json:"status"`
	Progress    int     `json:"progress"`
	Created     string  `json:"created"`
	StartedAt   *string `json:"started_at"`
	CompletedAt *string `json:"completed_at"`
}

var (
	routeListInstanceTasks = newRoute(http.MethodGet,
		"/v1/organizations/{organization_id}/projects/{project_id}/compute/instances/{instance_id}/tasks")
	routeGetInstanceTask = newRoute(http.MethodGet,
		"/v1/organizations/{organization_id}/projects/{project_id}/compute/instances/{instance_id}/tasks/{id}")
	routeGetDomainTask = newRoute(http.MethodGet,
		"/v1/organizations/{organization_id}/projects/{project_id}/domains/{id}/tasks/{task_id}")
)

// GetInstanceTask reads one task of an instance.
func (c *Client) GetInstanceTask(ctx context.Context, scope Scope, instanceID, taskID string) (*InstanceTask, error) {
	var task InstanceTask
	err := c.do(ctx, routeGetInstanceTask, scope.Params("instance_id", instanceID, "id", taskID), nil, &task)
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// ListRecentInstanceTasks reads the most recent tasks of an instance (the first page: the API
// lists them newest first), enough to find the tasks a request just queued without reading the
// instance's whole history.
func (c *Client) ListRecentInstanceTasks(ctx context.Context, scope Scope, instanceID string) ([]InstanceTask, error) {
	return listFirstPage[InstanceTask](ctx, c, routeListInstanceTasks, scope.Params("instance_id", instanceID))
}

// GetDomainTask reads one task of a domain.
func (c *Client) GetDomainTask(ctx context.Context, scope Scope, domainID, taskID string) (*DomainTask, error) {
	var task DomainTask
	err := c.do(ctx, routeGetDomainTask, scope.Params("id", domainID, "task_id", taskID), nil, &task)
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func resultMessage(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var object map[string]any
	if json.Unmarshal(raw, &object) != nil {
		return ""
	}
	for _, key := range []string{"error", "reason", "message", "detail"} {
		if value, ok := object[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}
