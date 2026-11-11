package fakeapi

import (
	"net/http"
	"slices"
	"strings"

	"github.com/google/uuid"
)

// GPU kinds: a GPU on the host of an instance, and an attachment of one to the instance.
const (
	KindGPU           Kind = "gpu"            // parent: the instance whose host holds it
	KindGPUAttachment Kind = "gpu_attachment" // parent: instance
)

// GPU task types.
const (
	TaskAttachGPU  = "attach_gpu"
	TaskDetachGPU  = "detach_gpu"
	TaskEnableIPv6 = "enable_ipv6"
)

// maxGPUsPerInstance is how many GPUs the API lets an instance hold at once.
const maxGPUsPerInstance = 1

var openGPUAttachmentStatuses = []string{"pending", "active", "detaching"}

// gpuSpecFields are the fields of a GPU model the API lists as available.
var gpuSpecFields = []string{
	"model", "vendor", "hourly_rate", "vram_gb", "memory_type", "memory_bandwidth_gbps", "architecture",
	"cuda_cores", "tensor_cores", "fp16_tflops", "fp8_tflops", "int8_tops", "tdp_watts", "pcie", "form_factor",
}

// registerGPU serves GPUs and IPv6 as the API does: the available GPUs are the models of the
// instance's host that no open attachment holds, one entry per model, as a bare list; the
// attachments listed are the pending, active and detaching ones, newest first, as a bare list;
// attach takes a model (400 gpu_model_unavailable when none is free, gpu_quota_exceeded beyond one
// per instance) and queues attach_gpu, whose completion makes the attachment active and whose
// failure ends it (no longer listed); both leave the instance running; detach takes an active attachment (400 otherwise), marks it
// detaching and queues detach_gpu, whose completion ends it. Both are refused while the instance
// is busy (409) or not active (403). The IPv6 activation queues enable_ipv6 (409
// ipv6_already_enabled once the instance has an IPv6 address), whose completion adds the primary
// IPv6 address.
func registerGPU(s *Server) {
	s.handle(http.MethodGet, computeInstancesTemplate+"/{id}/gpus/available", s.withScope(s.availableGPUs))
	s.handle(http.MethodGet, computeInstancesTemplate+"/{id}/gpus/attachments", s.withScope(s.gpuAttachments))
	s.handle(http.MethodPost, computeInstancesTemplate+"/{id}/gpus/attach", s.withScope(s.attachGPU))
	s.handle(http.MethodPost, computeInstancesTemplate+"/{id}/gpus/detach", s.withScope(s.detachGPU))
	s.handle(http.MethodPost, computeInstancesTemplate+"/{id}/ipv6", s.withScope(s.enableIPv6))
}

// SeedGPU adds a GPU to the host of an instance.
func (s *Server) SeedGPU(scope Scope, instanceID string, overrides Object) Object {
	return s.insert(KindGPU, scope, instanceID, merge(Object{
		"model":                 "NVIDIA L4",
		"vendor":                "nvidia",
		"status":                "available",
		"hourly_rate":           "0.7000",
		"vram_gb":               24,
		"memory_type":           "GDDR6",
		"memory_bandwidth_gbps": 300,
		"architecture":          "Ada Lovelace",
		"cuda_cores":            7424,
		"tensor_cores":          232,
		"fp16_tflops":           242,
		"fp8_tflops":            485,
		"int8_tops":             485,
		"tdp_watts":             72,
		"pcie":                  "PCIe 4.0 x16",
		"form_factor":           "Single slot, low profile",
	}, overrides))
}

// SeedGPUAttachment attaches a GPU of the instance's host to it, active, as if attached in the
// console.
func (s *Server) SeedGPUAttachment(scope Scope, instanceID, gpuID string) Object {
	gpu, ok := s.lookupQuiet(KindGPU, scope, instanceID, gpuID)
	if !ok {
		s.t.Fatalf("fakeapi: no GPU %s on instance %s", gpuID, instanceID)
	}
	return s.insert(KindGPUAttachment, scope, instanceID, Object{
		"gpu":            gpuOf(gpu, "active"),
		"status":         "active",
		"hostpci_slot":   "hostpci0",
		"attached_at":    now(),
		"detached_at":    nil,
		"billed_minutes": 1,
	})
}

