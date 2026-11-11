package backup

import (
	"context"
	"fmt"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/waiter"
)

var (
	taskPending = []string{client.TaskQueued, client.TaskPending, client.TaskRunning}
	taskDone    = []string{client.TaskCompleted, client.TaskResolved}
)

// instanceBusy matches the refusals while another task holds the instance or the instance is
// locked; any other refusal (wrong state, missing option) is final.
func instanceBusy(err error) bool {
	return client.HasCode(err, client.CodeInstanceTaskInProgress) || client.HasCode(err, client.CodeInstanceLocked)
}

// retryWhileBusy runs op until the instance accepts it, op fails otherwise, or ctx ends.
func retryWhileBusy(ctx context.Context, cfg waiter.Config, description string, op func(context.Context) error) error {
	return waiter.Retry(ctx, cfg, description, op, instanceBusy)
}

// instanceGone reports whether an instance is gone for its owner: not found, cancelled or
// terminated. Its backups can then no longer be used.
func instanceGone(ctx context.Context, c *client.Client, scope client.Scope, instanceID string) (bool, error) {
	instance, err := c.GetInstance(ctx, scope, instanceID)
	if client.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return instance.Gone(), nil
}

// waitTask waits for a task of an instance like waiter.InstanceTask does, calling progress whenever
// its status or completion changes.
func waitTask(ctx context.Context, cfg waiter.Config, c *client.Client, scope client.Scope, instanceID, taskID string, progress func(*client.InstanceTask)) error {
	var lastStatus string
	lastProgress := -1
	task, err := waiter.Wait[*client.InstanceTask]{
		Description: fmt.Sprintf("task %s of instance %s", taskID, instanceID),
		Pending:     taskPending,
		Target:      taskDone,
		Config:      cfg,
		Refresh: func(ctx context.Context) (*client.InstanceTask, string, error) {
			task, err := c.GetInstanceTask(ctx, scope, instanceID, taskID)
			if err != nil {
				return nil, "", err
			}
			if progress != nil && (task.Status != lastStatus || task.Progress != lastProgress) {
				lastStatus, lastProgress = task.Status, task.Progress
				progress(task)
			}
			return task, task.Status, nil
		},
	}.Run(ctx)
	if err != nil && task != nil {
		return &waiter.TaskFailedError{TaskID: task.ID, TaskType: task.TaskType, Status: task.Status, Message: task.FailureMessage()}
	}
	return err
}
