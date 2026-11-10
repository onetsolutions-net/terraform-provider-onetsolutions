package fakeapi

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/google/uuid"
)

// Kind names one object type of the fake.
type Kind string

// Every object type of the plan. Global kinds (catalog, organizations) use an empty Scope;
// projects use Scope{OrganizationID}; child kinds carry their parent's ID.
const (
	KindOrganization Kind = "organization"
	KindProject      Kind = "project"
	KindSize         Kind = "size"
	KindPublicImage  Kind = "public_image"
	KindPrivateImage Kind = "private_image"
	KindControlPanel Kind = "control_panel"
	KindHostingPlan  Kind = "hosting_plan"
	KindWebHosting   Kind = "web_hosting"
	KindInstance     Kind = "instance"
	KindInstanceTask Kind = "instance_task" // parent: instance
	KindIPAddress    Kind = "ip_address"    // parent: instance
	KindFirewallRule Kind = "firewall_rule" // parent: instance
	KindSnapshot     Kind = "snapshot"      // parent: instance
	KindSSHKey       Kind = "ssh_key"
	KindVPC          Kind = "vpc"
	KindDNSZone      Kind = "dns_zone"
	KindDNSRecord    Kind = "dns_record" // parent: zone
	KindDNSSEC       Kind = "dnssec"     // parent: zone, id = zone id
	KindDomain       Kind = "domain"
	KindDomainTask   Kind = "domain_task" // parent: domain
)

// Object is a JSON object as the API serves it.
type Object map[string]any

// Scope locates a project-level object.
type Scope struct {
	OrganizationID string
	ProjectID      string
}

type record struct {
	scope    Scope
	parentID string
	seq      int64
	data     Object
}

type store struct {
	seq     int64
	objects map[Kind]map[string]*record
}

func newStore() *store {
	return &store{objects: map[Kind]map[string]*record{}}
}

// clone deep-copies through JSON, so handlers and tests never share maps with the store.
func clone(o Object) Object {
	if o == nil {
		return nil
	}
	raw, err := json.Marshal(o)
	if err != nil {
		panic("fakeapi: object is not JSON: " + err.Error())
	}
	var copied Object
	if err := json.Unmarshal(raw, &copied); err != nil {
		panic("fakeapi: object is not JSON: " + err.Error())
	}
	return copied
}

func now() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000000Z")
}

func (st *store) insert(kind Kind, scope Scope, parentID string, data Object) Object {
	data = clone(data)
	if data == nil {
		data = Object{}
	}
	id := data.ID()
	if id == "" {
		id = uuid.NewString()
		data["id"] = id
	}
	stamp := now()
	if _, ok := data["created"]; !ok {
		data["created"] = stamp
	}
	if _, ok := data["modified"]; !ok {
		data["modified"] = stamp
	}
	st.seq++
	if st.objects[kind] == nil {
		st.objects[kind] = map[string]*record{}
	}
	st.objects[kind][id] = &record{scope: scope, parentID: parentID, seq: st.seq, data: data}
	return clone(data)
}

func (st *store) find(kind Kind, scope Scope, parentID, id string) (*record, bool) {
	rec, ok := st.objects[kind][id]
	if !ok || rec.scope != scope || rec.parentID != parentID {
		return nil, false
	}
	return rec, true
}

func (st *store) list(kind Kind, scope Scope, parentID string) []Object {
	recs := make([]*record, 0, len(st.objects[kind]))
	for _, rec := range st.objects[kind] {
		if rec.scope == scope && rec.parentID == parentID {
			recs = append(recs, rec)
		}
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].seq < recs[j].seq })
	out := make([]Object, 0, len(recs))
	for _, rec := range recs {
		out = append(out, clone(rec.data))
	}
	return out
}

func (st *store) byID(kind Kind, id string) (*record, bool) {
	rec, ok := st.objects[kind][id]
	return rec, ok
}

func (st *store) remove(kind Kind, id string) bool {
	if _, ok := st.objects[kind][id]; !ok {
		return false
	}
	delete(st.objects[kind], id)
	return true
}

// ID returns the object's "id", empty when it has none.
func (o Object) ID() string { return o.String("id") }

// String returns o[key] when it is a string, empty otherwise.
func (o Object) String(key string) string {
	value, _ := o[key].(string)
	return value
}

// Map returns o[key] when it is a JSON object, nil otherwise.
func (o Object) Map(key string) Object {
	value, _ := o[key].(map[string]any)
	return value
}

// List returns o[key] when it is a JSON array, nil otherwise.
func (o Object) List(key string) []any {
	value, _ := o[key].([]any)
	return value
}
