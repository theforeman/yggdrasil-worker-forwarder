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

## Building

```
make          # build the binary into _build/
make data     # render the D-Bus policy and systemd unit
make install  # install binary, policy, unit (honours DESTDIR)
make test
make vet
```
