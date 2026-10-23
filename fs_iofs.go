package jenny

import "io/fs"

// The io/fs methods of FS wrap those of its fstest.MapFS, which reads the
// map directly and so must not run concurrently with FS.Add or FS.Merge.

var (
	_ fs.ReadFileFS = (*FS)(nil)
	_ fs.ReadDirFS  = (*FS)(nil)
	_ fs.StatFS     = (*FS)(nil)
	_ fs.GlobFS     = (*FS)(nil)
	_ fs.SubFS      = (*FS)(nil)
)

// Open implements [io/fs.FS].
func (fsys *FS) Open(name string) (fs.File, error) {
	fsys.mu.RLock()
	defer fsys.mu.RUnlock()
	return fsys.m.Open(name)
}

// ReadFile implements [io/fs.ReadFileFS].
func (fsys *FS) ReadFile(name string) ([]byte, error) {
	fsys.mu.RLock()
	defer fsys.mu.RUnlock()
	return fsys.m.ReadFile(name)
}

// Stat implements [io/fs.StatFS].
func (fsys *FS) Stat(name string) (fs.FileInfo, error) {
	fsys.mu.RLock()
	defer fsys.mu.RUnlock()
	return fsys.m.Stat(name)
}

// ReadDir implements [io/fs.ReadDirFS].
func (fsys *FS) ReadDir(name string) ([]fs.DirEntry, error) {
	fsys.mu.RLock()
	defer fsys.mu.RUnlock()
	return fsys.m.ReadDir(name)
}

// Glob implements [io/fs.GlobFS].
func (fsys *FS) Glob(pattern string) ([]string, error) {
	fsys.mu.RLock()
	defer fsys.mu.RUnlock()
	return fsys.m.Glob(pattern)
}

// Sub implements [io/fs.SubFS]. The returned FS reads through the receiver, so
// it observes later additions and is equally safe for concurrent use.
func (fsys *FS) Sub(dir string) (fs.FS, error) {
	// Not MapFS.Sub: that would read the map without the lock.
	return fs.Sub(fsOnly{fsys}, dir)
}

// fsOnly hides all but the fs.FS methods, so that the fs helper funcs use
// Open rather than calling back into FS.
type fsOnly struct{ fs.FS }
