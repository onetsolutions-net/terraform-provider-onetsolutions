package fakeapi

import "sort"

// Exported accessors, for test assertions and for changes "made outside Terraform".

// Get returns a copy of an object of any scope.
func (s *Server) Get(kind Kind, id string) (Object, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.store.byID(kind, id)
	if !ok {
		return nil, false
	}
	return clone(rec.data), true
}

// Exists reports whether an object exists.
func (s *Server) Exists(kind Kind, id string) bool {
	_, ok := s.Get(kind, id)
	return ok
}

// Delete removes an object, as if deleted outside Terraform (drift tests).
func (s *Server) Delete(kind Kind, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.remove(kind, id)
}

// Mutate changes an object in place, as if changed outside Terraform.
func (s *Server) Mutate(kind Kind, id string, change func(Object)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.store.byID(kind, id)
	if !ok {
		return false
	}
	change(rec.data)
	rec.data["modified"] = now()
	return true
}

// All returns copies of every object of a kind, in creation order, whatever their scope.
func (s *Server) All(kind Kind) []Object {
	s.mu.Lock()
	defer s.mu.Unlock()
	recs := make([]*record, 0, len(s.store.objects[kind]))
	for _, rec := range s.store.objects[kind] {
		recs = append(recs, rec)
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].seq < recs[j].seq })
	out := make([]Object, 0, len(recs))
	for _, rec := range recs {
		out = append(out, clone(rec.data))
	}
	return out
}

// Scoped accessors, for the area handlers: an object is only found in the scope and under the
// parent it was created in, as the API does.

func (s *Server) insert(kind Kind, scope Scope, parentID string, data Object) Object {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.insert(kind, scope, parentID, data)
}

func (s *Server) lookup(kind Kind, scope Scope, parentID, id string) (Object, bool) {
	s.applyTransitions(kind, id)
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.store.find(kind, scope, parentID, id)
	if !ok {
		return nil, false
	}
	return clone(rec.data), true
}

func (s *Server) list(kind Kind, scope Scope, parentID string) []Object {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.list(kind, scope, parentID)
}

// update applies change to the stored object and returns a copy of the result.
func (s *Server) update(kind Kind, scope Scope, parentID, id string, change func(Object)) (Object, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.store.find(kind, scope, parentID, id)
	if !ok {
		return nil, false
	}
	change(rec.data)
	rec.data["modified"] = now()
	return clone(rec.data), true
}

func (s *Server) remove(kind Kind, scope Scope, parentID, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.store.find(kind, scope, parentID, id); !ok {
		return false
	}
	return s.store.remove(kind, id)
}

type transition struct {
	afterPolls int
	change     func(Object)
}

// afterPolls schedules change on an object once it has been read polls more times through
// lookup: an instance becomes active after a few reads, like the real API.
func (s *Server) afterPolls(kind Kind, id string, polls int, change func(Object)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := string(kind) + "/" + id
	s.transitions[key] = append(s.transitions[key], &transition{afterPolls: s.pollCounts[key] + polls, change: change})
}

func (s *Server) applyTransitions(kind Kind, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := string(kind) + "/" + id
	s.pollCounts[key]++
	pending := s.transitions[key][:0]
	for _, t := range s.transitions[key] {
		if s.pollCounts[key] < t.afterPolls {
			pending = append(pending, t)
			continue
		}
		if rec, ok := s.store.byID(kind, id); ok {
			t.change(rec.data)
			rec.data["modified"] = now()
		}
	}
	s.transitions[key] = pending
}
