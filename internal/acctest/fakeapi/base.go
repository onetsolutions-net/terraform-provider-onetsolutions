package fakeapi

import (
	"fmt"
	"net/http"
	"net/netip"
	"strings"

	"golang.org/x/crypto/ssh"
)

const (
	sshKeysTemplate = "/v1/organizations/{organization_id}/projects/{project_id}/compute/ssh-keys"
	vpcsTemplate    = "/v1/organizations/{organization_id}/projects/{project_id}/compute/vpcs"
)

var (
	sshKeyTypePrefixes = []string{"ssh-rsa ", "ssh-ed25519 ", "ssh-dss ", "ecdsa-sha2-"}
	vpcAllowedRanges   = []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("172.16.0.0/12"),
		netip.MustParsePrefix("192.168.0.0/16"),
	}
)

const vpcMinPrefixLength = 16

// registerBase serves SSH keys and VPCs, as the API does.
func registerBase(s *Server) {
	s.handle(http.MethodGet, sshKeysTemplate, s.withScope(func(w http.ResponseWriter, r *http.Request, scope Scope) {
		writePage(w, r, newestFirst(s.list(KindSSHKey, scope, "")))
	}))
	s.handle(http.MethodPost, sshKeysTemplate, s.withScope(s.createSSHKey))
	s.handle(http.MethodGet, sshKeysTemplate+"/{id}", s.withScope(func(w http.ResponseWriter, r *http.Request, scope Scope) {
		s.writeObject(w, KindSSHKey, scope, "", r.PathValue("id"))
	}))
	s.handle(http.MethodDelete, sshKeysTemplate+"/{id}", s.withScope(func(w http.ResponseWriter, r *http.Request, scope Scope) {
		s.deleteObject(w, KindSSHKey, scope, "", r.PathValue("id"))
	}))

	s.handle(http.MethodGet, vpcsTemplate, s.withScope(func(w http.ResponseWriter, r *http.Request, scope Scope) {
		writePage(w, r, newestFirst(s.list(KindVPC, scope, "")))
	}))
	s.handle(http.MethodPost, vpcsTemplate, s.withScope(s.createVPC))
	s.handle(http.MethodGet, vpcsTemplate+"/{id}", s.withScope(func(w http.ResponseWriter, r *http.Request, scope Scope) {
		s.writeObject(w, KindVPC, scope, "", r.PathValue("id"))
	}))
	s.handle(http.MethodPatch, vpcsTemplate+"/{id}", s.withScope(s.updateVPC))
	s.handle(http.MethodDelete, vpcsTemplate+"/{id}", s.withScope(s.deleteVPC))
	registerVPCIPAllocations(s)
}

func (s *Server) createSSHKey(w http.ResponseWriter, r *http.Request, scope Scope) {
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	errs := fieldErrors{}
	name := errs.requireString(body, "name", 255)
	publicKey := errs.requireString(body, "public_key", 0)
	var fingerprint string
	if publicKey != "" {
		key, err := parsePublicKey(publicKey)
		if err != nil {
			errs.add("public_key", "Invalid SSH public key format")
		} else {
			fingerprint = ssh.FingerprintSHA256(key)
		}
	}
	if name != "" && s.nameTaken(KindSSHKey, scope, name, "") {
		errs.add("name", fmt.Sprintf("SSH key with name '%s' already exists", name))
	}
	if len(errs) > 0 {
		writeFieldErrors(w, errs)
		return
	}
	writeJSON(w, http.StatusCreated, s.insert(KindSSHKey, scope, "", Object{
		"name":        name,
		"public_key":  publicKey,
		"fingerprint": fingerprint,
		"is_active":   true,
	}))
}

// parsePublicKey applies the API's checks: a supported type prefix, then a base64 blob whose
// declared type matches.
func parsePublicKey(value string) (ssh.PublicKey, error) {
	trimmed := strings.TrimSpace(value)
	supported := false
	for _, prefix := range sshKeyTypePrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			supported = true
		}
	}
	if !supported {
		return nil, fmt.Errorf("unsupported key type")
	}
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(trimmed))
	return key, err
}

func (s *Server) createVPC(w http.ResponseWriter, r *http.Request, scope Scope) {
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	errs := fieldErrors{}
	name := errs.requireString(body, "name", 255)
	cidr := errs.requireString(body, "cidr_block", 50)
	if cidr != "" {
		if message := checkVPCCIDR(cidr); message != "" {
			errs.add("cidr_block", message)
		}
	}
	if len(errs) == 0 && s.nameTaken(KindVPC, scope, name, "") {
		errs.add("name", fmt.Sprintf("VPC with name '%s' already exists in this project", name))
	}
	if len(errs) > 0 {
		writeFieldErrors(w, errs)
		return
	}
	writeJSON(w, http.StatusCreated, s.insert(KindVPC, scope, "", Object{
		"name":       name,
		"cidr_block": cidr,
		"is_active":  true,
	}))
}

func checkVPCCIDR(value string) string {
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		return "Invalid CIDR notation: " + value
	}
	if !prefix.Addr().Is4() {
		return "Only IPv4 CIDR blocks are supported"
	}
	if prefix.Bits() < vpcMinPrefixLength {
		return fmt.Sprintf("CIDR block must not be larger than /%d", vpcMinPrefixLength)
	}
	for _, allowed := range vpcAllowedRanges {
		if allowed.Bits() <= prefix.Bits() && allowed.Contains(prefix.Masked().Addr()) {
			return ""
		}
	}
	return "CIDR block must be inside a private range (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16)"
}

func (s *Server) updateVPC(w http.ResponseWriter, r *http.Request, scope Scope) {
	id := r.PathValue("id")
	current, ok := s.lookupQuiet(KindVPC, scope, "", id)
	if !ok {
		writeNotFound(w)
		return
	}
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	if cidr, present := stringField(body, "cidr_block"); present && cidr != current["cidr_block"] {
		writeJSON(w, http.StatusBadRequest, Object{"error": "Cannot change CIDR block of existing VPC"})
		return
	}
	errs := fieldErrors{}
	if _, present := body["name"]; present {
		name := errs.requireString(body, "name", 255)
		if name != "" && s.nameTaken(KindVPC, scope, name, id) {
			errs.add("name", fmt.Sprintf("VPC with name '%s' already exists in this project", name))
		}
	}
	if len(errs) > 0 {
		writeFieldErrors(w, errs)
		return
	}
	updated, found := s.update(KindVPC, scope, "", id, func(o Object) {
		if name, present := stringField(body, "name"); present {
			o["name"] = name
		}
	})
	if !found {
		writeNotFound(w)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// deleteVPC refuses while an active instance of the project uses the VPC, with the API's
// VPCInUseError (HTTP 400, code vpc_in_use).
func (s *Server) deleteVPC(w http.ResponseWriter, r *http.Request, scope Scope) {
	id := r.PathValue("id")
	if _, ok := s.lookupQuiet(KindVPC, scope, "", id); !ok {
		writeNotFound(w)
		return
	}
	for _, instance := range s.list(KindInstance, scope, "") {
		vpc, _ := instance["vpc"].(map[string]any)
		if vpc != nil && vpc["id"] == id && instance["is_active"] == true {
			writeError(w, http.StatusBadRequest, "Cannot delete VPC that is in use by instances", "vpc_in_use")
			return
		}
	}
	s.deleteObject(w, KindVPC, scope, "", id)
}
