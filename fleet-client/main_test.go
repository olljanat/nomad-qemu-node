package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestOSRelease(t *testing.T) {
	p := filepath.Join(t.TempDir(), "os-release")
	data := "PRETTY_NAME=\"Ubuntu 26.04 LTS\"\nID=ubuntu\n# comment\nIMAGE_TAG=\"v20261024-1\"\n"
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	r := osRelease(p)
	if r["IMAGE_TAG"] != "v20261024-1" || r["PRETTY_NAME"] != "Ubuntu 26.04 LTS" || r["ID"] != "ubuntu" {
		t.Fatalf("got %v", r)
	}
	if len(osRelease(filepath.Join(t.TempDir(), "missing"))) != 0 {
		t.Fatal("missing file should give no values")
	}
}

func TestVersions(t *testing.T) {
	if v := qemuVersion("QEMU emulator version 10.1.0 (Debian 1:10.1.0+ds-5ubuntu2)\nCopyright (c) 2003-2025 Fabrice Bellard and the QEMU Project developers"); v != "10.1.0" {
		t.Fatalf("qemu: %q", v)
	}
	if v := nomadVersion("Nomad v2.0.7\nBuildDate 2026-10-01T00:00:00Z\nRevision abc"); v != "2.0.7" {
		t.Fatalf("nomad: %q", v)
	}
	if qemuVersion("") != "" || nomadVersion("") != "" || nomadVersion("Error: x y") != "" {
		t.Fatal("empty output should give no version")
	}
}

func TestParseNode(t *testing.T) {
	st, err := parseNode([]byte(`{"ID":"n1","Status":"ready","SchedulingEligibility":"ineligible","Drain":false,"DrainStrategy":{"Deadline":3600}}`))
	if err != nil {
		t.Fatal(err)
	}
	want := &NomadStatus{NodeID: "n1", Status: "ready", Eligibility: "ineligible", Drain: true}
	if !reflect.DeepEqual(st, want) {
		t.Fatalf("got %+v", st)
	}
	st, _ = parseNode([]byte(`{"ID":"n1","Status":"ready","SchedulingEligibility":"eligible","DrainStrategy":null}`))
	if st.Drain {
		t.Fatal("no drain strategy means no drain")
	}
	if _, err := parseNode([]byte(`{}`)); err == nil {
		t.Fatal("node without ID accepted")
	}
	n, err := runningAllocations([]byte(`[{"ClientStatus":"running"},{"ClientStatus":"complete"},{"ClientStatus":"running"}]`))
	if err != nil || n != 2 {
		t.Fatalf("allocations %d %v", n, err)
	}
}

func TestSupported(t *testing.T) {
	if got := supported([]string{"os_upgrade", "docker_upgrade", "restart"}); !reflect.DeepEqual(got, []string{"os_upgrade", "restart"}) {
		t.Fatalf("got %v", got)
	}
	if got := supported([]string{"docker_upgrade"}); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestAPIBase(t *testing.T) {
	if p, _ := apiBase("", ""); p != "/api/v1" {
		t.Fatal(p)
	}
	if p, _ := apiBase("nomad dc1", "prod"); p != "/api/v1/swarms/nomad%20dc1/prod" {
		t.Fatal(p)
	}
	if _, err := apiBase("g", ""); err == nil {
		t.Fatal("group without environment accepted")
	}
}

// Without pending maintenance, or with a policy other than auto, the
// client never asks for a lock.
func TestActWithoutLock(t *testing.T) {
	locks := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		locks++
		_ = json.NewEncoder(w).Encode(map[string]any{"granted": false, "message": "no"})
	}))
	defer ts.Close()
	c := &client{manager: ts.URL, api: "/api/v1", http: ts.Client(), hostname: "n1", state: "idle"}

	cases := []struct {
		d     Desired
		state string
	}{
		{Desired{UpgradePolicy: "auto", MaintenanceReasons: []string{}}, "idle"},
		{Desired{UpgradePolicy: "auto", MaintenanceRequired: true, MaintenanceReasons: []string{"docker_upgrade"}}, "idle"},
		{Desired{UpgradePolicy: "none", MaintenanceRequired: true, MaintenanceReasons: []string{"os_upgrade"}}, "waiting"},
		{Desired{UpgradePolicy: "download", MaintenanceRequired: true, MaintenanceReasons: []string{"os_upgrade"}}, "waiting"},
		{Desired{UpgradePolicy: "auto", MaintenanceRequired: true, MaintenanceReasons: []string{"restart"}, MaintenanceWindowNext: "2026-10-24T23:00:00Z"}, "waiting"},
	}
	for i, tc := range cases {
		if state, msg := c.act(&tc.d, &Report{}); state != tc.state {
			t.Errorf("case %d: got %s (%s), want %s", i, state, msg, tc.state)
		}
	}
	if locks != 0 {
		t.Fatalf("%d lock requests", locks)
	}

	// Inside the window a refused lock keeps the node waiting.
	d := Desired{UpgradePolicy: "auto", MaintenanceRequired: true, MaintenanceWindowActive: true, MaintenanceReasons: []string{"os_upgrade"}}
	if state, msg := c.act(&d, &Report{}); state != "waiting" || msg != "pending os_upgrade, maintenance lock refused: no" {
		t.Fatalf("got %s: %s", state, msg)
	}
}

func TestResumeWithoutMarker(t *testing.T) {
	c := &client{drainMarker: filepath.Join(t.TempDir(), "drained")}
	if err := c.resume(); err != nil {
		t.Fatal(err)
	}
}
