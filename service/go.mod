module github.com/adityashantanu/substrate-agents-tco/service

go 1.27.0

require (
	github.com/agent-substrate/substrate v0.0.0
	google.golang.org/grpc v1.83.2
)

require (
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260720211330-0afa2a65878a // indirect
	google.golang.org/protobuf v1.36.12-0.20260120151049-f2248ac996af // indirect
)

// Built against the sibling checkout; the public module proxy may lag or
// diverge from the cluster build you are running against.
replace github.com/agent-substrate/substrate => ../../substrate
