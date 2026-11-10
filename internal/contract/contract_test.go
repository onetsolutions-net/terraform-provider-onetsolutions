package contract

import "testing"

func TestCheck(t *testing.T) {
	t.Parallel()
	published, err := SortedPublished()
	if err != nil {
		t.Fatal(err)
	}
	if len(published) < 150 {
		t.Fatalf("only %d operations parsed from the snapshot", len(published))
	}
	for _, op := range []Operation{
		{"GET", "/v1/compute/sizes"},
		{"post", "/v1/organizations/{organization_id}/projects/{project_id}/compute/vpcs"},
	} {
		if err := Check(op.Method, op.Path); err != nil {
			t.Errorf("Check(%s): %v", op, err)
		}
	}
	for _, op := range []Operation{
		{"PATCH", "/v1/organizations/{organization_id}/projects/{project_id}/compute/ssh-keys/{id}"},
		{"POST", "/v1/organizations/{organization_id}/projects"},
		{"GET", "/v1/compute/sizes/"},
	} {
		if err := Check(op.Method, op.Path); err == nil {
			t.Errorf("Check(%s) accepted an operation outside the contract", op)
		}
	}
}

func TestCheck_allowList(t *testing.T) {
	t.Parallel()
	published := map[Operation]struct{}{{"GET", "/v1/published"}: {}}
	allowList := []Operation{{"GET", "/v1/unpublished"}}
	for _, op := range []Operation{{"GET", "/v1/published"}, {"GET", "/v1/unpublished"}} {
		if err := check(op, published, allowList); err != nil {
			t.Errorf("check(%s): %v", op, err)
		}
	}
	if err := check(Operation{"POST", "/v1/unpublished"}, published, allowList); err == nil {
		t.Error("check accepted a method the allow-list does not name")
	}
}
