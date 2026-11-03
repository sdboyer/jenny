package jenny_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/sdboyer/jenny"
)

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
			j := ty.New("Owner", func(...Type) (jenny.File, error) { return jenny.NewClaim(tt.pattern), nil })
			for _, p := range tt.prefix {
				j = j.Prefix(p)
			}
			fl := mustGen(t, j, ab...)
			if len(fl) != 1 {
				t.Fatalf("got %d Files, want 1", len(fl))
			}
			if got := fl[0].Path(); got != tt.want {
				t.Errorf("Path() = %q, want %q", got, tt.want)
			}
			if got := fl[0].FromString(); got != "Owner" {
				t.Errorf("From is %q, want %q", got, "Owner")
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
		if got := jenny.NewClaim(tt.pattern).Match(tt.path); got != tt.want {
			t.Errorf("NewClaim(%q).Match(%q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
		}
	}

	// A directory placed by Prefix is literal, even if it holds pattern
	// syntax.
	fl := mustGen(t, ty.New("O", func(...Type) (jenny.File, error) { return jenny.NewClaim("*.go"), nil }).Prefix("gen[1]"), ab...)
	c := fl[0].(jenny.Claim)
	if !c.Match("gen[1]/a.go") || c.Match("gen1/a.go") {
		t.Errorf("claim placed in gen[1] matched as a pattern: %s", c.Path())
	}
}

func TestClaimValidate(t *testing.T) {
	tests := map[string]struct {
		c       jenny.Claim
		wantErr bool
	}{
		"ok":                 {jenny.NewClaim("pkg/*.go", src("t")), false},
		"literal":            {jenny.NewClaim("a.go", src("t")), false},
		"bad pattern":        {jenny.NewClaim("[", src("t")), true},
		"empty":              {jenny.NewClaim("", src("t")), true},
		"parent dir":         {jenny.NewClaim("../*.go", src("t")), true},
		"empty element":      {jenny.NewClaim("a//*.go", src("t")), true},
		"parent in glob":     {jenny.NewClaim("*/../x", src("t")), true},
		"absolute":           {jenny.NewClaim("/a/*.go", src("t")), true},
		"trailing slash":     {jenny.NewClaim("a/*/", src("t")), true},
		"no From":            {jenny.NewClaim("*.go"), true},
		"escape in dir elem": {jenny.NewClaim(`a\b/*.go`, src("t")), false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if err := tt.c.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Validate() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}

	// Prefix can place a claim somewhere invalid, which is caught.
	j := ty.New("O", func(...Type) (jenny.File, error) { return jenny.NewClaim("*.go"), nil }).Prefix("../out")
	if _, err := j.Generate(ab...); err == nil {
		t.Error("expected error for claim placed outside the root")
	}
}

// ownsGo is a single-input jenny that returns its file and a claim on every
// .go file beside it, once per input.
func ownsGo(name string) jenny.Jenny[Type] {
	return ty.New(name, func(t Type) (jenny.Files, error) {
		return jenny.Files{jenny.NewFile(t.Name+".go", nil), jenny.NewClaim("*.go")}, nil
	})
}

func TestClaimsInPipelines(t *testing.T) {
	t.Run("repeated and overlapping claims are allowed", func(t *testing.T) {
		j := jenny.Pipeline(
			ownsGo("GoTypes"), // claims *.go once per input
			ty.New("Other", func(...Type) (jenny.Files, error) {
				// Overlaps GoTypes' claim, and its file is inside it.
				return jenny.Files{jenny.NewFile("other.go", nil), jenny.NewClaim("o*.go")}, nil
			}),
		)
		want := []string{"a.go", "*.go", "b.go", "*.go", "other.go", "o*.go"}
		if diff := cmp.Diff(want, paths(mustGen(t, j, ab...))); diff != "" {
			t.Errorf("(-want +got):\n%s", diff)
		}
	})

	t.Run("a claim and a file may share a literal path", func(t *testing.T) {
		j := ty.New("Both", func(...Type) (jenny.Files, error) {
			return jenny.Files{jenny.NewFile("a.go", nil), jenny.NewClaim("a.go")}, nil
		})
		mustGen(t, j, ab...)
	})

	t.Run("file collisions are still errors", func(t *testing.T) {
		if _, err := jenny.Pipeline(ownsGo("One"), ownsGo("Two")).Generate(ab...); err == nil {
			t.Error("expected path collision error")
		}
	})
}

func TestClaimsThroughDecorators(t *testing.T) {
	calls := 0
	upper := func(f jenny.Content) (jenny.Content, error) {
		calls++
		data, _ := f.Render()
		return jenny.NewFile(f.Path(), []byte(strings.ToUpper(string(data))), f.From()...), nil
	}
	j := jenny.Prefix("out",
		gr.Adapt(func(g Group) ([]Type, error) { return g.Types, nil },
			ownsGo("GoTypes").Prefix("types"),
		).Map(upper),
	)
	fl := mustGen(t, j, groups...)

	want := []string{
		"out/types/a.go (GoTypes) jenny.Bytes \"\"",
		"out/types/*.go (GoTypes) jenny.Claim \"\"",
		"out/types/b.go (GoTypes) jenny.Bytes \"\"",
		"out/types/*.go (GoTypes) jenny.Claim \"\"",
		"out/types/c.go (GoTypes) jenny.Bytes \"\"",
		"out/types/*.go (GoTypes) jenny.Claim \"\"",
	}
	if diff := cmp.Diff(want, describe(fl)); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
	if calls != 3 {
		t.Errorf("FileMapper called %d times, want 3: it must see only Content", calls)
	}
}
