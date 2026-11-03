package jenny_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/sdboyer/jenny"
)

// owner is a many-input jenny that generates nothing and owns pattern.
func owner(pattern string) jenny.Jenny[Type] {
	return ty.New("Owner", func(...Type) (jenny.File, error) { return nil, nil }, jenny.Owns(pattern))
}

func claimPaths(cs []jenny.Claim) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Path() + " (" + c.FromString() + ")"
	}
	return out
}

func TestClaimPath(t *testing.T) {
	tests := map[string]struct {
		pattern string
		prefix  []string // applied innermost first
		want    string
	}{
		"root glob":          {"*.go", nil, "*.go"},
		"literal dir":        {"pkg/api/*.go", nil, "pkg/api/*.go"},
		"glob dir":           {"gen/*/types.go", nil, "gen/*/types.go"},
		"literal file":       {"a/b.go", nil, "a/b.go"},
		"prefixed":           {"*.go", []string{"pkg/api"}, "pkg/api/*.go"},
		"prefixed twice":     {"sub/*.go", []string{"inner", "outer"}, "outer/inner/sub/*.go"},
		"meta in prefix dir": {"*.go", []string{"gen[1]"}, `gen\[1]/*.go`},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			j := owner(tt.pattern)
			for _, p := range tt.prefix {
				j = j.Prefix(p)
			}
			if diff := cmp.Diff([]string{tt.want + " (Owner)"}, claimPaths(j.Claims())); diff != "" {
				t.Errorf("(-want +got):\n%s", diff)
			}
		})
	}
}

func TestClaimMatch(t *testing.T) {
	tests := []struct {
		pattern, path string
		want          bool
	}{
		{"*.go", "a.go", true},
		{"*.go", "a.ts", false},
		{"*.go", "sub/a.go", false}, // '*' never crosses '/'
		{"pkg/api/*.go", "pkg/api/a.go", true},
		{"pkg/api/*.go", "pkg/api/sub/a.go", false},
		{"pkg/api/*.go", "pkg/a.go", false},
		{"pkg/api/*.go", "pkg/apix/a.go", false},
		{"gen/*/types.go", "gen/v1/types.go", true},
		{"gen/*/types.go", "gen/v1/x/types.go", false},
		{"*_gen.go", "Types_GEN.go", true}, // case-insensitive, like path conflicts
		{"pkg/*.go", "PKG/a.go", true},
		{"a/b.go", "a/b.go", true},
		{`\*.go`, "*.go", true}, // path.Match escapes
		{`\*.go`, "a.go", false},
	}
	for _, tt := range tests {
		if got := owner(tt.pattern).Claims()[0].Match(tt.path); got != tt.want {
			t.Errorf("claim %q: Match(%q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
		}
	}

	// A directory placed by Prefix is literal, even if it holds pattern
	// syntax.
	c := owner("*.go").Prefix("gen[1]").Claims()[0]
	if !c.Match("gen[1]/a.go") || c.Match("gen1/a.go") {
		t.Errorf("claim placed in gen[1] matched as a pattern: %s", c.Path())
	}
}

func TestClaimValidate(t *testing.T) {
	tests := map[string]struct {
		j       jenny.Jenny[Type]
		wantErr bool
	}{
		"ok":                 {owner("pkg/*.go"), false},
		"literal":            {owner("a.go"), false},
		"escape in dir elem": {owner(`a\b/*.go`), false},
		"bad pattern":        {owner("["), true},
		"empty":              {owner(""), true},
		"parent dir":         {owner("../*.go"), true},
		"empty element":      {owner("a//*.go"), true},
		"parent in glob":     {owner("*/../x"), true},
		"absolute":           {owner("/a/*.go"), true},
		"trailing slash":     {owner("a/*/"), true},
		"device name":        {owner("nul/*.go"), true}, // #37's portable path rules
		"placed outside":     {owner("*.go").Prefix("../out"), true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := tt.j.GenerateFS(ab...); (err != nil) != tt.wantErr {
				t.Errorf("GenerateFS() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// goTypesOwning is a single-input jenny that owns every .go file beside its
// output.
func goTypesOwning(name string) jenny.Jenny[Type] {
	return ty.New(name, func(t Type) (jenny.File, error) {
		return jenny.NewFile(t.Name+".go", nil), nil
	}, jenny.Owns("*.go"))
}

func TestClaimsThroughComposition(t *testing.T) {
	calls := 0
	upper := func(f jenny.File) (jenny.File, error) {
		calls++
		data, _ := f.Render()
		return jenny.NewFile(f.Path(), []byte(strings.ToUpper(string(data))), f.From()...), nil
	}
	j := jenny.Prefix("out",
		gr.New("GroupDoc", func(g Group) (jenny.File, error) { return jenny.NewFile(g.Name+".md", nil), nil }, jenny.Owns("*.md")),
		gr.Adapt(func(g Group) ([]Type, error) { return g.Types, nil },
			goTypesOwning("GoTypes").Prefix("types"),
		).Map(upper),
	).With(func(j jenny.Jenny[Group]) jenny.Jenny[Group] { return j.Prefix("gen") })

	want := []string{"gen/out/*.md (GroupDoc)", "gen/out/types/*.go (GoTypes)"}
	if diff := cmp.Diff(want, claimPaths(j.Claims())); diff != "" {
		t.Errorf("Claims (-want +got):\n%s", diff)
	}
	jfs, err := j.GenerateFS(groups...)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, claimPaths(jfs.Claims())); diff != "" {
		t.Errorf("FS claims (-want +got):\n%s", diff)
	}
	if calls != 3 {
		t.Errorf("FileMapper called %d times, want 3, once per file", calls)
	}

	// Claims don't depend on inputs: a run with none still claims, and so
	// does an Adapt subtree that never runs.
	none, err := j.GenerateFS()
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, claimPaths(none.Claims())); diff != "" {
		t.Errorf("claims of a run with no inputs (-want +got):\n%s", diff)
	}
	filtered := gr.Adapt(func(Group) (*Type, error) { return nil, nil },
		jenny.For[*Type]().New("Never", func(*Type) (jenny.File, error) { return nil, nil }, jenny.Owns("*.never")))
	if diff := cmp.Diff([]string{"*.never (Never)"}, claimPaths(filtered.Claims())); diff != "" {
		t.Errorf("claims of an Adapt that filters everything (-want +got):\n%s", diff)
	}
}

func TestClaimsOverlap(t *testing.T) {
	// Overlapping claims, a claim over another jenny's file, and a claim and a
	// file at the same literal path are all allowed.
	j := jenny.Pipeline(
		goTypesOwning("GoTypes"),
		ty.New("Other", func(...Type) (jenny.File, error) { return jenny.NewFile("other.go", nil), nil }, jenny.Owns("o*.go", "other.go")),
	)
	jfs, err := j.GenerateFS(ab...)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"*.go (GoTypes)", "o*.go (Other)", "other.go (Other)"}
	if diff := cmp.Diff(want, claimPaths(jfs.Claims())); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}

	// File collisions are still errors.
	if _, err := jenny.Pipeline(goTypesOwning("One"), goTypesOwning("Two")).Generate(ab...); err == nil {
		t.Error("expected path collision error")
	}
}
