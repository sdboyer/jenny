package jenny_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/sdboyer/jenny"
)

func TestVerifyErrorOrderIsDeterministic(t *testing.T) {
	fl := make([]jenny.File, 100)
	for i := range fl {
		fl[i] = *jenny.NewFile(fmt.Sprintf("f%03d", i), []byte("x"), src("test"))
	}
	jfs := fsWith(t, fl...)
	dir := t.TempDir()
	var first string
	for i := range 5 {
		err := jfs.Verify(context.Background(), dir)
		if err == nil {
			t.Fatal("expected errors")
		}
		if i == 0 {
			first = err.Error()
			if lines := strings.Split(first, "\n"); !slices.IsSorted(lines) {
				t.Fatalf("Verify errors are not in path order:\n%s", first)
			}
		} else if err.Error() != first {
			t.Fatal("Verify error text differs between runs")
		}
	}
}

func TestWriteVerifyHonorContext(t *testing.T) {
	jfs := fsWith(t, *jenny.NewFile("a", []byte("a"), src("test")), *jenny.NewFile("b", []byte("b"), src("test")))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dir := t.TempDir()

	if err := jfs.Write(ctx, dir); !errors.Is(err, context.Canceled) {
		t.Errorf("Write on canceled ctx returned %v, want context.Canceled", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("Write on canceled ctx wrote %d entries", len(entries))
	}
	if err := jfs.Verify(ctx, dir); !errors.Is(err, context.Canceled) {
		t.Errorf("Verify on canceled ctx returned %v, want context.Canceled", err)
	}
}
