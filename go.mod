module github.com/theforeman/yggdrasil-worker-forwarder

go 1.19

require (
	git.sr.ht/~spc/go-log v0.1.1
	github.com/pelletier/go-toml v1.9.5
	github.com/redhatinsights/yggdrasil v0.4.4
	github.com/redhatinsights/yggdrasil_v0 v0.0.0-20220216151445-6e0de0ad703b
	google.golang.org/grpc v1.58.3
)

require (
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	golang.org/x/net v0.27.0 // indirect
	golang.org/x/sys v0.26.0 // indirect
	golang.org/x/text v0.16.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20231009173412-8bfb1ae86b6c // indirect
	google.golang.org/protobuf v1.31.0 // indirect
)
replace github.com/redhatinsights/yggdrasil_v0 v0.0.0-20220216151445-6e0de0ad703b => ./yggdrasil
