# ygg-forwarder-worker

A worker for the yggdrasil daemon that forwards requests as a simple POST.

It runs on the Foreman/Satellite server as part of Cloud Connector, and forwards
messages received from the Red Hat Hybrid Cloud Console to
`/api/v2/rh_cloud/cloud_request`, where Foreman turns them into remote execution
jobs.

## Transports

The worker speaks both yggdrasil IPC protocols from a single binary and picks
one at startup:

| yggdrasil | IPC | Selected when | Ships on |
|---|---|---|---|
| 0.2.z | gRPC | `YGG_SOCKET_ADDR` is set in the environment | EL7, EL8, EL9 < 9.5 (Foreman client repos) |
| 0.4.z | D-Bus | `YGG_SOCKET_ADDR` is absent | EL9.5+, EL10 (EL repos) |

yggdrasil 0.2.z execs its workers and sets `YGG_SOCKET_ADDR`; 0.4.z does not
start workers at all — they are standalone D-Bus activated systemd services — so
the variable's presence is a reliable signal.

Under D-Bus the worker claims `com.redhat.Yggdrasil1.Worker1.foreman_rh_cloud`
and exports `/com/redhat/Yggdrasil1/Worker1/foreman_rh_cloud`.

Because the gRPC and D-Bus yggdrasil releases publish the same Go module path,
the 0.2.z tree is carried as a git submodule and aliased to `yggdrasil_v0` with
a directory `replace`. Clone with submodules:

```
git clone --recurse-submodules https://github.com/theforeman/yggdrasil-worker-forwarder
# or, in an existing clone:
git submodule update --init
```

## Configuration

Configuration is read from a TOML file whose `env` array is exported into the
process environment. The file is named by `CONFIG_FILE`, which the caller is
expected to set: under gRPC yggdrasil execs the worker with it set, and under
D-Bus the systemd unit sets it directly to `/etc/rhc/workers/foreman_rh_cloud.toml`
via `Environment=`. `CONFIG_FILE` being unset is not an error, since the same
values may be supplied directly through the environment instead.

```toml
env = [
  "FORWARDER_USER=cloud_connector_user",
  "FORWARDER_PASSWORD=...",
  "FORWARDER_URL=https://satellite.example.com/api/v2/rh_cloud/cloud_request",
  "FORWARDER_HANDLER=foreman_rh_cloud",
  "FORWARDER_CA_FILE=/etc/pki/katello/certs/katello-server-ca.crt",
]
```

| Variable | Required | Meaning |
|---|---|---|
| `FORWARDER_URL` | yes | Foreman cloud_request endpoint to POST to |
| `FORWARDER_USER` | yes | Foreman user for basic auth |
| `FORWARDER_PASSWORD` | yes | Password for that user |
| `FORWARDER_HANDLER` | no | Directive to register as, default `foreman_rh_cloud` |
| `FORWARDER_CA_FILE` | no | PEM CA bundle used to verify the Foreman TLS certificate instead of the system trust store |
| `YGG_LOG_LEVEL` | no | Log level, overridden by `-log-level` |

## Building

```
make          # build the binary into _build/
make data     # render the D-Bus policy and systemd unit
make install  # install binary, policy, unit (honours DESTDIR)
make test
make vet
```
