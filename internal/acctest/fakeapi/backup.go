package fakeapi

import (
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// KindBackup is a backup of an instance (parent: instance).
const KindBackup Kind = "backup"

const backupsTemplate = "/v1/organizations/{organization_id}/projects/{project_id}/compute/instances/{instance_id}/backups"

// Task types of the backups and of the instance actions.
const (
	TaskCreateBackup      = "create_backup"
	TaskRestoreBackup     = "restore_backup"
	TaskRebootInstance    = "reboot_vps"
	TaskResetInstance     = "reset_vps"
	TaskReinstallInstance = "reinstall_os"
)

const (
	backupOptionRequiredDetail = "Backup option is not enabled for this instance"
	backupNotCompletedDetail   = "Cannot restore backup with status other than completed"
)

// registerBackup serves the backups of an instance and the reboot, reset and reinstall actions,
// as the API does: a backup needs the instance's backup option and an idle instance, its name is
// unique on the instance, it has no delete and stays listed with status "deleted" once its
// retention has passed; a restore needs a completed backup; the actions queue a task.
func registerBackup(s *Server) {
	s.handle(http.MethodGet, backupsTemplate, s.withInstance(func(w http.ResponseWriter, r *http.Request, scope Scope, instanceID string) {
		writePage(w, r, newestFirst(s.backupsOf(scope, instanceID)))
	}))
	s.handle(http.MethodPost, backupsTemplate, s.withInstance(s.createBackup))
	s.handle(http.MethodGet, backupsTemplate+"/{id}", s.withInstance(func(w http.ResponseWriter, r *http.Request, scope Scope, instanceID string) {
		backup, ok := s.backupOf(scope, instanceID, r.PathValue("id"))
		if !ok {
			writeNotFound(w)
			return
		}
		writeJSON(w, http.StatusOK, backup)
	}))
	s.handle(http.MethodPost, backupsTemplate+"/{id}/restore", s.withInstance(s.restoreBackup))

	s.handle(http.MethodPost, computeInstancesTemplate+"/{id}/reboot", s.withScope(s.instanceAction(TaskRebootInstance)))
	s.handle(http.MethodPost, computeInstancesTemplate+"/{id}/reset", s.withScope(s.instanceAction(TaskResetInstance)))
	s.handle(http.MethodPost, computeInstancesTemplate+"/{id}/reinstall", s.withScope(s.reinstallInstance))
}

// SeedBackup adds a completed backup to an instance.
func (s *Server) SeedBackup(scope Scope, instanceID string, overrides Object) Object {
	return s.insert(KindBackup, scope, instanceID, merge(Object{
		"name":           s.nextName("seeded-backup"),
		"status":         "completed",
		"size_gb":        "4.10",
		"retention_days": 7,
		"expires_at":     time.Now().UTC().AddDate(0, 0, 7).Format(time.RFC3339),
		"backup_type":    "manual",
		"location":       nil,
	}, overrides))
}

func (s *Server) createBackup(w http.ResponseWriter, r *http.Request, scope Scope, instanceID string) {
	instance, _ := s.lookupQuiet(KindInstance, scope, "", instanceID)
	if instance["backup_enabled"] != true {
		writeJSON(w, http.StatusForbidden, Object{"error": backupOptionRequiredDetail, "code": "backup_option_required"})
		return
	}
	if s.refuseWhenBusy(w, scope, instanceID) {
		return
	}
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	errs := fieldErrors{}
	name := errs.requireString(body, "name", 255)
	retention := 7
	if raw, present := body["retention_days"]; present {
		days, isNumber := raw.(float64)
		switch {
		case !isNumber || days != float64(int(days)):
			errs.add("retention_days", "A valid integer is required.")
		case days < 1:
			errs.add("retention_days", "Ensure this value is greater than or equal to 1.")
		case days > 7:
			errs.add("retention_days", "Ensure this value is less than or equal to 7.")
		default:
			retention = int(days)
		}
	}
	if len(errs) > 0 {
		writeFieldErrors(w, errs)
		return
	}
	for _, existing := range s.list(KindBackup, scope, instanceID) {
		if existing["name"] == name {
			writeFieldErrors(w, fieldErrors{"name": {"A backup with this name already exists for this instance."}})
			return
		}
	}
	backup := s.insert(KindBackup, scope, instanceID, Object{
		"name":           name,
		"status":         "pending",
		"size_gb":        nil,
		"retention_days": retention,
		"expires_at":     time.Now().UTC().AddDate(0, 0, retention).Format(time.RFC3339),
		"backup_type":    "manual",
		"location":       nil,
	})
	backupID := backup.ID()
	task := s.newInstanceTask(scope, instanceID, TaskCreateBackup,
		Object{"backup_id": backupID, "backup_name": name, "retention_days": retention},
		func() {
			s.update(KindBackup, scope, instanceID, backupID, func(o Object) {
				o["status"] = "completed"
				o["size_gb"] = "3.20"
			})
		})
	backup["task_id"] = task.ID()
	writeJSON(w, http.StatusCreated, backup)
}

// backupOf reads a backup and, while it is not complete, gives it the "failed" status once its
// create_backup task has failed, as the API's task does.
func (s *Server) backupOf(scope Scope, instanceID, id string) (Object, bool) {
	backup, ok := s.lookup(KindBackup, scope, instanceID, id)
	if !ok || (backup["status"] != "pending" && backup["status"] != "running") {
		return backup, ok
	}
	for _, task := range s.list(KindInstanceTask, scope, instanceID) {
		if task["task_type"] == TaskCreateBackup && task.Map("parameters").String("backup_id") == id && task["status"] == "failed" {
			return s.update(KindBackup, scope, instanceID, id, func(o Object) { o["status"] = "failed" })
		}
	}
	return backup, true
}

func (s *Server) backupsOf(scope Scope, instanceID string) []Object {
	listed := s.list(KindBackup, scope, instanceID)
	for i, backup := range listed {
		if current, ok := s.backupOf(scope, instanceID, backup.ID()); ok {
			listed[i] = current
		}
	}
	return listed
}

func (s *Server) restoreBackup(w http.ResponseWriter, r *http.Request, scope Scope, instanceID string) {
	if s.refuseWhenBusy(w, scope, instanceID) {
		return
	}
	id := r.PathValue("id")
	backup, ok := s.backupOf(scope, instanceID, id)
	if !ok {
		writeNotFound(w)
		return
	}
	if backup["status"] != "completed" {
		writeJSON(w, http.StatusBadRequest, Object{"error": backupNotCompletedDetail})
		return
	}
	body, ok := decodeOptionalBody(w, r)
	if !ok {
		return
	}
	if body["restore_as_new"] == true {
		writeFieldErrors(w, fieldErrors{"restore_as_new": {"Restoring a backup into a new instance is not supported."}})
		return
	}
	task := s.newInstanceTask(scope, instanceID, TaskRestoreBackup, Object{"backup_id": id}, func() {
		s.update(KindInstance, scope, "", instanceID, func(o Object) {
			o["state_sync"] = Object{"vm_status": "running", "last_synced_at": now()}
		})
	})
	writeJSON(w, http.StatusAccepted, Object{"task_id": task.ID()})
}

// decodeOptionalBody reads a JSON body that may be empty.
func decodeOptionalBody(w http.ResponseWriter, r *http.Request) (Object, bool) {
	if r.ContentLength == 0 {
		return Object{}, true
	}
	return decodeBody(w, r)
}

// instanceAction serves reboot and reset: queued as a task on an active, unlocked instance.
func (s *Server) instanceAction(taskType string) func(http.ResponseWriter, *http.Request, Scope) {
	return func(w http.ResponseWriter, r *http.Request, scope Scope) {
		id := r.PathValue("id")
		if _, ok := s.instanceForAction(w, scope, id, true); !ok {
			return
		}
		task := s.newInstanceTask(scope, id, taskType, Object{}, func() {
			s.update(KindInstance, scope, "", id, func(o Object) {
				o["state_sync"] = Object{"vm_status": "running", "last_synced_at": now()}
			})
		})
		writeJSON(w, http.StatusAccepted, Object{"task_id": task.ID()})
	}
}

// reinstallInstance mirrors reinstall: the image must be public or one of the project's, the SSH
// keys UUIDs; the instance's snapshots are deleted at once, and the task installs the image. The
// task's parameters are the request body, for tests to inspect.
func (s *Server) reinstallInstance(w http.ResponseWriter, r *http.Request, scope Scope) {
	id := r.PathValue("id")
	if _, ok := s.instanceForAction(w, scope, id, true); !ok {
		return
	}
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	errs := fieldErrors{}
	imageID := errs.requireString(body, "image_id", 0)
	if imageID != "" {
		if _, err := uuid.Parse(imageID); err != nil {
			errs.add("image_id", "Must be a valid UUID.")
		}
	}
	for _, raw := range anyList(body["ssh_key_ids"]) {
		keyID, _ := raw.(string)
		if _, err := uuid.Parse(keyID); err != nil {
			errs.add("ssh_key_ids", "Must be a valid UUID.")
		}
	}
	if len(errs) > 0 {
		writeFieldErrors(w, errs)
		return
	}
	image, found := s.lookupQuiet(KindPublicImage, Scope{}, "", imageID)
	if !found {
		if image, found = s.lookupQuiet(KindPrivateImage, scope, "", imageID); !found {
			writeError(w, http.StatusNotFound, "Image not found.", "image_not_found")
			return
		}
	}
	for _, snapshot := range s.list(KindSnapshot, scope, id) {
		s.remove(KindSnapshot, scope, id, snapshot.ID())
	}
	task := s.newInstanceTask(scope, id, TaskReinstallInstance, body, func() {
		s.update(KindInstance, scope, "", id, func(o Object) {
			o["image"] = image
			o["os_name"] = fmt.Sprintf("%s %s", image.String("os_name"), image.String("os_version"))
			o["password_reset_enabled"] = body["allow_password_reset"] != false
			o["state_sync"] = Object{"vm_status": "running", "last_synced_at": now()}
		})
	})
	writeJSON(w, http.StatusAccepted, Object{"task_id": task.ID()})
}
