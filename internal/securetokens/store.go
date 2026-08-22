package securetokens

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const tokenBytes = 32

var ErrNotFound = errors.New("protected token not found")

type Store interface {
	Put(ctx context.Context, scope string, digest []byte, token string) (string, error)
	Read(ctx context.Context, reference string) (string, error)
	Delete(ctx context.Context, reference string) error
}

func Generate() (string, []byte, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate protected token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, Digest(token), nil
}

func Digest(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

type FileStore struct {
	root string
}

func NewFileStore(root string) (*FileStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("protected token root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve protected token root: %w", err)
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("create protected token root: %w", err)
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return nil, fmt.Errorf("inspect protected token root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, errors.New("protected token root must be a directory and not a symlink")
	}
	if err := os.Chmod(abs, 0o700); err != nil {
		return nil, fmt.Errorf("protect token root: %w", err)
	}
	return &FileStore{root: abs}, nil
}

func (s *FileStore) Put(ctx context.Context, scope string, digest []byte, token string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !validScope(scope) || len(digest) != sha256.Size || strings.TrimSpace(token) == "" {
		return "", errors.New("invalid protected token input")
	}
	name := hex.EncodeToString(digest)
	reference := scope + "/" + name
	dir := filepath.Join(s.root, scope)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create protected token scope: %w", err)
	}
	if info, err := os.Lstat(dir); err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", errors.New("protected token scope is unsafe")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", fmt.Errorf("protect token scope: %w", err)
	}

	destination := filepath.Join(dir, name)
	if _, err := os.Lstat(destination); err == nil {
		return "", errors.New("protected token already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect protected token destination: %w", err)
	}

	temp, err := os.CreateTemp(dir, ".token-*")
	if err != nil {
		return "", fmt.Errorf("create protected token temporary file: %w", err)
	}
	tempName := temp.Name()
	removeTemp := true
	defer func() {
		_ = temp.Close()
		if removeTemp {
			_ = os.Remove(tempName)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return "", fmt.Errorf("protect token temporary file: %w", err)
	}
	if _, err := temp.WriteString(token + "\n"); err != nil {
		return "", fmt.Errorf("write protected token: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return "", fmt.Errorf("sync protected token: %w", err)
	}
	if err := temp.Close(); err != nil {
		return "", fmt.Errorf("close protected token: %w", err)
	}
	if err := os.Rename(tempName, destination); err != nil {
		return "", fmt.Errorf("publish protected token: %w", err)
	}
	removeTemp = false
	return reference, nil
}

func (s *FileStore) Read(ctx context.Context, reference string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path, err := s.resolve(reference)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("inspect protected token: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return "", errors.New("protected token file is unsafe")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read protected token: %w", err)
	}
	token := strings.TrimSuffix(string(body), "\n")
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return "", errors.New("protected token file is malformed")
	}
	return token, nil
}

func (s *FileStore) Delete(ctx context.Context, reference string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.resolve(reference)
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect protected token: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("protected token file is unsafe")
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("delete protected token: %w", err)
	}
	return nil
}

func (s *FileStore) resolve(reference string) (string, error) {
	parts := strings.Split(reference, "/")
	if len(parts) != 2 || !validScope(parts[0]) || len(parts[1]) != sha256.Size*2 {
		return "", errors.New("invalid protected token reference")
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return "", errors.New("invalid protected token reference")
	}
	return filepath.Join(s.root, parts[0], parts[1]), nil
}

func validScope(scope string) bool {
	return scope == "review" || scope == "signup"
}

type MemoryStore struct {
	mu     sync.Mutex
	values map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{values: make(map[string]string)}
}

func (s *MemoryStore) Put(_ context.Context, scope string, digest []byte, token string) (string, error) {
	if !validScope(scope) || len(digest) != sha256.Size || token == "" {
		return "", errors.New("invalid protected token input")
	}
	reference := scope + "/" + hex.EncodeToString(digest)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.values[reference]; exists {
		return "", errors.New("protected token already exists")
	}
	s.values[reference] = token
	return reference, nil
}

func (s *MemoryStore) Read(_ context.Context, reference string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	token, ok := s.values[reference]
	if !ok {
		return "", ErrNotFound
	}
	return token, nil
}

func (s *MemoryStore) Delete(_ context.Context, reference string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, reference)
	return nil
}
