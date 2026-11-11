package fakeapi

import (
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// unorderableBillingCycle is the billing cycle the API refuses to order.
const unorderableBillingCycle = "hourly"

const computeInstancesTemplate = "/v1/organizations/{organization_id}/projects/{project_id}/compute/instances"

// Instance task types the compute handlers queue.
const (
	TaskCreateInstance = "create_instance"
	TaskStartInstance  = "start_instance"
	TaskStopInstance   = "stop_instance"
	TaskChangeHostname = "change_hostname"
	TaskUpdateVPC      = "update_vpc"
)

var (
	billingCycles          = []string{"hourly", "monthly", "quarterly", "semi_annually", "annually", "biennially", "triennially"}
	orderableBillingCycles = billingCycles[1:]
	hostnamePattern        = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$`)
)

// registerCompute serves the instances as the current API does: the
// create answers with the order (not the instance), whose local_order_id the instance carries as
// order_id and list_instances filters on (?order_id=, ignored with the field under
// WithoutOrderLink); the instance is provisioned after a few reads; hourly cannot be ordered; the
// order installs the name, size, image and installation options but not vpc_id, tags
// or volume_id (the API does not apply them to an order); an unpaid order (WithUnpaidOrders)
// creates no instance until PayOrder; power actions are checked against the hidden power_status
// (409 instance_already_running/stopped) and, like name/VPC changes, queue tasks; a resize is an
// upgrade order to another, eligible size, applied once paid (here: at once, as if paid from the
// account credit, unless WithUnpaidResizes). DELETE requests the cancellation at the end of the
// billing period (202 with the instance, idempotent, never refused while locked); the instance
// stays listed and active, and revoke-cancellation withdraws the request. The create_instance
// task fails like any other through FailNextTask: the instance then ends in status "error".
func registerCompute(s *Server) {
	s.handle(http.MethodGet, computeInstancesTemplate, s.withScope(s.listComputeInstances))
	s.handle(http.MethodPost, computeInstancesTemplate, s.withScope(s.createComputeInstance))
	s.handle(http.MethodPost, computeInstancesTemplate+"/order-preview", s.withScope(s.previewComputeInstanceOrder))
	s.handle(http.MethodGet, computeInstancesTemplate+"/{id}", s.withScope(func(w http.ResponseWriter, r *http.Request, scope Scope) {
		s.writeObject(w, KindInstance, scope, "", r.PathValue("id"))
	}))
	s.handle(http.MethodPatch, computeInstancesTemplate+"/{id}", s.withScope(s.updateComputeInstance))
	s.handle(http.MethodDelete, computeInstancesTemplate+"/{id}", s.withScope(s.deleteComputeInstance))
	s.handle(http.MethodPost, computeInstancesTemplate+"/{id}/revoke-cancellation", s.withScope(s.revokeInstanceCancellation))
	s.handle(http.MethodPost, computeInstancesTemplate+"/{id}/start", s.withScope(s.powerAction(TaskStartInstance, "running")))
	s.handle(http.MethodPost, computeInstancesTemplate+"/{id}/stop", s.withScope(s.powerAction(TaskStopInstance, "stopped")))
	s.handle(http.MethodPost, computeInstancesTemplate+"/{id}/resize", s.withScope(s.resizeComputeInstance))
	s.handle(http.MethodPost, computeInstancesTemplate+"/{id}/resize-preview", s.withScope(s.previewComputeInstanceResize))
	s.handle(http.MethodGet, computeInstancesTemplate+"/{id}/eligible-sizes", s.withScope(s.eligibleComputeInstanceSizes))
	registerGPU(s)
}

func (s *Server) listComputeInstances(w http.ResponseWriter, r *http.Request, scope Scope) {
	activeFilter := r.URL.Query().Get("is_active")
	var statuses []string
	if raw := r.URL.Query().Get("status"); raw != "" {
		statuses = strings.Split(raw, ",")
	}
	orderID := strings.TrimSpace(r.URL.Query().Get("order_id"))
	if s.withoutOrderLink {
		orderID = ""
	} else if _, err := uuid.Parse(orderID); orderID != "" && err != nil {
		writeFieldErrors(w, fieldErrors{"order_id": {"Must be a valid UUID."}})
		return
	}
	var kept []Object
	for _, instance := range newestFirst(s.list(KindInstance, scope, "")) {
		active := instance["is_active"] == true
		if (activeFilter == "" || activeFilter == "true") && !active || activeFilter == "false" && active {
			continue
		}
		if len(statuses) > 0 && !slices.Contains(statuses, instance.String("status")) {
			continue
		}
		if orderID != "" && instance.String("order_id") != orderID {
			continue
		}
		kept = append(kept, instance)
	}
	writePage(w, r, kept)
}

// instanceOrder validates an InstanceCreate / InstanceOrderPreview body and resolves what it
// references. requireName is false for the preview, where the name is optional.
func (s *Server) instanceOrder(w http.ResponseWriter, r *http.Request, scope Scope, requireName bool) (body, size, image Object, ok bool) {
	body, ok = decodeBody(w, r)
	if !ok {
		return nil, nil, nil, false
	}
	errs := fieldErrors{}
	if name, present := stringField(body, "name"); requireName || present && name != "" {
		name = errs.requireString(body, "name", 253)
		if name != "" && !hostnamePattern.MatchString(name) {
			errs.add("name", "Enter a valid hostname.")
		}
	}
	sizeID := errs.requireString(body, "size_id", 0)
	imageID := errs.requireString(body, "image_id", 0)
	cycle := errs.requireString(body, "billing_cycle", 0)
	if cycle != "" && !slices.Contains(billingCycles, cycle) {
		errs.add("billing_cycle", fmt.Sprintf("%q is not a valid choice.", cycle))
	}
	if sizeID != "" {
		if size, ok = s.lookupQuiet(KindSize, Scope{}, "", sizeID); !ok {
			errs.add("size_id", "Instance size not found.")
		}
	}
	if imageID != "" {
		if image, ok = s.lookupQuiet(KindPublicImage, Scope{}, "", imageID); !ok {
			if image, ok = s.lookupQuiet(KindPrivateImage, scope, "", imageID); !ok {
				errs.add("image_id", "Image not found.")
			}
		}
	}
	if vpcID, present := stringField(body, "vpc_id"); present && vpcID != "" {
		if _, found := s.lookupQuiet(KindVPC, scope, "", vpcID); !found {
			errs.add("vpc_id", "VPC not found.")
		}
	}
	for _, raw := range anyList(body["ssh_keys"]) {
		keyID, _ := raw.(string)
		if _, found := s.lookupQuiet(KindSSHKey, scope, "", keyID); !found {
			errs.add("ssh_keys", fmt.Sprintf("SSH key %s not found.", keyID))
		}
	}
	if len(errs) > 0 {
		writeFieldErrors(w, errs)
		return nil, nil, nil, false
	}
	if cycle == unorderableBillingCycle {
		writeError(w, http.StatusBadRequest, "This billing cycle cannot be ordered: choose monthly or a longer cycle.",
			"billing_cycle_not_orderable")
		return nil, nil, nil, false
	}
	return body, size, image, true
}

// unpaidOrder is an order WithUnpaidOrders left awaiting payment: PayOrder creates its instance.
type unpaidOrder struct {
	scope             Scope
	body, size, image Object
}

func (s *Server) createComputeInstance(w http.ResponseWriter, r *http.Request, scope Scope) {
	body, size, image, ok := s.instanceOrder(w, r, scope, true)
	if !ok {
		return
	}
	cycle := body.String("billing_cycle")
	amount := sizePrice(size, cycle)
	localOrderID := uuid.NewString()
	if s.unpaidOrders {
		s.mu.Lock()
		s.unpaidInstanceRuns[localOrderID] = unpaidOrder{scope: scope, body: body, size: size, image: image}
		s.mu.Unlock()
		writeJSON(w, http.StatusCreated, Object{
			"order_id":            9000,
			"order_number":        "9000000",
			"invoice_id":          900001,
			"local_order_id":      localOrderID,
			"total":               priceFloat(amount),
			"amount_due":          priceFloat(amount) * 1.2,
			"credit_applied":      0.0,
			"status":              "pending_payment",
			"invoice_payment_url": "https://console.onetsolutions.net/billing/invoices/900001",
		})
		return
	}
	s.provisionOrdered(scope, localOrderID, body, size, image)
	writeJSON(w, http.StatusCreated, Object{
		"order_id":       len(s.All(KindInstance)) + 1000,
		"order_number":   strconv.Itoa(len(s.All(KindInstance)) + 5000),
		"invoice_id":     nil,
		"local_order_id": localOrderID,
		"total":          priceFloat(amount),
		"amount_due":     0.0,
		"credit_applied": priceFloat(amount),
		"status":         "Active",
	})
}

// PayOrder pays an order WithUnpaidOrders left unpaid, as the console does: its instance is
// created and installed after the usual polls. It returns the new instance.
func (s *Server) PayOrder(localOrderID string) Object {
	s.mu.Lock()
	order, ok := s.unpaidInstanceRuns[localOrderID]
	delete(s.unpaidInstanceRuns, localOrderID)
	s.mu.Unlock()
	if !ok {
		s.t.Fatalf("fakeapi: no unpaid order %s", localOrderID)
	}
	return s.provisionOrdered(order.scope, localOrderID, order.body, order.size, order.image)
}

// UnpaidOrders returns the local ids of the orders awaiting payment.
func (s *Server) UnpaidOrders() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.unpaidInstanceRuns))
	for id := range s.unpaidInstanceRuns {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// provisionOrdered creates the instance of a paid order, as the API does, and
// installs it after the configured task polls.
func (s *Server) provisionOrdered(scope Scope, localOrderID string, body, size, image Object) Object {
	cycle := body.String("billing_cycle")
	instance := s.SeedInstance(scope, Object{
		"order_id":               localOrderID,
		"name":                   body.String("name"),
		"status":                 "pending",
		"is_installed":           false,
		"size":                   size,
		"image":                  image,
		"os_name":                image["os_name"],
		"billing_cycle":          cycle,
		"recurring_amount":       sizePrice(size, cycle),
		"password_reset_enabled": body["allow_password_reset"] != false,
		"state_sync":             Object{"vm_status": "stopped", "last_synced_at": now()},
	})
	id := instance.ID()

	task := s.newInstanceTask(scope, id, TaskCreateInstance, Object{}, nil)
	s.mu.Lock()
	failure := s.tasks[task.ID()].failWith
	s.mu.Unlock()
	s.afterPolls(KindInstance, id, s.taskPolls, func(o Object) {
		if failure != "" {
			o["status"] = "error"
			return
		}
		o["status"] = "active"
		o["is_installed"] = true
		switch {
		case s.powerSyncDelay > 0:
			o["state_sync"] = nil
		case !s.installedStopped:
			o["state_sync"] = Object{"vm_status": "running", "last_synced_at": now()}
		}
		o["next_billing_date"] = now()
	})
	if s.powerSyncDelay > 0 && failure == "" {
		s.afterPolls(KindInstance, id, s.taskPolls+s.powerSyncDelay, func(o Object) {
			o["state_sync"] = Object{"vm_status": "running", "last_synced_at": now()}
		})
	}
	return instance
}

func (s *Server) previewComputeInstanceOrder(w http.ResponseWriter, r *http.Request, scope Scope) {
	body, size, _, ok := s.instanceOrder(w, r, scope, false)
	if !ok {
		return
	}
	total := priceFloat(sizePrice(size, body.String("billing_cycle")))
	writeJSON(w, http.StatusOK, Object{
		"subtotal":          total,
		"setup_total":       0.0,
		"discount":          0.0,
		"total":             total,
		"tax_rate":          20.0,
		"tax_amount":        total * 0.2,
		"total_incl_tax":    total * 1.2,
		"is_reverse_charge": false,
		"is_tax_known":      true,
		"available_credit":  0.0,
		"credit_to_apply":   0.0,
		"amount_due":        total * 1.2,
		"currency":          "EUR",
		"item_count":        1,
		"contracts":         []any{},
	})
}

// busy answers 409 like refuse_when_busy and InstanceMustBeUnlocked do.
func busy(w http.ResponseWriter, instance Object) bool {
	if instance["locked"] == true {
		writeError(w, http.StatusConflict, "Instance is locked by an operation in progress.", "instance_locked")
		return true
	}
	return false
}

func (s *Server) instanceForAction(w http.ResponseWriter, scope Scope, id string, requireActive bool) (Object, bool) {
	instance, ok := s.lookup(KindInstance, scope, "", id)
	if !ok {
		writeNotFound(w)
		return nil, false
	}
	if busy(w, instance) {
		return nil, false
	}
	if requireActive && refuseInactive(w, instance) {
		return nil, false
	}
	return instance, true
}

func (s *Server) updateComputeInstance(w http.ResponseWriter, r *http.Request, scope Scope) {
	id := r.PathValue("id")
	current, ok := s.instanceForAction(w, scope, id, false)
	if !ok {
		return
	}
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	errs := fieldErrors{}
	if _, present := body["name"]; present {
		if name := errs.requireString(body, "name", 253); name != "" && !hostnamePattern.MatchString(name) {
			errs.add("name", "Enter a valid hostname.")
		}
	}
	var vpc any
	vpcChange := false
	if raw, present := body["vpc_id"]; present {
		currentVPC := current.Map("vpc")
		vpcID, _ := raw.(string)
		switch {
		case raw == nil:
			vpcChange = currentVPC != nil
		case vpcID != "":
			found, exists := s.lookupQuiet(KindVPC, scope, "", vpcID)
			if !exists {
				errs.add("vpc_id", "VPC not found.")
			}
			vpc = found
			vpcChange = currentVPC == nil || currentVPC.ID() != vpcID
		}
	}
	if len(errs) > 0 {
		writeFieldErrors(w, errs)
		return
	}
	nameChange := false
	updated, _ := s.update(KindInstance, scope, "", id, func(o Object) {
		if name, present := stringField(body, "name"); present && name != o.String("name") {
			o["name"] = name
			nameChange = true
		}
		if _, present := body["tags"]; present {
			o["tags"] = stringsOrEmpty(body["tags"])
		}
		if vpcChange {
			o["vpc"] = vpc
		}
	})
	if nameChange {
		s.newInstanceTask(scope, id, TaskChangeHostname, Object{"hostname": updated.String("name")}, nil)
	}
	if vpcChange {
		s.newInstanceTask(scope, id, TaskUpdateVPC, Object{}, nil)
	}
	writeJSON(w, http.StatusOK, updated)
}

// cancellationReasonCodes are the cancellation reason codes the API accepts.
var cancellationReasonCodes = []string{"too_expensive", "no_longer_needed", "missing_feature", "technical_issue", "switching_provider", "other"}

// cancellationReasonWord counts words as the API does when it checks a cancellation reason.
var cancellationReasonWord = regexp.MustCompile(`[\p{L}\p{N}]+`)

// DeleteInstanceDefaultReason is the reason the API records for a DELETE sent without one.
const DeleteInstanceDefaultReason = "Cancellation requested through the API delete endpoint."

// deleteComputeInstance mirrors DELETE on an instance: the optional body is validated as the API
// validates it, then the cancellation is requested at the end of the billing period
// unless it already is or the instance has ended; every success is 202 with the instance.
func (s *Server) deleteComputeInstance(w http.ResponseWriter, r *http.Request, scope Scope) {
	id := r.PathValue("id")
	if _, ok := s.lookup(KindInstance, scope, "", id); !ok {
		writeNotFound(w)
		return
	}
	body := Object{}
	if r.ContentLength != 0 {
		decoded, ok := decodeBody(w, r)
		if !ok {
			return
		}
		body = decoded
	}
	errs := fieldErrors{}
	code := "no_longer_needed"
	if raw, present := body["reason_code"]; present {
		code, _ = raw.(string)
		if !slices.Contains(cancellationReasonCodes, code) {
			errs.add("reason_code", fmt.Sprintf("%q is not a valid choice.", code))
		}
	}
	reason := DeleteInstanceDefaultReason
	if _, present := body["reason"]; present {
		if reason = errs.requireString(body, "reason", 1000); reason != "" && len(cancellationReasonWord.FindAllString(reason, -1)) < 7 {
			errs.add("reason", "Tell us why in at least 7 words, so we can act on it.")
		}
	}
	if len(errs) > 0 {
		writeFieldErrors(w, errs)
		return
	}
	updated, _ := s.update(KindInstance, scope, "", id, func(o Object) {
		ended := o["is_active"] != true || o.String("status") == "terminated" || o.String("status") == "cancelled"
		if o["cancellation_requested_at"] != nil || ended {
			return
		}
		o["cancellation_requested_at"] = now()
		o["cancellation_reason"] = reason
		o["cancellation_reason_code"] = code
		o["billing_end_date"] = o["next_billing_date"]
		if o["billing_end_date"] == nil {
			o["billing_end_date"] = now()
		}
	})
	writeJSON(w, http.StatusAccepted, updated)
}

// revokeInstanceCancellation mirrors revoke_cancellation: 400 no_cancellation_requested when
// there is none, otherwise the request is withdrawn and the instance answered.
func (s *Server) revokeInstanceCancellation(w http.ResponseWriter, r *http.Request, scope Scope) {
	id := r.PathValue("id")
	instance, ok := s.lookup(KindInstance, scope, "", id)
	if !ok {
		writeNotFound(w)
		return
	}
	if instance["cancellation_requested_at"] == nil {
		writeError(w, http.StatusBadRequest, "No cancellation request to revoke", "no_cancellation_requested")
		return
	}
	updated, _ := s.update(KindInstance, scope, "", id, func(o Object) {
		o["cancellation_requested_at"] = nil
		o["cancellation_reason"] = ""
		o["cancellation_reason_code"] = ""
		o["billing_end_date"] = nil
	})
	writeJSON(w, http.StatusOK, updated)
}

// powerAction mirrors LifecycleMixin.start_instance/stop_instance: the action is refused with 409
// instance_already_running/stopped when the instance's power_status already is the target,
// whatever state_sync reads; the task then records the target in both.
func (s *Server) powerAction(taskType, to string) func(http.ResponseWriter, *http.Request, Scope) {
	return func(w http.ResponseWriter, r *http.Request, scope Scope) {
		id := r.PathValue("id")
		instance, ok := s.instanceForAction(w, scope, id, true)
		if !ok {
			return
		}
		defer s.catchUpSync(id)
		if s.powerStatusOf(instance) == to {
			writeError(w, http.StatusConflict, "The instance is already "+to+".", "instance_already_"+to)
			return
		}
		task := s.newInstanceTask(scope, id, taskType, Object{}, func() {
			_, _ = s.update(KindInstance, scope, "", id, func(o Object) {
				o["state_sync"] = Object{"vm_status": to, "last_synced_at": now()}
				delete(s.powerStatus, id)
			})
		})
		writeJSON(w, http.StatusAccepted, Object{"task_id": task.ID()})
	}
}

// powerStatusOf is the instance's power_status: what the last power task or sync recorded, which
// follows state_sync (running, or stopped for anything else) unless DesyncPowerState set it apart.
func (s *Server) powerStatusOf(instance Object) string {
	s.mu.Lock()
	status, set := s.powerStatus[instance.ID()]
	s.mu.Unlock()
	if set {
		return status
	}
	if instance.Map("state_sync").String("vm_status") == "running" {
		return "running"
	}
	return "stopped"
}

// DesyncPowerState makes state_sync read staleVMStatus while the instance's power_status keeps
// what it was, as between a power task and the next sync. The sync catches up polls reads of the
// instance after the next power action on it, refused or not.
func (s *Server) DesyncPowerState(id, staleVMStatus string, polls int) {
	instance, ok := s.Get(KindInstance, id)
	if !ok {
		s.t.Fatalf("fakeapi: no instance %s", id)
	}
	actual := s.powerStatusOf(instance)
	s.Mutate(KindInstance, id, func(o Object) {
		o["state_sync"] = Object{"vm_status": staleVMStatus, "last_synced_at": now()}
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	s.powerStatus[id] = actual
	s.syncCatchUp[id] = polls
}

// catchUpSync schedules the sync DesyncPowerState left pending, if any.
func (s *Server) catchUpSync(id string) {
	s.mu.Lock()
	polls, pending := s.syncCatchUp[id]
	delete(s.syncCatchUp, id)
	actual := s.powerStatus[id]
	s.mu.Unlock()
	if !pending {
		return
	}
	s.afterPolls(KindInstance, id, polls, func(o Object) {
		o["state_sync"] = Object{"vm_status": actual, "last_synced_at": now()}
		delete(s.powerStatus, id)
	})
}

// refuseInactive answers what the API answers on a change to an instance that is not
// active (cancelled, terminated, suspended, still provisioning): 403 permission_denied.
func refuseInactive(w http.ResponseWriter, instance Object) bool {
	if instance["is_active"] == true && instance.String("status") == "active" {
		return false
	}
	writeJSON(w, http.StatusForbidden, Object{"detail": inactiveInstanceDetail, "code": "permission_denied"})
	return true
}

const inactiveInstanceDetail = "This instance is not active. Wait for provisioning to complete before performing actions on it."

// eligibleSizes mirrors eligible_upgrade_sizes: the active sizes with at least as much memory,
// the current one excluded.
func (s *Server) eligibleSizes(instance Object) []Object {
	current := instance.Map("size")
	currentRAM, _ := current["ram_mb"].(float64)
	var eligible []Object
	for _, size := range s.list(KindSize, Scope{}, "") {
		ram, _ := size["ram_mb"].(float64)
		if size.ID() != current.ID() && size["is_active"] == true && ram >= currentRAM {
			eligible = append(eligible, size)
		}
	}
	return eligible
}

func (s *Server) eligibleComputeInstanceSizes(w http.ResponseWriter, r *http.Request, scope Scope) {
	instance, ok := s.lookup(KindInstance, scope, "", r.PathValue("id"))
	if !ok {
		writeNotFound(w)
		return
	}
	eligible := s.eligibleSizes(instance)
	if eligible == nil {
		eligible = []Object{}
	}
	writeJSON(w, http.StatusOK, eligible)
}

// resizeTarget validates an InstanceResize body like operations_mixin.resize: the same size is
// refused (SAME_SIZE), whatever the billing cycle.
func (s *Server) resizeTarget(w http.ResponseWriter, r *http.Request, instance Object) (size Object, cycle string, ok bool) {
	body, ok := decodeBody(w, r)
	if !ok {
		return nil, "", false
	}
	errs := fieldErrors{}
	sizeID := errs.requireString(body, "size_id", 0)
	cycle, _ = stringField(body, "billing_cycle")
	if cycle != "" && !slices.Contains(billingCycles, cycle) {
		errs.add("billing_cycle", fmt.Sprintf("%q is not a valid choice.", cycle))
	}
	if len(errs) > 0 {
		writeFieldErrors(w, errs)
		return nil, "", false
	}
	if sizeID == instance.Map("size").ID() {
		writeError(w, http.StatusBadRequest, "New size is the same as current size", "same_size")
		return nil, "", false
	}
	for _, eligible := range s.eligibleSizes(instance) {
		if eligible.ID() == sizeID {
			return eligible, cycle, true
		}
	}
	writeError(w, http.StatusBadRequest, "This size is not an eligible upgrade for the instance.", "size_not_eligible")
	return nil, "", false
}

func (s *Server) resizeComputeInstance(w http.ResponseWriter, r *http.Request, scope Scope) {
	id := r.PathValue("id")
	instance, ok := s.instanceForAction(w, scope, id, true)
	if !ok {
		return
	}
	size, cycle, ok := s.resizeTarget(w, r, instance)
	if !ok {
		return
	}
	s.mu.Lock()
	awaitingPayment := s.pendingResizes[id]
	if s.unpaidResizes {
		s.pendingResizes[id] = true
	}
	s.mu.Unlock()
	if awaitingPayment {
		writeError(w, http.StatusConflict, "A previous plan change of this service is awaiting payment. Pay its payment "+
			"notice before ordering another one.", "upgrade_awaiting_payment")
		return
	}
	if cycle == "" {
		cycle = instance.String("billing_cycle")
	}
	amount := sizePrice(size, cycle)
	if !s.unpaidResizes {
		s.afterPolls(KindInstance, id, 1, func(o Object) {
			o["size"] = size
			o["billing_cycle"] = cycle
			o["recurring_amount"] = amount
		})
	}
	writeJSON(w, http.StatusAccepted, Object{
		"order_id":    2000,
		"invoice_id":  nil,
		"invoice_url": nil,
		"message":     "Upgrade order created. Please pay the invoice to complete the resize.",
	})
}

func (s *Server) previewComputeInstanceResize(w http.ResponseWriter, r *http.Request, scope Scope) {
	instance, ok := s.lookup(KindInstance, scope, "", r.PathValue("id"))
	if !ok {
		writeNotFound(w)
		return
	}
	size, cycle, ok := s.resizeTarget(w, r, instance)
	if !ok {
		return
	}
	if cycle == "" {
		cycle = instance.String("billing_cycle")
	}
	price := sizePrice(size, cycle)
	writeJSON(w, http.StatusOK, Object{
		"product_price":                price,
		"config_options_price":         "0.00",
		"total_price":                  price,
		"currency":                     "EUR",
		"billing_cycle":                cycle,
		"available_billing_cycles":     orderableBillingCycles,
		"recurring_price":              price,
		"recurring_monthly_equivalent": price,
		"monthly_reference_price":      price,
		"monthly_savings_percent":      "0.00",
		"kept_options":                 []any{},
		"dropped_options":              []any{},
	})
}

// sizePrice returns the price of a size on a billing cycle, "0.00" when the size has none.
func sizePrice(size Object, cycle string) string {
	for _, raw := range size.List("pricing_options") {
		option, _ := raw.(map[string]any)
		if option["billing_cycle"] == cycle {
			if price, ok := option["price"].(string); ok {
				return price
			}
		}
	}
	return "0.00"
}

func priceFloat(price string) float64 {
	value, err := strconv.ParseFloat(price, 64)
	if err != nil {
		return 0
	}
	return value
}

func anyList(raw any) []any {
	list, _ := raw.([]any)
	return list
}

func stringsOrEmpty(raw any) []any {
	list := anyList(raw)
	if list == nil {
		return []any{}
	}
	return list
}
