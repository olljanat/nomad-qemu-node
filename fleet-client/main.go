// fleet-client reports the state of a Nomad QEMU node to BurFleet
// (https://github.com/burmilla/os-fleet-manager) and applies the target OS
// version and scheduled restarts it gets back, inside the node's
// maintenance window and only while it holds a maintenance lock.
//
// Before the node goes down it is drained in Nomad, so the VMs are
// rescheduled to other nodes, and once it is back it is made eligible for
// scheduling again.
//
// Based on the BurmillaOS fleet-client:
// https://github.com/burmilla/os-services/tree/v3.0.x/images/20-fleetclient
package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

var Version = "dev"

// NodeType tells BurFleet which kind of server reports, so that it does not
// apply BurmillaOS settings to it.
const NodeType = "nomad-qemu-node"

const (
	reasonOSUpgrade = "os_upgrade"
	reasonRestart   = "restart"

	// lockRenewInterval keeps the maintenance lock (30m TTL by default)
	// while a long drain or the upgrade is running.
	lockRenewInterval = 5 * time.Minute

	// retryAfterFailure keeps a failed upgrade from draining the node again
	// on every check.
	retryAfterFailure = time.Hour
)

// Desired is the response of the BurFleet config and report APIs.
type Desired struct {
	Swarm                   string   `json:"swarm"`
	Environment             string   `json:"environment"`
	OSImage                 string   `json:"os_image"`
	OSVersion               string   `json:"os_version"`
	UpgradePolicy           string   `json:"upgrade_policy"`
	CheckIntervalSeconds    int      `json:"check_interval_seconds"`
	MaintenanceWindowActive bool     `json:"maintenance_window_active"`
	MaintenanceWindowNext   string   `json:"maintenance_window_next_start"`
	MaintenanceRequired     bool     `json:"maintenance_required"`
	MaintenanceReasons      []string `json:"maintenance_reasons"`
}

// NomadStatus is the state of the local Nomad client node.
type NomadStatus struct {
	NodeID      string `json:"node_id"`
	Status      string `json:"status"`
	Eligibility string `json:"eligibility"`
	Drain       bool   `json:"drain"`
	// Allocations is the number of running allocations (VMs) on the node.
	Allocations int `json:"allocations"`
}

type Report struct {
	Hostname      string       `json:"hostname"`
	Swarm         string       `json:"swarm"`
	Environment   string       `json:"environment"`
	NodeType      string       `json:"node_type"`
	OSVersion     string       `json:"os_version"`
	KernelVersion string       `json:"kernel_version"`
	QEMUVersion   string       `json:"qemu_version"`
	NomadVersion  string       `json:"nomad_version"`
	ClientVersion string       `json:"client_version"`
	BootTime      time.Time    `json:"boot_time"`
	State         string       `json:"state"`
	Message       string       `json:"message"`
	Nomad         *NomadStatus `json:"nomad,omitempty"`
}

type client struct {
	manager    string
	api        string // base path, names the group + Environment
	user, pass string
	swarm      string
	env        string
	hostname   string
	interval   time.Duration
	http       *http.Client

	drain         bool
	drainDeadline time.Duration
	upgradeArgs   []string
	// drainMarker exists while the node is drained by fleet-client, so
	// that it is made eligible again after the reboot. It must be on
	// persistent storage.
	drainMarker string

	state, msg string
	failedAt   time.Time // last failed maintenance
	// rebooting is set once a reboot has been started: nothing more is
	// done until the system goes down.
	rebooting bool
	nomadErr  string // last error reading the Nomad status, to log changes only
}

