package securetokens

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateUsesThirtyTwoRandomBytes(t *testing.T) {
	first, firstDigest, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || len(firstDigest) != 32 || len(first) != 43 {
		t.Fatalf("unexpected generated token properties")
	}
}

func TestFileStoreWritesPrivateRegularFileAndRoundTrips(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tokens")
	store, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	token, digest, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	reference, err := store.Put(context.Background(), "signup", digest, token)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, reference)
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 {
		t.Fatalf("unsafe token metadata: mode=%v", info.Mode())
	}
	got, err := store.Read(context.Background(), reference)
	if err != nil || got != token {
		t.Fatalf("round trip failed: err=%v", err)
	}
	if err := store.Delete(context.Background(), reference); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(context.Background(), reference); err != ErrNotFound {
		t.Fatalf("read after delete error = %v", err)
	}
}

func TestFileStoreRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "signup"), 0o700); err != nil {
		t.Fatal(err)
	}
	reference := "signup/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := os.Symlink(target, filepath.Join(root, reference)); err != nil {
		t.Fatal(err)
	}
	store, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(context.Background(), reference); err == nil {
		t.Fatal("expected symlink rejection")
	}
}
