package hosting_test

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/fakeapi"
)

func seedHosting(f *fakeapi.Server) fakeapi.Object {
	return f.SeedWebHosting(f.Scope(), nil)
}

// stateID returns the id of a resource in the state.
func stateID(state *terraform.State, name string) (string, error) {
	rs, ok := state.RootModule().Resources[name]
	if !ok {
		return "", fmt.Errorf("%s not in state", name)
	}
	return rs.Primary.ID, nil
}

// shortImportID returns "<hosting_id>/<id>" for a resource, the form used when the provider
// sets organization_id and project_id.
func shortImportID(name string) func(*terraform.State) (string, error) {
	return func(state *terraform.State) (string, error) {
		rs, ok := state.RootModule().Resources[name]
		if !ok {
			return "", fmt.Errorf("%s not in state", name)
		}
		return rs.Primary.Attributes["hosting_id"] + "/" + rs.Primary.ID, nil
	}
}

// checkChildren asserts the IDs of the objects of kind in the fake are exactly want.
func checkChildren(f *fakeapi.Server, kind fakeapi.Kind, want ...string) func(*terraform.State) error {
	return func(*terraform.State) error {
		var got []string
		for _, object := range f.All(kind) {
			got = append(got, object.ID())
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			return fmt.Errorf("%s in the API: got %v, want %v", kind, got, want)
		}
		return nil
	}
}

func terminateHosting(f *fakeapi.Server, hostingID string) {
	f.Mutate(fakeapi.KindWebHosting, hostingID, func(o fakeapi.Object) {
		o["status"] = "terminated"
		o["is_active"] = false
	})
}
