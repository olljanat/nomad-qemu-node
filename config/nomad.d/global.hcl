addresses {
  http = "0.0.0.0"
}

bind_addr = "0.0.0.0"

client {
  enabled = true
  options {
    "driver.allowlist"      = "qemu,raw_exec"
    "fingerprint.allowlist" = "cpu,host,memory,network,nomad,secrets_plugins,signal,storage"
    "user.checked_drivers"  = "qemu,raw_exec"
    "user.denylist"         = ""
  }
  max_kill_timeout = "1h"
}

consul {
  client_auto_join = false
}

data_dir = "/data/nomad"

disable_update_check = true

plugin "qemu" {
}

plugin "raw_exec" {
  config {
    enabled = true
  }
}

telemetry {
  collection_interval        = "15s"
  disable_hostname           = true
  prometheus_metrics         = true
  publish_allocation_metrics = true
  publish_node_metrics       = true
}
