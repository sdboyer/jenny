package jenny_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/sdboyer/jenny"
)

type src string

func (s src) JennyName() string { return string(s) }

func fsWith(t *testing.T, fl ...jenny.File) *jenny.FS {
	t.Helper()
	jfs := jenny.NewFS()
	if err := jfs.Add(fl...); err != nil {
		t.Fatal(err)
	}
	return jfs
}

func TestWriteVerify(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	jfs := fsWith(t,
		*jenny.NewFile("a.txt", []byte("a"), src("test")),
		*jenny.NewFile("sub/b.txt", []byte("b"), src("test")),
	)

	if err := jfs.Verify(ctx, dir); err == nil {
		t.Fatal("expected Verify to fail before Write")
	}
	if err := jfs.Write(ctx, dir); err != nil {
		t.Fatal(err)
	}
	if err := jfs.Verify(ctx, dir); err != nil {
		t.Fatalf("Verify after Write: %s", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatal(err)
	}
	err := jfs.Verify(ctx, dir)
	if err == nil {
		t.Fatal("expected Verify to fail")
	}
	if !strings.Contains(err.Error(), "a.txt: generated file should exist") {
		t.Errorf("missing file not reported: %s", err)
	}
	if !strings.Contains(err.Error(), "b.txt would have changed") {
		t.Errorf("changed file not reported: %s", err)
	}
}

// Run with -race: Verify collects errors from concurrent goroutines.
func TestVerifyManyMismatches(t *testing.T) {
	fl := make([]jenny.File, 200)
	for i := range fl {
		fl[i] = *jenny.NewFile(fmt.Sprintf("f%03d", i), []byte("x"), src("test"))
	}
	err := fsWith(t, fl...).Verify(context.Background(), t.TempDir())
	if err == nil {
		t.Fatal("expected errors")
	}
	if n := strings.Count(err.Error(), "should exist"); n != len(fl) {
		t.Errorf("got %d missing-file errors, want %d", n, len(fl))
	}
}

func TestWriteCannotEscapeRoot(t *testing.T) {
	jfs := jenny.NewFS()
	if err := jfs.Add(*jenny.NewFile("../escaped.txt", []byte("x"), src("test"))); err == nil {
		t.Fatal("FS accepted a File whose path escapes the root")
	}
}

func TestMergeMap(t *testing.T) {
	a := fsWith(t, *jenny.NewFile("a", []byte("a"), src("test")))
	b := fsWith(t, *jenny.NewFile("b", []byte("b"), src("test")))
	if err := a.Merge(b); err != nil {
		t.Fatal(err)
	}
	if err := a.Merge(b); err == nil {
		t.Error("expected duplicate path error on second Merge")
	}

	m, err := a.Map(func(f jenny.File) (jenny.File, error) {
		f.Data = append([]byte("!"), f.Data...)
		return f, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"a": "!a", "b": "!b"}
	if diff := cmp.Diff(want, contents(m.AsFiles())); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
}

func TestFilesValidate(t *testing.T) {
	tests := map[string]struct {
		fl      jenny.Files
		wantErr bool
	}{
		"ok":                    {jenny.Files{*jenny.NewFile("a", nil, src("t")), *jenny.NewFile("b", nil, src("t"))}, false},
		"duplicate":             {jenny.Files{*jenny.NewFile("a", nil, src("t")), *jenny.NewFile("a", nil, src("t"))}, true},
		"case-insensitive dupe": {jenny.Files{*jenny.NewFile("a", nil, src("t")), *jenny.NewFile("A", nil, src("t"))}, true},
		"absolute":              {jenny.Files{*jenny.NewFile("/a", nil, src("t"))}, true},
		"nested":                {jenny.Files{*jenny.NewFile("a/b/c.go", nil, src("t"))}, false},
		"parent escape":         {jenny.Files{*jenny.NewFile("../a", nil, src("t"))}, true},
		"inner parent escape":   {jenny.Files{*jenny.NewFile("a/../../b", nil, src("t"))}, true},
		"dot element":           {jenny.Files{*jenny.NewFile("./a", nil, src("t"))}, true},
		"empty element":         {jenny.Files{*jenny.NewFile("a//b", nil, src("t"))}, true},
		"trailing slash":        {jenny.Files{*jenny.NewFile("a/", nil, src("t"))}, true},
		"backslash":             {jenny.Files{*jenny.NewFile(`a\..\..\b`, nil, src("t"))}, true},
		"no From":               {jenny.Files{*jenny.NewFile("a", nil)}, true},
		"nonexistent":           {jenny.Files{{}}, true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if err := tt.fl.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Validate() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
