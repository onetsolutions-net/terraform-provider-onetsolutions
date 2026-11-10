package client

import (
	"context"
	"net/http"
)

// SSHKey is the SSHKey schema.
type SSHKey struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	PublicKey   string `json:"public_key"`
	Fingerprint string `json:"fingerprint"`
	IsActive    bool   `json:"is_active"`
	Created     string `json:"created"`
	Modified    string `json:"modified"`
}

// SSHKeyCreate is the body of create_ssh_key.
type SSHKeyCreate struct {
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
}

// VPC is the VPC schema.
type VPC struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CIDRBlock string `json:"cidr_block"`
	IsActive  bool   `json:"is_active"`
	Created   string `json:"created"`
	Modified  string `json:"modified"`
}

// VPCCreate is the body of create_vpc.
type VPCCreate struct {
	Name      string `json:"name"`
	CIDRBlock string `json:"cidr_block"`
}

// VPCUpdate is the body of partial_update_vpc; nil fields are left unchanged.
type VPCUpdate struct {
	Name *string `json:"name,omitempty"`
}

// CodeVPCInUse is the code of the error the API returns when deleting a VPC instances still use.
const CodeVPCInUse = "vpc_in_use"

const (
	sshKeysPath = "/v1/organizations/{organization_id}/projects/{project_id}/compute/ssh-keys"
	vpcsPath    = "/v1/organizations/{organization_id}/projects/{project_id}/compute/vpcs"
)

var (
	routeListSSHKeys  = newRoute(http.MethodGet, sshKeysPath)
	routeCreateSSHKey = newRoute(http.MethodPost, sshKeysPath)
	routeGetSSHKey    = newRoute(http.MethodGet, sshKeysPath+"/{id}")
	routeDeleteSSHKey = newRoute(http.MethodDelete, sshKeysPath+"/{id}")

	routeListVPCs  = newRoute(http.MethodGet, vpcsPath)
	routeCreateVPC = newRoute(http.MethodPost, vpcsPath)
	routeGetVPC    = newRoute(http.MethodGet, vpcsPath+"/{id}")
	routeUpdateVPC = newRoute(http.MethodPatch, vpcsPath+"/{id}")
	routeDeleteVPC = newRoute(http.MethodDelete, vpcsPath+"/{id}")
)

// ListSSHKeys lists the SSH keys of a project.
func (c *Client) ListSSHKeys(ctx context.Context, scope Scope) ([]SSHKey, error) {
	return listAll[SSHKey](ctx, c, routeListSSHKeys, scope.Params(), nil)
}

// CreateSSHKey registers a public key in a project.
func (c *Client) CreateSSHKey(ctx context.Context, scope Scope, in SSHKeyCreate) (*SSHKey, error) {
	var key SSHKey
	if err := c.do(ctx, routeCreateSSHKey, scope.Params(), in, &key); err != nil {
		return nil, err
	}
	return &key, nil
}

// GetSSHKey reads one SSH key.
func (c *Client) GetSSHKey(ctx context.Context, scope Scope, id string) (*SSHKey, error) {
	var key SSHKey
	if err := c.do(ctx, routeGetSSHKey, scope.Params("id", id), nil, &key); err != nil {
		return nil, err
	}
	return &key, nil
}

// DeleteSSHKey removes an SSH key.
func (c *Client) DeleteSSHKey(ctx context.Context, scope Scope, id string) error {
	return c.do(ctx, routeDeleteSSHKey, scope.Params("id", id), nil, nil)
}

// ListVPCs lists the VPCs of a project.
func (c *Client) ListVPCs(ctx context.Context, scope Scope) ([]VPC, error) {
	return listAll[VPC](ctx, c, routeListVPCs, scope.Params(), nil)
}

// CreateVPC creates a VPC.
func (c *Client) CreateVPC(ctx context.Context, scope Scope, in VPCCreate) (*VPC, error) {
	var vpc VPC
	if err := c.do(ctx, routeCreateVPC, scope.Params(), in, &vpc); err != nil {
		return nil, err
	}
	return &vpc, nil
}

// GetVPC reads one VPC.
func (c *Client) GetVPC(ctx context.Context, scope Scope, id string) (*VPC, error) {
	var vpc VPC
	if err := c.do(ctx, routeGetVPC, scope.Params("id", id), nil, &vpc); err != nil {
		return nil, err
	}
	return &vpc, nil
}

// UpdateVPC changes the fields of in that are set.
func (c *Client) UpdateVPC(ctx context.Context, scope Scope, id string, in VPCUpdate) (*VPC, error) {
	var vpc VPC
	if err := c.do(ctx, routeUpdateVPC, scope.Params("id", id), in, &vpc); err != nil {
		return nil, err
	}
	return &vpc, nil
}

// DeleteVPC deletes a VPC. The API refuses while instances are attached (CodeVPCInUse).
func (c *Client) DeleteVPC(ctx context.Context, scope Scope, id string) error {
	return c.do(ctx, routeDeleteVPC, scope.Params("id", id), nil, nil)
}
