package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

const (
	keyA = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOiuTuZpapVCjAUUGcXj2zMLBbrr0QyMsca1UEx+EBWQ alice@example.com"
	keyB = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIL85YGKsHVZg1FilB2p/FQs777cdPnqWtr8rfoKImcUU bob@example.com"
	own  = "ssh-rsa AAAAB3NzaC1yc2E own@laptop"
)

func TestUpdateAuthorizedKeys(t *testing.T) {
	got := updateAuthorizedKeys(own+"\n", []string{keyA, keyB, keyA})
	want := own + "\n" + keysBegin + "\n" + keyA + "\n" + keyB + "\n" + keysEnd + "\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if !slices.Equal(managedKeys(got), []string{keyA, keyB}) {
		t.Fatalf("managed keys: %q", managedKeys(got))
	}
	// Replacing keeps own keys, also ones added after the block.
	got = updateAuthorizedKeys(got+"ssh-rsa AAAA later\n", []string{keyB})
	want = own + "\nssh-rsa AAAA later\n" + keysBegin + "\n" + keyB + "\n" + keysEnd + "\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if got = updateAuthorizedKeys(got, []string{}); got != own+"\nssh-rsa AAAA later\n" {
		t.Fatalf("empty list should remove the block: %q", got)
	}
	if got = updateAuthorizedKeys("", nil); got != "" {
		t.Fatalf("%q", got)
	}
}

func TestApplyKeys(t *testing.T) {
	file := filepath.Join(t.TempDir(), "root", ".ssh", "authorized_keys")
	keys, err := applyKeys(file, []string{keyA})
	if err != nil || !slices.Equal(keys, []string{keyA}) {
		t.Fatalf("%q %v", keys, err)
	}
	if fi, err := os.Stat(file); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", fi, err)
	}
	if fi, _ := os.Stat(filepath.Dir(file)); fi.Mode().Perm() != 0o700 {
		t.Fatalf(".ssh mode %v", fi.Mode())
	}
	// A wrong mode of an existing .ssh is fixed.
	os.Chmod(filepath.Dir(file), 0o775)
	applyKeys(file, []string{keyA})
	if fi, _ := os.Stat(filepath.Dir(file)); fi.Mode().Perm() != 0o700 {
		t.Fatalf(".ssh mode not fixed: %v", fi.Mode())
	}
	os.WriteFile(file, []byte(own+"\n"+keysBegin+"\n"+keyA+"\n"+keysEnd+"\n"), 0o600)
	if keys, _ = applyKeys(file, []string{keyB}); !slices.Equal(keys, []string{keyB}) {
		t.Fatalf("%q", keys)
	}
	data, _ := os.ReadFile(file)
	if string(data) != own+"\n"+keysBegin+"\n"+keyB+"\n"+keysEnd+"\n" {
		t.Fatalf("own keys must be kept:\n%s", data)
	}
	if keys, _ = applyKeys(file, []string{}); keys == nil || len(keys) != 0 {
		t.Fatalf("expected empty list, got %#v", keys)
	}
}

func TestPasswordAuthentication(t *testing.T) {
	on, ok := passwordAuthentication("port 22\npermitrootlogin yes\npasswordauthentication no\n")
	if !ok || on {
		t.Fatalf("%v %v", on, ok)
	}
	if on, ok = passwordAuthentication("PasswordAuthentication yes"); !ok || !on {
		t.Fatalf("%v %v", on, ok)
	}
	if _, ok = passwordAuthentication(""); ok {
		t.Fatal("empty output")
	}
}
