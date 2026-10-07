// Package store persists the XML documents issued and received by the
// service. DGII requires taxpayers to keep the signed XML of every e-CF.
package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Kind classifies stored documents.
type Kind string

// Document kinds.
const (
	EmitidoECF    Kind = "emitidos/ecf"
	EmitidoRFCE   Kind = "emitidos/rfce"
	EmitidoACECF  Kind = "emitidos/aprobaciones"
	EmitidoANECF  Kind = "emitidos/anulaciones"
	RecibidoECF   Kind = "recibidos/ecf"
	RecibidoACECF Kind = "recibidos/aprobaciones"
	AcuseEnviado  Kind = "recibidos/acuses"
	// AcuseRecibido holds the ARECF returned by buyers for issued e-CF.
	AcuseRecibido Kind = "emitidos/acuses"
	// Estado holds JSON submission-tracking records.
	Estado Kind = "estado"
)

// ErrNotFound is returned when a document does not exist.
var ErrNotFound = errors.New("store: documento no encontrado")

// Store persists documents keyed by kind and file name (RNC + e-NCF.xml).
type Store interface {
	Put(ctx context.Context, kind Kind, name string, data []byte) error
	Get(ctx context.Context, kind Kind, name string) ([]byte, error)
	Exists(ctx context.Context, kind Kind, name string) (bool, error)
	// List returns the names stored under kind, sorted.
	List(ctx context.Context, kind Kind) ([]string, error)
}

// FS stores documents as files under a root directory.
type FS struct {
	root string
}

// NewFS creates a filesystem store rooted at dir.
func NewFS(dir string) (*FS, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	return &FS{root: dir}, nil
}

func (s *FS) path(kind Kind, name string) (string, error) {
	if name == "" || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return "", fmt.Errorf("store: nombre de archivo inválido %q", name)
	}
	return filepath.Join(s.root, filepath.FromSlash(string(kind)), name), nil
}

// Put writes the document atomically.
func (s *FS) Put(_ context.Context, kind Kind, name string, data []byte) error {
	p, err := s.path(kind, name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), p)
}

// Get reads a document.
func (s *FS) Get(_ context.Context, kind Kind, name string) ([]byte, error) {
	p, err := s.path(kind, name)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return b, err
}

// Exists reports whether a document exists.
func (s *FS) Exists(_ context.Context, kind Kind, name string) (bool, error) {
	p, err := s.path(kind, name)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(p)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// List returns the document names of kind, sorted.
func (s *FS) List(_ context.Context, kind Kind) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(s.root, filepath.FromSlash(string(kind))))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Type().IsRegular() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// Memory is an in-memory Store for tests and ephemeral deployments.
type Memory struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// NewMemory creates an empty in-memory store.
func NewMemory() *Memory { return &Memory{data: map[string][]byte{}} }

// Put stores a copy of data.
func (m *Memory) Put(_ context.Context, kind Kind, name string, data []byte) error {
	m.mu.Lock()
	m.data[string(kind)+"/"+name] = append([]byte(nil), data...)
	m.mu.Unlock()
	return nil
}

// Get returns a stored document.
func (m *Memory) Get(_ context.Context, kind Kind, name string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.data[string(kind)+"/"+name]
	if !ok {
		return nil, ErrNotFound
	}
	return b, nil
}

// List returns the document names of kind, sorted.
func (m *Memory) List(_ context.Context, kind Kind) ([]string, error) {
	prefix := string(kind) + "/"
	m.mu.RLock()
	var out []string
	for k := range m.data {
		if strings.HasPrefix(k, prefix) && !strings.Contains(k[len(prefix):], "/") {
			out = append(out, k[len(prefix):])
		}
	}
	m.mu.RUnlock()
	sort.Strings(out)
	return out, nil
}

// Exists reports whether a document exists.
func (m *Memory) Exists(_ context.Context, kind Kind, name string) (bool, error) {
	m.mu.RLock()
	_, ok := m.data[string(kind)+"/"+name]
	m.mu.RUnlock()
	return ok, nil
}
