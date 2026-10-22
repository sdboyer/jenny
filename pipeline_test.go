package jenny_test

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/sdboyer/jenny"
)

func TestPipeline(t *testing.T) {
	j := jenny.Pipeline(
		ty.New("O2O", o2o),
		ty.New("M2O", m2o),
		ty.New("M2M", m2m),
	)
	want := []string{"a.o2o", "b.o2o", "index.m2o", "a.m2m", "b.m2m"}
	if diff := cmp.Diff(want, paths(mustGen(t, j, ab...))); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
}

func TestPipelineCollisions(t *testing.T) {
	upper := func(t Type) (*jenny.File, error) { return jenny.NewFile("A.o2o", nil), nil }
	tests := map[string]jenny.Jenny[Type]{
		"exact":            jenny.Pipeline(ty.New("One", o2o), ty.New("Two", o2o)),
		"case-insensitive": jenny.Pipeline(ty.New("One", o2o), ty.New("Upper", upper)),
	}
	for name, j := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := j.Generate(ab...); err == nil {
				t.Error("expected path collision error")
			}
		})
	}
}

func TestPipelineCollectsAllErrors(t *testing.T) {
	e1, e2 := errors.New("e1"), errors.New("e2")
	j := jenny.Pipeline(
		ty.New("One", func(Type) (*jenny.File, error) { return nil, e1 }),
		ty.New("Two", func(...Type) (*jenny.File, error) { return nil, e2 }),
	)
	_, err := j.Generate(ab...)
	if !errors.Is(err, e1) || !errors.Is(err, e2) {
		t.Errorf("expected both errors, got %v", err)
	}
}

func TestGroupForms(t *testing.T) {
	header := func(f jenny.File) (jenny.File, error) {
		f.Data = append([]byte("// gen\n"), f.Data...)
		return f, nil
	}
	tests := map[string]struct {
		group, chained jenny.Jenny[Type]
	}{
		"Prefix": {
			jenny.Prefix("p", ty.New("O2O", o2o), ty.New("M2O", m2o)),
			jenny.Pipeline(ty.New("O2O", o2o), ty.New("M2O", m2o)).Prefix("p"),
		},
		"Map": {
			jenny.Map(header, ty.New("O2O", o2o), ty.New("M2O", m2o)),
			jenny.Pipeline(ty.New("O2O", o2o), ty.New("M2O", m2o)).Map(header),
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if diff := cmp.Diff(contents(mustGen(t, tt.chained, ab...)), contents(mustGen(t, tt.group, ab...))); diff != "" {
				t.Errorf("(-chained +group):\n%s", diff)
			}
		})
	}
}
