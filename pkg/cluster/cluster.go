package cluster

import "time"

// Interface describes the actions that the agent is supposed to be able to do.
type Interface interface {
	// StartLayer broadcast that the layer is currently downloading here.
	StartLayer(digest string, duration time.Duration)
	// EndLayer broadcast that the layer is finished downloading on this host.
	EndLayer(digest string)
	// Endpoints returns a list of nodes that can relay the layer with digest.
	Endpoints(digest string) []string
	// Serve starts the blocking routine to coordinate within the cluster.
	Serve() error
}