func main() {
	c := &client{
		manager:       strings.TrimRight(os.Getenv("FLEET_MANAGER_URL"), "/"),
		swarm:         envOr("FLEET_GROUP", os.Getenv("FLEET_SWARM")),
		env:           os.Getenv("FLEET_ENVIRONMENT"),
		hostname:      os.Getenv("FLEET_HOSTNAME"),
		user:          os.Getenv("FLEET_USERNAME"),
		pass:          os.Getenv("FLEET_PASSWORD"),
		http:          &http.Client{Timeout: 30 * time.Second, Transport: transport(os.Getenv("FLEET_CA_CERT"))},
		drain:         envOr("FLEET_NOMAD_DRAIN", "true") != "false",
		drainDeadline: time.Hour,
		upgradeArgs:   strings.Fields(os.Getenv("FLEET_UPGRADE_ARGS")),
		drainMarker:   envOr("FLEET_STATE_DIR", "/data/fleet-client") + "/drained",
		state:         "idle",
	}
	if c.manager == "" {
		log.Fatal("FLEET_MANAGER_URL is not set")
	}
	if v := os.Getenv("FLEET_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			log.Fatalf("invalid FLEET_INTERVAL: %v", err)
		}
		c.interval = d
	}
	if v := os.Getenv("FLEET_NOMAD_DRAIN_DEADLINE"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			log.Fatalf("invalid FLEET_NOMAD_DRAIN_DEADLINE: %v", err)
		}
		c.drainDeadline = d
	}
	if c.hostname == "" {
		c.hostname, _ = os.Hostname()
	}
	api, err := apiBase(c.swarm, c.env)
	if err != nil {
		log.Fatal(err)
	}
	c.api = api
	log.Printf("fleet-client %s: %s (group %q, environment %q) reporting to %s", Version, c.hostname, c.swarm, c.env, c.manager)

	for {
		time.Sleep(c.run())
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// run does one report + act cycle and returns when to run the next one.
func (c *client) run() time.Duration {
	if c.rebooting {
		return time.Minute
	}
	resumeErr := c.resume()
	rep := c.collect()
	d, err := c.report(rep)
	if err != nil {
		log.Printf("report failed: %v", err)
		return time.Minute
	}
	interval := time.Duration(d.CheckIntervalSeconds) * time.Second
	if c.interval > 0 {
		interval = c.interval
	}
	if interval <= 0 {
		interval = 5 * time.Minute
	}

	state, msg := c.act(d, rep)
	if resumeErr != nil && state == "idle" {
		// Nomad is usually not up yet right after a boot: retry soon.
		state, msg = "error", "enable Nomad scheduling eligibility: "+resumeErr.Error()
		interval = min(interval, time.Minute)
	}
	if state != c.state || msg != c.msg {
		if msg != "" {
			log.Printf("%s: %s", state, msg)
		}
		c.state, c.msg = state, msg
		// Let the manager know right away, e.g. about errors.
		rep.State, rep.Message = state, msg
		if _, err := c.report(rep); err != nil {
			log.Printf("report failed: %v", err)
		}
	}
	return interval
}

// supported returns the maintenance reasons this client can act on.
// docker_upgrade does not apply to Nomad QEMU nodes.
func supported(reasons []string) []string {
	out := []string{}
	for _, r := range reasons {
		if r == reasonOSUpgrade || r == reasonRestart {
			out = append(out, r)
		}
	}
	return out
}

func (c *client) act(d *Desired, rep *Report) (string, string) {
	reasons := supported(d.MaintenanceReasons)
	if !d.MaintenanceRequired || len(reasons) == 0 {
		return "idle", ""
	}
	pending := strings.Join(reasons, ", ")
	if d.UpgradePolicy != "auto" {
		return "waiting", "pending " + pending + " (upgrade_policy " + d.UpgradePolicy + ")"
	}
	if !d.MaintenanceWindowActive {
		return "waiting", "pending " + pending + ", next maintenance window " + d.MaintenanceWindowNext
	}
	if !c.failedAt.IsZero() && time.Since(c.failedAt) < retryAfterFailure {
		return c.state, c.msg
	}
	state, msg := c.maintain(d, rep, reasons)
	if state == "error" {
		c.failedAt = time.Now()
	}
	return state, msg
}

// maintain takes the maintenance lock, drains the node and upgrades or
// restarts it.
func (c *client) maintain(d *Desired, rep *Report, reasons []string) (string, string) {
	pending := strings.Join(reasons, ", ")
	granted, why, err := c.lock(reasons[0])
	if err != nil {
		return c.state, c.msg
	}
	if !granted {
		return "waiting", "pending " + pending + ", maintenance lock refused: " + why
	}

	rep.State, rep.Message = "upgrading", "starting "+pending
	if _, err := c.report(rep); err != nil {
		log.Printf("report failed: %v", err)
	}
	log.Printf("maintenance lock granted, starting %s", pending)
	stop := c.keepLock(reasons[0])
	defer stop()

	if c.drain {
		if err := c.drainNode(); err != nil {
			c.undrain()
			return "error", "drain Nomad node: " + err.Error()
		}
	}
	if contains(reasons, reasonOSUpgrade) {
		image := d.OSImage + ":" + d.OSVersion
		if err := upgrade(image, c.upgradeArgs); err != nil {
			c.undrain()
			return "error", fmt.Sprintf("upgrade to %s: %v", image, err)
		}
		log.Printf("upgraded to %s, rebooting", image)
	} else {
		log.Print("scheduled restart")
	}
	if err := run("systemctl", "reboot"); err != nil {
		c.undrain()
		return "error", fmt.Sprintf("reboot: %v", err)
	}
	c.rebooting = true
	return "upgrading", "rebooting"
}

// undrain makes the node eligible again after a failed maintenance, so
// that it keeps serving with the current version.
func (c *client) undrain() {
	if !c.drain {
		return
	}
	if err := c.resume(); err != nil {
		log.Printf("enable Nomad scheduling eligibility: %v", err)
	}
}

// keepLock renews the maintenance lock until the returned function is
// called.
func (c *client) keepLock(reason string) func() {
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(lockRenewInterval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				if granted, why, err := c.lock(reason); err == nil && !granted {
					log.Printf("maintenance lock renewal refused: %s", why)
				}
			}
		}
	}()
	return func() { close(done) }
}

