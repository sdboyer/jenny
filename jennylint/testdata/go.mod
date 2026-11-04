module example.com/user

go 1.27.0

require github.com/sdboyer/jenny v0.0.0

require (
	github.com/google/go-cmp v0.7.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
)

replace github.com/sdboyer/jenny => ../..
