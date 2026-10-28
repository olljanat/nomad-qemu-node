package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// SSH is the SSH part of the desired state and of the report. A nil field
// is not managed.
type SSH struct {
	AuthorizedKeys         []string `json:"authorized_keys"`
	PasswordAuthentication *bool    `json:"password_authentication"`
}

const (
	keysBegin = "# BEGIN burmilla-fleet-manager"
	keysEnd   = "# END burmilla-fleet-manager"

	sshdBin = "/usr/sbin/sshd"
)

// sshPaths are the files fleet-client manages. /root and /etc/ssh are
// persistent on Nomad QEMU nodes.
type sshPaths struct {
	authorizedKeys string // of root, the only user which logs in
	sshdDropIn     string
}

var defaultSSHPaths = sshPaths{
	authorizedKeys: "/root/.ssh/authorized_keys",
	// Wins over sshd_config and later drop-ins (sshd uses the first value
	// it finds, and sshd_config includes sshd_config.d first).
	sshdDropIn: "/etc/ssh/sshd_config.d/50-burmilla-fleet.conf",
}

// applySSH applies the desired SSH settings and returns the resulting
// state for the report.
func (c *client) applySSH(d SSH) (*SSH, error) {
	st := &SSH{}
	var errs []string
	if d.AuthorizedKeys != nil {
		keys, err := applyKeys(c.ssh.authorizedKeys, d.AuthorizedKeys)
		if err != nil {
			errs = append(errs, err.Error())
		}
		st.AuthorizedKeys = keys
	}
	pass, err := applyPasswordAuthentication(c.ssh.sshdDropIn, d.PasswordAuthentication)
	if err != nil {
		errs = append(errs, err.Error())
	}
	st.PasswordAuthentication = pass
	if len(errs) > 0 {
		return st, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return st, nil
}

// applyPasswordAuthentication writes the sshd drop-in when the setting is
// managed, reloads sshd and returns the effective setting (sshd -T).
func applyPasswordAuthentication(dropIn string, want *bool) (*bool, error) {
	// sshd -t and -T need the privilege separation directory, which only
	// exists while sshd runs (socket activation).
	_ = os.MkdirAll("/run/sshd", 0o755)
	if want != nil {
		mode := "no"
		if *want {
			mode = "yes"
		}
		content := "# Managed by fleet-client\nPasswordAuthentication " + mode + "\n"
		if mode == "no" {
			content += "KbdInteractiveAuthentication no\n"
		}
		old, _ := os.ReadFile(dropIn)
		if string(old) != content {
			if err := writeFile(dropIn, content); err != nil {
				return nil, err
			}
			if _, err := capture(sshdBin, "-t"); err != nil {
				os.Remove(dropIn)
				return nil, fmt.Errorf("sshd rejects the configuration: %v", err)
			}
			// Reloads a running sshd; when it is not running (socket
			// activation) it reads the file when it starts.
			if err := run("systemctl", "try-reload-or-restart", "ssh.service"); err != nil {
				return nil, fmt.Errorf("reload sshd: %v", err)
			}
			log.Printf("sshd: PasswordAuthentication %s", mode)
		}
	}
	out, err := capture(sshdBin, "-T")
	if err != nil {
		if want == nil {
			// Only reading the state, not an error of the node.
			log.Printf("cannot read sshd settings: %v", err)
			return nil, nil
		}
		return nil, fmt.Errorf("sshd -T: %v", err)
	}
	on, ok := passwordAuthentication(out)
	if !ok {
		return nil, nil
	}
	if want != nil && on != *want {
		return &on, fmt.Errorf("sshd does not apply password_authentication (overridden in sshd_config?)")
	}
	return &on, nil
}

// passwordAuthentication finds the setting in sshd -T output.
func passwordAuthentication(out string) (bool, bool) {
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.EqualFold(f[0], "passwordauthentication") {
			return strings.EqualFold(f[1], "yes"), true
		}
	}
	return false, false
}

// applyKeys writes the managed block of an authorized_keys file and
// returns the keys now in it. Keys outside of the block are kept.
func applyKeys(file string, keys []string) ([]string, error) {
	dir := filepath.Dir(file)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	// sshd (StrictModes) ignores the keys when .ssh is not owned by the
	// user or is writable by others. Nodes installed from older ISOs have
	// it owned by uid 1001 of the image build.
	if err := os.Lchown(dir, os.Getuid(), os.Getgid()); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	old, err := os.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	content := updateAuthorizedKeys(string(old), keys)
	if content != string(old) {
		if err := writeFile(file, content); err != nil {
			return nil, err
		}
		log.Printf("updated managed SSH keys in %s", file)
	}
	return managedKeys(content), nil
}

func writeFile(path, content string) error {
	tmp := path + ".fleet-tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// updateAuthorizedKeys replaces the managed block of an authorized_keys
// file. Lines outside of the block are kept as they are.
func updateAuthorizedKeys(content string, keys []string) string {
	var out []string
	inBlock := false
	for _, line := range strings.Split(strings.TrimRight(content, "\n"), "\n") {
		switch {
		case line == keysBegin:
			inBlock = true
		case line == keysEnd:
			inBlock = false
		case !inBlock && (line != "" || len(out) > 0):
			out = append(out, line)
		}
	}
	var block []string
	for _, k := range keys {
		if k = strings.TrimSpace(k); k != "" && !slices.Contains(block, k) {
			block = append(block, k)
		}
	}
	if len(block) > 0 {
		out = append(out, keysBegin)
		out = append(out, block...)
		out = append(out, keysEnd)
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n") + "\n"
}

// managedKeys returns the keys of the managed block.
func managedKeys(content string) []string {
	keys := []string{}
	inBlock := false
	for _, line := range strings.Split(content, "\n") {
		switch {
		case line == keysBegin:
			inBlock = true
		case line == keysEnd:
			inBlock = false
		case inBlock && line != "":
			keys = append(keys, line)
		}
	}
	return keys
}