// upgrade installs image to the passive system of elemental. The new
// version is active after the next boot.
func upgrade(image string, extra []string) error {
	// /.snapshots is read-only on a running system.
	if err := run("mount", "-o", "remount,rw", "/.snapshots"); err != nil {
		log.Printf("remount /.snapshots: %v", err)
	}
	args := append([]string{"upgrade", "--system", "oci:" + image}, extra...)
	return run("elemental", args...)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	os.Stdout.Write(out.Bytes())
	if err != nil {
		return fmt.Errorf("%v: %s", err, lastLine(out.String()))
	}
	return nil
}

// capture runs a command and returns its output, with the last line of
// the output in the error.
func capture(name string, args ...string) (string, error) {
	var out, stderr bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%v: %s", err, lastLine(stderr.String()+out.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		s = s[i+1:]
	}
	return s
}

func output(name string, args ...string) string {
	out, err := capture(name, args...)
	if err != nil {
		log.Printf("%s %s: %v", name, strings.Join(args, " "), err)
	}
	return out
}

func (c *client) collect() *Report {
	rep := &Report{
		Hostname:      c.hostname,
		Swarm:         c.swarm,
		Environment:   c.env,
		NodeType:      NodeType,
		ClientVersion: Version,
		OSVersion:     osRelease("/etc/os-release")["IMAGE_TAG"],
		KernelVersion: kernelVersion(),
		QEMUVersion:   qemuVersion(output("qemu-system-x86_64", "--version")),
		NomadVersion:  nomadVersion(output("nomad", "version")),
		BootTime:      bootTime(),
		State:         c.state,
		Message:       c.msg,
	}
	st, err := nomadStatus()
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	if msg != c.nomadErr {
		if msg != "" {
			log.Printf("cannot read Nomad node status: %s", msg)
		}
		c.nomadErr = msg
	}
	rep.Nomad = st
	return rep
}

// osRelease parses an os-release file.
func osRelease(path string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || strings.HasPrefix(k, "#") {
			continue
		}
		if u, err := strconv.Unquote(v); err == nil {
			v = u
		} else {
			v = strings.Trim(v, `'"`)
		}
		out[k] = v
	}
	return out
}

// qemuVersion parses "QEMU emulator version 10.1.0 (Debian 1:10.1.0+ds-5ubuntu2)".
func qemuVersion(out string) string {
	f := strings.Fields(firstLine(out))
	for i, v := range f {
		if v == "version" && i+1 < len(f) {
			return f[i+1]
		}
	}
	return ""
}

// nomadVersion parses "Nomad v2.0.7" from the first line of nomad version.
func nomadVersion(out string) string {
	f := strings.Fields(firstLine(out))
	if len(f) < 2 || f[0] != "Nomad" {
		return ""
	}
	return strings.TrimPrefix(f[1], "v")
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}

func kernelVersion() string {
	data, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	return strings.TrimSpace(string(data))
}

func bootTime() time.Time {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return time.Time{}
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if v, ok := strings.CutPrefix(s.Text(), "btime "); ok {
			sec, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			return time.Unix(sec, 0).UTC()
		}
	}
	return time.Time{}
}

// apiBase returns the BurFleet API path of a node. The group (a standalone
// group in BurFleet) and Environment in the path are what make the node a
// member of them.
func apiBase(swarm, env string) (string, error) {
	switch {
	case swarm == "" && env == "":
		return "/api/v1", nil
	case swarm == "" || env == "":
		return "", errors.New("FLEET_GROUP and FLEET_ENVIRONMENT must be set together")
	}
	return "/api/v1/swarms/" + url.PathEscape(swarm) + "/" + url.PathEscape(env), nil
}

func (c *client) post(path string, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.manager+c.api+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.user != "" {
		req.SetBasicAuth(c.user, c.pass)
	}
	return c.http.Do(req)
}

// transport trusts the extra CA in caFile, e.g. for a BurFleet with a
// certificate from a private CA.
func transport(caFile string) http.RoundTripper {
	t := http.DefaultTransport.(*http.Transport).Clone()
	if caFile == "" {
		return t
	}
	pem, err := os.ReadFile(caFile)
	if err != nil {
		log.Fatalf("read FLEET_CA_CERT: %v", err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		log.Fatalf("no certificates in FLEET_CA_CERT %s", caFile)
	}
	t.TLSClientConfig = &tls.Config{RootCAs: pool}
	return t
}

func (c *client) report(rep *Report) (*Desired, error) {
	resp, err := c.post("/report", rep)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("fleet manager returned %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	d := &Desired{}
	return d, json.NewDecoder(resp.Body).Decode(d)
}

func (c *client) lock(reason string) (bool, string, error) {
	resp, err := c.post("/maintenance/lock", map[string]string{"hostname": c.hostname, "reason": reason})
	if err != nil {
		log.Printf("maintenance lock request failed: %v", err)
		return false, "", err
	}
	defer resp.Body.Close()
	var r struct {
		Granted bool
		Message string
	}
	_ = json.NewDecoder(resp.Body).Decode(&r)
	return resp.StatusCode == http.StatusOK && r.Granted, r.Message, nil
}
