package client

import (
	"context"
	"encoding/json"
	"net/http"
)

// TaskTypeAttachGPU is the type of the task that attaches a GPU.
const TaskTypeAttachGPU = "attach_gpu"

// GPU attachment statuses. An attachment is listed while pending, active or detaching; a detached
// or failed one is no longer listed.
const (
	GPUAttachmentPending   = "pending"
	GPUAttachmentActive    = "active"
	GPUAttachmentDetaching = "detaching"
)

// GPUSpecs are the characteristics of a GPU model.
type GPUSpecs struct {
	Model               string `json:"model"`
	Vendor              string `json:"vendor"`
	HourlyRate          string `json:"hourly_rate"`
	VRAMGB              *int64 `json:"vram_gb"`
	MemoryType          string `json:"memory_type"`
	MemoryBandwidthGBps *int64 `json:"memory_bandwidth_gbps"`
	Architecture        string `json:"architecture"`
	CUDACores           *int64 `json:"cuda_cores"`
	TensorCores         *int64 `json:"tensor_cores"`
	FP16TFLOPS          *int64 `json:"fp16_tflops"`
	FP8TFLOPS           *int64 `json:"fp8_tflops"`
	INT8TOPS            *int64 `json:"int8_tops"`
	TDPWatts            *int64 `json:"tdp_watts"`
	PCIe                string `json:"pcie"`
	FormFactor          string `json:"form_factor"`
}

// GPU is one physical GPU, as nested in an attachment.
type GPU struct {
	GPUSpecs
	ID     string `json:"id"`
	Status string `json:"status"`
}

// GPUAttachment is a GPU attached to an instance: a session billed while it lasts.
type GPUAttachment struct {
	ID            string  `json:"id"`
	GPU           *GPU    `json:"gpu"`
	Status        string  `json:"status"`
	AttachedAt    *string `json:"attached_at"`
	DetachedAt    *string `json:"detached_at"`
	BilledMinutes int64   `json:"billed_minutes"`
}

// Model returns the model of the attached GPU, empty when unknown.
func (a *GPUAttachment) Model() string {
	if a.GPU == nil {
		return ""
	}
	return a.GPU.Model
}

// GPUAttach is the body of attach_gpu.
type GPUAttach struct {
	Model string `json:"model"`
}

// GPUDetach is the body of detach_gpu.
type GPUDetach struct {
	AttachmentID string `json:"attachment_id"`
}

var (
	routeListAvailableGPUs  = newRoute(http.MethodGet, computeInstancesPath+"/{id}/gpus/available")
	routeListGPUAttachments = newRoute(http.MethodGet, computeInstancesPath+"/{id}/gpus/attachments")
	routeAttachGPU          = newRoute(http.MethodPost, computeInstancesPath+"/{id}/gpus/attach")
	routeDetachGPU          = newRoute(http.MethodPost, computeInstancesPath+"/{id}/gpus/detach")
)

// ListAvailableGPUs lists the GPU models that can be attached to an instance now, one entry per
// model.
func (c *Client) ListAvailableGPUs(ctx context.Context, scope Scope, instanceID string) ([]GPUSpecs, error) {
	return listAll[GPUSpecs](ctx, c, routeListAvailableGPUs, scope.Params("id", instanceID), nil)
}

// ListGPUAttachments lists the GPU attachments of an instance that are pending, active or
// detaching, newest first.
func (c *Client) ListGPUAttachments(ctx context.Context, scope Scope, instanceID string) ([]GPUAttachment, error) {
	return listAll[GPUAttachment](ctx, c, routeListGPUAttachments, scope.Params("id", instanceID), nil)
}

// AttachGPU attaches a GPU of a model to an instance and returns the task doing it. The
// attachment is billed: the request is never retried blindly.
func (c *Client) AttachGPU(ctx context.Context, scope Scope, instanceID string, in GPUAttach) (string, error) {
	var ref InstanceTaskRef
	if err := c.do(ctx, routeAttachGPU, scope.Params("id", instanceID), in, &ref); err != nil {
		return "", err
	}
	return ref.TaskID, nil
}

// DetachGPU detaches an active attachment, which ends its billing, and returns the task doing it.
func (c *Client) DetachGPU(ctx context.Context, scope Scope, instanceID, attachmentID string) (string, error) {
	var ref InstanceTaskRef
	if err := c.do(ctx, routeDetachGPU, scope.Params("id", instanceID), GPUDetach{AttachmentID: attachmentID}, &ref); err != nil {
		return "", err
	}
	return ref.TaskID, nil
}

// FindAttachGPUTask returns the ID of the task attaching the GPU of attachmentID to an instance,
// empty when there is none among the recent tasks (see ListRecentInstanceTasks).
func (c *Client) FindAttachGPUTask(ctx context.Context, scope Scope, instanceID, attachmentID string) (string, error) {
	tasks, err := listFirstPage[parameterizedTask](ctx, c, routeListInstanceTasks, scope.Params("instance_id", instanceID))
	if err != nil {
		return "", err
	}
	for _, task := range tasks {
		if task.TaskType != TaskTypeAttachGPU {
			continue
		}
		var parameters struct {
			AttachmentID string `json:"attachment_id"`
		}
		if json.Unmarshal(task.Parameters, &parameters) == nil && parameters.AttachmentID == attachmentID {
			return task.ID, nil
		}
	}
	return "", nil
}