func gpuOf(gpu Object, status string) Object {
	out := Object{"id": gpu.ID(), "status": status}
	for _, field := range gpuSpecFields {
		out[field] = gpu[field]
	}
	return out
}

// openAttachments returns the listed attachments of an instance, newest first, after ending the
// pending ones whose attach task failed, as the API does when the task gives up.
func (s *Server) openAttachments(scope Scope, instanceID string) []Object {
	failed := map[string]bool{}
	for _, task := range s.list(KindInstanceTask, scope, instanceID) {
		if task.String("task_type") == TaskAttachGPU && task.String("status") == "failed" {
			failed[task.Map("parameters").String("attachment_id")] = true
		}
	}
	var open []Object
	for _, attachment := range newestFirst(s.list(KindGPUAttachment, scope, instanceID)) {
		if failed[attachment.ID()] && attachment.String("status") == "pending" {
			s.remove(KindGPUAttachment, scope, instanceID, attachment.ID())
			continue
		}
		if slices.Contains(openGPUAttachmentStatuses, attachment.String("status")) {
			open = append(open, attachment)
		}
	}
	return open
}

// freeGPUs returns the GPUs of the instance's host no open attachment holds.
func (s *Server) freeGPUs(scope Scope, instanceID string) []Object {
	held := map[string]bool{}
	for _, attachment := range s.openAttachments(scope, instanceID) {
		held[attachment.Map("gpu").ID()] = true
	}
	var free []Object
	for _, gpu := range s.list(KindGPU, scope, instanceID) {
		if !held[gpu.ID()] && gpu.String("status") == "available" {
			free = append(free, gpu)
		}
	}
	slices.SortStableFunc(free, func(a, b Object) int { return strings.Compare(a.String("model"), b.String("model")) })
	return free
}

func (s *Server) availableGPUs(w http.ResponseWriter, r *http.Request, scope Scope) {
	instanceID := r.PathValue("id")
	if _, ok := s.lookupQuiet(KindInstance, scope, "", instanceID); !ok {
		writeNotFound(w)
		return
	}
	options := []Object{}
	seen := map[string]bool{}
	for _, gpu := range s.freeGPUs(scope, instanceID) {
		model := gpu.String("model")
		if seen[model] {
			continue
		}
		seen[model] = true
		option := Object{}
		for _, field := range gpuSpecFields {
			option[field] = gpu[field]
		}
		options = append(options, option)
	}
	writeJSON(w, http.StatusOK, options)
}

func (s *Server) gpuAttachments(w http.ResponseWriter, r *http.Request, scope Scope) {
	instanceID := r.PathValue("id")
	if _, ok := s.lookupQuiet(KindInstance, scope, "", instanceID); !ok {
		writeNotFound(w)
		return
	}
	attachments := s.openAttachments(scope, instanceID)
	if attachments == nil {
		attachments = []Object{}
	}
	writeJSON(w, http.StatusOK, attachments)
}

// gpuActionTarget resolves the instance of a GPU or IPv6 action, refusing it like the API when the
// instance is unknown (404), not active (403), or busy (409).
func (s *Server) gpuActionTarget(w http.ResponseWriter, scope Scope, instanceID string) (Object, bool) {
	instance, ok := s.lookupQuiet(KindInstance, scope, "", instanceID)
	if !ok {
		writeNotFound(w)
		return nil, false
	}
	if refuseInactive(w, instance) || s.refuseWhenBusy(w, scope, instanceID) {
		return nil, false
	}
	return instance, true
}

