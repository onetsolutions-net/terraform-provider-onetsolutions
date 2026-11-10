package fakeapi

import (
	"net/http"
	"slices"
)

type taskState struct {
	kind       Kind
	polls      int
	failWith   string
	finished   bool
	onComplete func()
}

// FailNextTask makes the next task of taskType end "failed" with message in its result.
func (s *Server) FailNextTask(taskType, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failTasks[taskType] = append(s.failTasks[taskType], message)
}

// newInstanceTask queues a task on an instance, as an action endpoint of the API does, and
// returns it. onComplete (may be nil) runs once, when a read completes the task: that is where a
// handler applies the effect of the action (status, power state, renamed host…).
func (s *Server) newInstanceTask(scope Scope, instanceID, taskType string, parameters Object, onComplete func()) Object {
	instanceName := ""
	if instance, ok := s.lookupQuiet(KindInstance, scope, "", instanceID); ok {
		instanceName = instance.String("name")
	}
	task := s.insert(KindInstanceTask, scope, instanceID, Object{
		"instance_id":  instanceID,
		"instance":     Object{"id": instanceID, "name": instanceName},
		"task_type":    taskType,
		"status":       "queued",
		"parameters":   parameters,
		"result":       Object{},
		"progress":     0,
		"started_at":   nil,
		"completed_at": nil,
		"resolved_at":  nil,
		"created_by":   nil,
	})
	s.trackTask(KindInstanceTask, task.ID(), taskType, onComplete)
	return task
}

// newDomainTask queues a task on a domain; see newInstanceTask.
func (s *Server) newDomainTask(scope Scope, domainID, taskType string, onComplete func()) Object {
	task := s.insert(KindDomainTask, scope, domainID, Object{
		"task_type":    taskType,
		"status":       "pending",
		"progress":     0,
		"started_at":   nil,
		"completed_at": nil,
	})
	s.trackTask(KindDomainTask, task.ID(), taskType, onComplete)
	return task
}

func (s *Server) trackTask(kind Kind, id, taskType string, onComplete func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := &taskState{kind: kind, onComplete: onComplete}
	if queue := s.failTasks[taskType]; len(queue) > 0 {
		state.failWith = queue[0]
		s.failTasks[taskType] = queue[1:]
	}
	s.tasks[id] = state
}

// advanceTask is called on every read of a task: it runs, then completes (or fails) after
// taskPolls reads.
func (s *Server) advanceTask(id string) {
	s.mu.Lock()
	state, ok := s.tasks[id]
	if !ok || state.finished {
		s.mu.Unlock()
		return
	}
	rec, found := s.store.byID(state.kind, id)
	if !found {
		s.mu.Unlock()
		return
	}
	state.polls++
	stamp := now()
	if rec.data["started_at"] == nil {
		rec.data["started_at"] = stamp
	}
	var onComplete func()
	switch {
	case state.polls < s.taskPolls:
		rec.data["status"] = "running"
		rec.data["progress"] = 100 * state.polls / s.taskPolls
	case state.failWith != "":
		state.finished = true
		rec.data["status"] = "failed"
		rec.data["completed_at"] = stamp
		if state.kind == KindInstanceTask {
			rec.data["result"] = Object{"error": state.failWith}
		}
	default:
		state.finished = true
		rec.data["status"] = "completed"
		rec.data["progress"] = 100
		rec.data["completed_at"] = stamp
		onComplete = state.onComplete
	}
	rec.data["modified"] = stamp
	s.mu.Unlock()

	if onComplete != nil {
		onComplete()
	}
}

// lookupQuiet is lookup without counting a poll.
func (s *Server) lookupQuiet(kind Kind, scope Scope, parentID, id string) (Object, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.store.find(kind, scope, parentID, id)
	if !ok {
		return nil, false
	}
	return clone(rec.data), true
}

// newestFirst reverses a list in creation order, the order the API lists them in.
func newestFirst(items []Object) []Object {
	slices.Reverse(items)
	return items
}

func registerTasks(s *Server) {
	const instanceTasks = "/v1/organizations/{organization_id}/projects/{project_id}/compute/instances/{instance_id}/tasks"
	const domainTask = "/v1/organizations/{organization_id}/projects/{project_id}/domains/{id}/tasks/{task_id}"

	s.handle(http.MethodGet, instanceTasks, func(w http.ResponseWriter, r *http.Request) {
		scope, ok := s.projectScope(w, r)
		if !ok {
			return
		}
		instanceID := r.PathValue("instance_id")
		if _, ok := s.lookupQuiet(KindInstance, scope, "", instanceID); !ok {
			writeNotFound(w)
			return
		}
		writePage(w, r, newestFirst(s.list(KindInstanceTask, scope, instanceID)))
	})
	s.handle(http.MethodGet, instanceTasks+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		scope, ok := s.projectScope(w, r)
		if !ok {
			return
		}
		instanceID, id := r.PathValue("instance_id"), r.PathValue("id")
		if _, ok := s.lookupQuiet(KindInstanceTask, scope, instanceID, id); !ok {
			writeNotFound(w)
			return
		}
		s.advanceTask(id)
		task, _ := s.lookupQuiet(KindInstanceTask, scope, instanceID, id)
		writeJSON(w, http.StatusOK, task)
	})

	s.handle(http.MethodGet, domainTask, func(w http.ResponseWriter, r *http.Request) {
		scope, ok := s.projectScope(w, r)
		if !ok {
			return
		}
		domainID, id := r.PathValue("id"), r.PathValue("task_id")
		if _, ok := s.lookupQuiet(KindDomainTask, scope, domainID, id); !ok {
			writeNotFound(w)
			return
		}
		s.advanceTask(id)
		task, _ := s.lookupQuiet(KindDomainTask, scope, domainID, id)
		writeJSON(w, http.StatusOK, task)
	})
}
