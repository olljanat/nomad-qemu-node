acl {
  enabled                  = true
  token_max_expiration_ttl = "720h"
}

tls {
  ca_file                = "/etc/nomad.d/tls/nomad-agent-ca.pem"
  cert_file              = "/etc/nomad.d/tls/nomad-agent.pem"
  key_file               = "/etc/nomad.d/tls/nomad-agent-key.pem"
  http                   = true
  rpc                    = true
  verify_https_client    = false
  verify_server_hostname = true
}
