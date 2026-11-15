package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/ryan-flan/DOINK/companion/internal/savedvars"
)

// The public description of an event (docs/events.md for people,
// docs/event.schema.json for programs) must say what the addon writes.
// These tests hold both to the /doink test fixtures: a new type or field
// fails here until it is documented.
//
// Both files are outside the Go module, so `go test`'s result cache
// doesn't notice edits to them: after changing either, run with -count=1
// (CI always does).

const (
	schemaPath = "../../../docs/event.schema.json"
	docPath    = "../../../docs/events.md"
)

type schemaNode = map[string]any

func loadSchema(t *testing.T) schemaNode {
	t.Helper()
	raw, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	var root schemaNode
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("%s: %v", schemaPath, err)
	}
	return root
}

func keysOf[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func stringsOf(v any) []string {
	switch v := v.(type) {
	case string:
		return []string{v}
	case []any:
		var out []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func hasType(value any, name string) bool {
	switch value := value.(type) {
	case nil:
		return name == "null"
	case string:
		return name == "string"
	case bool:
		return name == "boolean"
	case float64:
		return name == "number" || (name == "integer" && value == math.Trunc(value))
	case []any:
		return name == "array"
	case map[string]any:
		return name == "object"
	}
	return false
}

// check is the small part of JSON Schema these files use: type, const,
// required, properties and items. It returns what is wrong with value.
func check(node schemaNode, value any, path string) []string {
	var problems []string
	if types := stringsOf(node["type"]); len(types) > 0 {
		if !slices.ContainsFunc(types, func(name string) bool { return hasType(value, name) }) {
			return []string{fmt.Sprintf("%s: %v is not %s", path, value, strings.Join(types, " or "))}
		}
	}
	if want, ok := node["const"]; ok && want != value {
		problems = append(problems, fmt.Sprintf("%s: %v is not %v", path, value, want))
	}
	if list, ok := value.([]any); ok {
		if items, ok := node["items"].(schemaNode); ok {
			for i, item := range list {
				problems = append(problems, check(items, item, fmt.Sprintf("%s[%d]", path, i))...)
			}
		}
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return problems
	}
	for _, key := range stringsOf(node["required"]) {
		if _, present := obj[key]; !present {
			problems = append(problems, fmt.Sprintf("%s: %q is missing", path, key))
		}
	}
	props, _ := node["properties"].(schemaNode)
	for key, v := range obj {
		if prop, ok := props[key].(schemaNode); ok {
			problems = append(problems, check(prop, v, path+"."+key)...)
		}
	}
	return problems
}

func TestSchemaDescribesEveryFixture(t *testing.T) {
	root := loadSchema(t)
	defs, _ := root["$defs"].(schemaNode)
	if got, want := keysOf(defs), testTypes(); !slices.Equal(got, want) {
		t.Fatalf("schema $defs = %v, fixtures = %v", got, want)
	}

	// Each type's "if type is X then data is #/$defs/X" entry.
	routed := map[string]string{}
	rules, _ := root["allOf"].([]any)
	for _, r := range rules {
		raw, _ := json.Marshal(r)
		var rule struct {
			If struct {
				Properties struct {
					Type struct {
						Const string `json:"const"`
					} `json:"type"`
				} `json:"properties"`
			} `json:"if"`
			Then struct {
				Properties struct {
					Data struct {
						Ref string `json:"$ref"`
					} `json:"data"`
				} `json:"properties"`
			} `json:"then"`
		}
		if err := json.Unmarshal(raw, &rule); err != nil {
			t.Fatal(err)
		}
		routed[rule.If.Properties.Type.Const] = rule.Then.Properties.Data.Ref
	}

	for _, typ := range testTypes() {
		def, _ := defs[typ].(schemaNode)
		fixture := testFixtures[typ]
		if got, want := routed[typ], "#/$defs/"+typ; got != want {
			t.Errorf("%s: allOf sends its data to %q, want %q", typ, got, want)
		}
		props, _ := def["properties"].(schemaNode)
		if got, want := keysOf(props), keysOf(fixture); !slices.Equal(got, want) {
			t.Errorf("%s: schema properties = %v, fixture = %v", typ, got, want)
		}
		required := stringsOf(def["required"])
		sort.Strings(required)
		if want := keysOf(fixture); !slices.Equal(required, want) {
			t.Errorf("%s: schema required = %v, fixture = %v", typ, required, want)
		}
		for _, problem := range check(def, fixture, typ) {
			t.Error(problem)
		}
	}
}

var jsonBlock = regexp.MustCompile("(?s)```json\n(.*?)```")

// Every example in docs/events.md must be an event the companion accepts
// and the schema describes, and every type must have one with exactly
// the fixture's fields.
func TestDocumentedExamples(t *testing.T) {
	root := loadSchema(t)
	defs, _ := root["$defs"].(schemaNode)
	doc, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, m := range jsonBlock.FindAllSubmatch(doc, -1) {
		event, err := savedvars.DecodeEvent(m[1])
		if err != nil {
			t.Errorf("example isn't a valid event (%v):\n%s", err, m[1])
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal(m[1], &obj); err != nil {
			t.Fatal(err)
		}
		for _, problem := range check(root, obj, event.Type) {
			t.Error("example " + problem)
		}
		def, ok := defs[event.Type].(schemaNode)
		if !ok {
			t.Errorf("example has undocumented type %q", event.Type)
			continue
		}
		for _, problem := range check(def, obj["data"], event.Type+".data") {
			t.Error("example " + problem)
		}
		if got, want := keysOf(event.Data), keysOf(testFixtures[event.Type]); !slices.Equal(got, want) {
			t.Errorf("%s example data has %v, fixture has %v", event.Type, got, want)
		}
		seen[event.Type] = true
	}
	for _, typ := range testTypes() {
		if !seen[typ] {
			t.Errorf("docs/events.md has no example of %s", typ)
		}
		if !strings.Contains(string(doc), "\n### `"+typ+"`\n") {
			t.Errorf("docs/events.md has no section for %s", typ)
		}
	}
}
