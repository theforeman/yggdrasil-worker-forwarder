module github.com/theforeman/yggdrasil-worker-forwarder

go 1.25.0

require (
	git.sr.ht/~spc/go-log v0.0.0-20210611184941-ce2f05edb627
	github.com/pelletier/go-toml v1.9.5
	github.com/redhatinsights/yggdrasil v0.4.9
	github.com/redhatinsights/yggdrasil_v0 v0.0.0-20220216151445-6e0de0ad703b
	google.golang.org/grpc v1.81.1
)

require (
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/subpop/go-log v0.1.2 // indirect
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.38.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260226221140-a57be14db171 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

// The gRPC-era yggdrasil (0.2.z) and the D-Bus-era yggdrasil (0.4.z) publish the same module
// path, so only one of them can be required directly. The 0.2.z tree is carried as a git
// submodule and aliased to yggdrasil_v0 via a directory replacement, which is the only form of
// replace that does not verify the replacement's declared module path.
replace github.com/redhatinsights/yggdrasil_v0 v0.0.0-20220216151445-6e0de0ad703b => ./yggdrasil
