package api

import (
	"net/http"
	"os"
	"sort"
	"testing"

	"gopkg.in/yaml.v3"
)

// openAPISpec is just enough of api/openapi.yaml's shape to extract the
// (method, path) pairs this test diffs against Server.Routes() —
// deliberately not a full OpenAPI 3 model, since spec-drift detection
// only needs the path/method keys, not schema validation.
type openAPISpec struct {
	Paths map[string]map[string]any `yaml:"paths"`
}

// specRoutes loads api/openapi.yaml and returns every (method, pattern)
// pair it documents, in the same "GET /v1/foo/{id}" shape Server.Routes()
// produces.
func specRoutes(t *testing.T) []Route {
	t.Helper()
	raw, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatalf("read api/openapi.yaml: %v", err)
	}
	var spec openAPISpec
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse api/openapi.yaml: %v", err)
	}

	var routes []Route
	for path, methods := range spec.Paths {
		for method := range methods {
			routes = append(routes, Route{Method: httpMethod(method), Pattern: "/v1" + path})
		}
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Pattern != routes[j].Pattern {
			return routes[i].Pattern < routes[j].Pattern
		}
		return routes[i].Method < routes[j].Method
	})
	return routes
}

func httpMethod(yamlKey string) string {
	switch yamlKey {
	case "get":
		return "GET"
	case "post":
		return "POST"
	case "put":
		return "PUT"
	case "delete":
		return "DELETE"
	case "patch":
		return "PATCH"
	default:
		return yamlKey
	}
}

// TestContract_RoutesMatchOpenAPISpec is this task's spec-drift test
// (docs/PLAN.md Task 36 Acceptance: "spec-drift test fails on
// undocumented route"). It diffs the server's actual registered route
// table against api/openapi.yaml's documented paths in both directions:
// a route registered in code but missing from the spec fails, and a
// documented route with nothing registered also fails.
func TestContract_RoutesMatchOpenAPISpec(t *testing.T) {
	f := newTestFixture(t)

	got := f.server.Routes()
	want := specRoutes(t)

	if len(got) != len(want) {
		t.Fatalf("route count mismatch: server has %d, spec has %d\nserver: %+v\nspec:   %+v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("route %d: server has %+v, spec has %+v", i, got[i], want[i])
		}
	}
}

// TestContract_UndocumentedRouteFailsDrift proves the drift test actually
// bites: registering one extra route the spec doesn't know about must
// change Routes() so a diff against specRoutes would fail. This does not
// call TestContract_RoutesMatchOpenAPISpec itself (that would mutate the
// shared fixture); it asserts the underlying mechanism (Routes()
// reflecting whatever is registered) directly.
func TestContract_UndocumentedRouteFailsDrift(t *testing.T) {
	f := newTestFixture(t)
	f.server.register("GET", "/v1/undocumented-route", staticResource("test:undocumented"), func(w http.ResponseWriter, r *http.Request) {})

	got := f.server.Routes()
	want := specRoutes(t)
	if len(got) == len(want) {
		t.Fatalf("expected route-count drift after registering an undocumented route, got equal counts (%d)", len(got))
	}
}
