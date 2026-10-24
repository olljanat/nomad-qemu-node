package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// The nomad CLI is used for everything Nomad related, so the address, TLS
// settings and ACL token come from the usual NOMAD_ADDR, NOMAD_CACERT,
// NOMAD_TOKEN, ... environment variables.

// nomadStatus reads the state of the local Nomad client node. It needs a
// token with node:read when ACLs are enabled.
func nomadStatus() (*NomadStatus, error) {
	out, err := capture("nomad", "node", "status", "-self", "-json")
	if err != nil {
		return nil, err
	}
	st, err := parseNode([]byte(out))
	if err != nil {
		return nil, err
	}
	out, err = capture("nomad", "operator", "api", "/v1/node/"+st.NodeID+"/allocations")
	if err != nil {
		return nil, err
	}
	if st.Allocations, err = runningAllocations([]byte(out)); err != nil {
		return nil, err
	}
	return st, nil
}

// parseNode parses the output of nomad node status -json.
func parseNode(data []byte) (*NomadStatus, error) {
	var n struct {
		ID                    string
		Status                string
		SchedulingEligibility string
		Drain                 bool
		DrainStrategy         *json.RawMessage
	}
	if err := json.Unmarshal(data, &n); err != nil {
		return nil, fmt.Errorf("parse node status: %w", err)
	}
	if n.ID == "" {
		return nil, fmt.Errorf("parse node status: no node ID")
	}
	return &NomadStatus{
		NodeID:      n.ID,
		Status:      n.Status,
		Eligibility: n.SchedulingEligibility,
		Drain:       n.Drain || n.DrainStrategy != nil,
	}, nil
}

func runningAllocations(data []byte) (int, error) {
	var allocs []struct{ ClientStatus string }
	if err := json.Unmarshal(data, &allocs); err != nil {
		return 0, fmt.Errorf("parse allocations: %w", err)
	}
	n := 0
	for _, a := range allocs {
		if a.ClientStatus == "running" {
			n++
		}
	}
	return n, nil
}

// drainNode moves the allocations of the node to other nodes and waits
// until the drain is done. System jobs keep running until the reboot. The
// marker is written first, so the node is made eligible again even when
// fleet-client is interrupted.
func (c *client) drainNode() error {
	if err := os.MkdirAll(filepath.Dir(c.drainMarker), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(c.drainMarker, nil, 0o600); err != nil {
		return err
	}
	return run("nomad", "node", "drain", "-self", "-enable", "-yes", "-ignore-system",
		"-deadline", c.drainDeadline.String())
}

// resume makes the node eligible for scheduling again after fleet-client
// drained it. Nodes which were drained by someone else are left alone.
func (c *client) resume() error {
	if _, err := os.Stat(c.drainMarker); err != nil {
		return nil
	}
	if st, err := nomadStatus(); err != nil {
		return err
	} else if st.Drain {
		// -disable also makes the node eligible.
		if err := run("nomad", "node", "drain", "-self", "-disable", "-yes"); err != nil {
			return err
		}
	} else if err := run("nomad", "node", "eligibility", "-self", "-enable"); err != nil {
		return err
	}
	return os.Remove(c.drainMarker)
}
