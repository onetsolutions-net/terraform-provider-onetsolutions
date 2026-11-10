package waiter

import (
	"context"
	"fmt"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
)

var (
	taskPending = []string{client.TaskQueued, client.TaskPending, client.TaskRunning}
	taskDone    = []string{client.TaskCompleted, client.TaskResolved}
)

// TaskFailedError is returned when a task ends failed or canceled.
type TaskFailedError struct {
	TaskID   string
	TaskType string
	Status   string
	Message  string
}

func (e *TaskFailedError) Error() string {
	msg := fmt.Sprintf("task %s (%s) ended %s", e.TaskID, e.TaskType, e.Status)
	if e.Message != "" {
		msg += ": " + e.Message
	}
	return msg
}

// InstanceTask waits until a task of an instance completes. A failed or canceled task is a
// *TaskFailedError.
func InstanceTask(ctx context.Context, cfg Config, c *client.Client, scope client.Scope, instanceID, taskID string) (*client.InstanceTask, error) {
	task, err := Wait[*client.InstanceTask]{
		Description: fmt.Sprintf("task %s of instance %s", taskID, instanceID),
		Pending:     taskPending,
		Target:      taskDone,
		Config:      cfg,
		Refresh: func(ctx context.Context) (*client.InstanceTask, string, error) {
			task, err := c.GetInstanceTask(ctx, scope, instanceID, taskID)
			if err != nil {
				return nil, "", err
			}
			return task, task.Status, nil
		},
	}.Run(ctx)
	if err != nil {
		if task != nil {
			return task, &TaskFailedError{TaskID: task.ID, TaskType: task.TaskType, Status: task.Status, Message: task.FailureMessage()}
		}
		return nil, err
	}
	return task, nil
}

// DomainTask waits until a task of a domain completes. A failed or canceled task is a
// *TaskFailedError.
func DomainTask(ctx context.Context, cfg Config, c *client.Client, scope client.Scope, domainID, taskID string) (*client.DomainTask, error) {
	task, err := Wait[*client.DomainTask]{
		Description: fmt.Sprintf("task %s of domain %s", taskID, domainID),
		Pending:     taskPending,
		Target:      taskDone,
		Config:      cfg,
		Refresh: func(ctx context.Context) (*client.DomainTask, string, error) {
			task, err := c.GetDomainTask(ctx, scope, domainID, taskID)
			if err != nil {
				return nil, "", err
			}
			return task, task.Status, nil
		},
	}.Run(ctx)
	if err != nil {
		if task != nil {
			return task, &TaskFailedError{TaskID: task.ID, TaskType: task.TaskType, Status: task.Status}
		}
		return nil, err
	}
	return task, nil
}