func (s *Server) attachGPU(w http.ResponseWriter, r *http.Request, scope Scope) {
	instanceID := r.PathValue("id")
	if _, ok := s.gpuActionTarget(w, scope, instanceID); !ok {
		return
	}
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	errs := fieldErrors{}
	model := errs.requireString(body, "model", 0)
	if len(errs) > 0 {
		writeFieldErrors(w, errs)
		return
	}
	var gpu Object
	for _, free := range s.freeGPUs(scope, instanceID) {
		if free.String("model") == model {
			gpu = free
			break
		}
	}
	if gpu == nil {
		writeError(w, http.StatusBadRequest, "No GPU of this model is available.", "gpu_model_unavailable")
		return
	}
	if len(s.openAttachments(scope, instanceID)) >= maxGPUsPerInstance {
		writeError(w, http.StatusBadRequest, "The instance already has the maximum number of GPUs.", "gpu_quota_exceeded")
		return
	}
	attachment := s.insert(KindGPUAttachment, scope, instanceID, Object{
		"gpu":            gpuOf(gpu, "reserved"),
		"status":         "pending",
		"hostpci_slot":   "hostpci0",
		"attached_at":    nil,
		"detached_at":    nil,
		"billed_minutes": 0,
	})
	task := s.newInstanceTask(scope, instanceID, TaskAttachGPU, Object{"attachment_id": attachment.ID()}, func() {
		_, _ = s.update(KindGPUAttachment, scope, instanceID, attachment.ID(), func(o Object) {
			o["status"] = "active"
			o["attached_at"] = now()
			o["billed_minutes"] = 1
			o.Map("gpu")["status"] = "active"
		})
		s.leaveRunning(scope, instanceID)
	})
	writeJSON(w, http.StatusAccepted, Object{"task_id": task.ID()})
}

func (s *Server) detachGPU(w http.ResponseWriter, r *http.Request, scope Scope) {
	instanceID := r.PathValue("id")
	if _, ok := s.gpuActionTarget(w, scope, instanceID); !ok {
		return
	}
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	attachmentID, _ := stringField(body, "attachment_id")
	if _, err := uuid.Parse(attachmentID); err != nil {
		writeFieldErrors(w, fieldErrors{"attachment_id": {"Must be a valid UUID."}})
		return
	}
	attachment, ok := s.lookupQuiet(KindGPUAttachment, scope, instanceID, attachmentID)
	if !ok {
		writeNotFound(w)
		return
	}
	if attachment.String("status") != "active" {
		writeError(w, http.StatusBadRequest, "This GPU attachment is not active.", "gpu_unavailable")
		return
	}
	_, _ = s.update(KindGPUAttachment, scope, instanceID, attachmentID, func(o Object) { o["status"] = "detaching" })
	task := s.newInstanceTask(scope, instanceID, TaskDetachGPU, Object{"attachment_id": attachmentID}, func() {
		s.remove(KindGPUAttachment, scope, instanceID, attachmentID)
		s.leaveRunning(scope, instanceID)
	})
	writeJSON(w, http.StatusAccepted, Object{"task_id": task.ID()})
}

// leaveRunning records the instance running, as a GPU attach or detach leaves it, even when it
// was stopped.
func (s *Server) leaveRunning(scope Scope, instanceID string) {
	_, _ = s.update(KindInstance, scope, "", instanceID, func(o Object) {
		o["state_sync"] = Object{"vm_status": "running", "last_synced_at": now()}
	})
	s.mu.Lock()
	delete(s.powerStatus, instanceID)
	s.mu.Unlock()
}

func (s *Server) enableIPv6(w http.ResponseWriter, r *http.Request, scope Scope) {
	instanceID := r.PathValue("id")
	instance, ok := s.gpuActionTarget(w, scope, instanceID)
	if !ok {
		return
	}
	if instance["primary_ipv6"] != nil {
		writeError(w, http.StatusConflict, "This instance already has an IPv6 block.", "ipv6_already_enabled")
		return
	}
	task := s.newInstanceTask(scope, instanceID, TaskEnableIPv6, Object{}, func() { s.assignIPv6(scope, instanceID) })
	writeJSON(w, http.StatusAccepted, Object{"task_id": task.ID()})
}

// assignIPv6 gives an instance its primary IPv6 address, as the enable_ipv6 task does.
func (s *Server) assignIPv6(scope Scope, instanceID string) {
	address := s.SeedIPAddress(scope, instanceID, Object{
		"address":       "2001:db8:" + instanceID[:4] + "::1",
		"ip_type":       "ipv6",
		"netmask":       "64",
		"gateway":       "2001:db8::1",
		"network_range": "2001:db8:" + instanceID[:4] + "::/64",
		"is_primary":    true,
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec, ok := s.store.find(KindInstance, scope, "", instanceID); ok {
		rec.data["primary_ipv6"] = address["address"]
		rec.data["ip_addresses"] = s.ipAddressesOfLocked(scope, instanceID)
		rec.data["modified"] = now()
	}
}
