package serf

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	libserf "github.com/hashicorp/serf/serf"
	"github.com/zhouhaibing089/pull-agent/pkg/cluster"
)

// a list of user event names
const (
	EventStartLayer string = "START_LAYER"
	EventEndLayer   string = "END_LAYER"
)

// serf is a cluster.Interface implementation based on github.com/hashicorp/serf.
type serf struct {
	// channel to receive cluster event
	eventCh chan libserf.Event
	// serf agent deal with cluster invocations
	agent *libserf.Serf
	// peer is the first known peer
	peer string
	// address is the public address of current host
	// used for advertiseAddr as well as the event payload.
	address string

	// byDigest is the memory mapping state. keyed by layer digest, valued by
	// a list of nodes.
	// byNode is the memory mapping state. keyed by node address, valued by
	// a list of digests.
	// lock is used for read/write protection.
	byDigest map[string][]string
	lock     sync.Mutex
}

// a list of layer event status.
const (
	StatusStarted int = 1
	StatusEnded   int = 0
)

// LayerEvent is the payload of user event to broadcast layer information.
type LayerEvent struct {
	// Status is StatusStarted or StatusEnded
	Status int `json:"status"`
	// Digest is the layer sha256 checksum digest.
	Digest string `json:"digest"`
	// Address is the peer address.
	Address string `json:"address"`
}

// New creates a new cluster instance
func New(peer string) (cluster.Interface, error) {
	eventCh := make(chan libserf.Event, 10)
	config := libserf.DefaultConfig()
	config.EventCh = eventCh
	// advertise its public address.
	address := getAdvertiseAddr()
	config.MemberlistConfig.AdvertiseAddr = address
	// create the serf agent.
	agent, err := libserf.Create(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create serf agent: %s", err)
	}
	return &serf{
		eventCh:  eventCh,
		agent:    agent,
		peer:     peer,
		byDigest: make(map[string][]string),
	}, nil
}

var _ cluster.Interface = &serf{}

// StartLayer broadcasts a message to cluster that it can accept new requests for
// that layer. It optionally accepts a duration parameter which indicates the time
// that it will expire after.
func (s *serf) StartLayer(digest string, duration time.Duration) {
	layerEvent := &LayerEvent{
		Status:  StatusStarted,
		Digest:  digest,
		Address: s.address,
	}
	data, err := json.Marshal(layerEvent)
	if err != nil {
		log.Printf("failed to marshal start event for %s: %s", digest, err)
		return
	}
	s.agent.UserEvent(EventStartLayer, data, false)
}

// EndLayer broadcast a message to cluster that no more new requests can reach to
// this host for layer downloading.
func (s *serf) EndLayer(digest string) {
	layerEvent := &LayerEvent{
		Status:  StatusEnded,
		Digest:  digest,
		Address: s.address,
	}
	data, err := json.Marshal(layerEvent)
	if err != nil {
		log.Printf("failed to marshal end event for %s: %s", digest, err)
		return
	}
	s.agent.UserEvent(EventEndLayer, data, false)
}

// Endpoints returns a list of endpoints that can be served as a relay point for
// layer downloading.
func (s *serf) Endpoints(digest string) []string {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.byDigest[digest]
}

// Serve starts the routine, and listens events inside the cluster, specifically,
// it listens for the layer downloading(start and end) events, and refresh its
// memory state accordingly. The memory state is simply a map which is keyed by
// layer digest, and values are a list of node that are currently downloading it.
func (s *serf) Serve() error {
	// TODO: if the peer flag is empty, get the node list by kube api.
	// also, we might want to do this join periodically in case any failure
	// happened later.
	n, err := s.agent.Join([]string{s.peer}, true)
	if n != 1 {
		return fmt.Errorf("failed to contact %s", s.peer)
	}
	if err != nil {
		return fmt.Errorf("failed to join cluster: %s", err)
	}
	// process the events inside the cluster, this is supposed to be blocking.
	for {
		select {
		case event := <-s.eventCh:
			log.Printf("receive %s: %s", event.EventType(), event.String())
			// we only deal with user events
			uevent, ok := event.(libserf.UserEvent)
			if !ok {
				continue
			}
			layerEvent := &LayerEvent{}
			err := json.Unmarshal(uevent.Payload, layerEvent)
			if err != nil {
				log.Printf("failed to decode payload as layer event")
				continue
			}
			// update the memory state accordingly.
			if layerEvent.Status == StatusStarted {
				log.Printf("add layer %s from %s", layerEvent.Digest, layerEvent.Address)
				s.addNode(layerEvent.Digest, layerEvent.Address)
			}
			if layerEvent.Status == StatusEnded {
				log.Printf("remove layer %s from %s", layerEvent.Digest, layerEvent.Address)
				s.delNode(layerEvent.Digest, layerEvent.Address)
			}
		}
	}
}

// addNode appends one more nodes to the node list keyed by provided digest.
func (s *serf) addNode(digest string, node string) {
	s.lock.Lock()
	defer s.lock.Unlock()
	nodes, ok := s.byDigest[digest]
	if !ok {
		nodes = []string{}
	}
	for _, n := range nodes {
		if node == n {
			return
		}
	}
	nodes = append(nodes, node)
	s.byDigest[digest] = nodes
}

// delNode removes the node from node list keyed by provided digest.
func (s *serf) delNode(digest string, node string) {
	s.lock.Lock()
	defer s.lock.Unlock()
	nodes, ok := s.byDigest[digest]
	if !ok {
		return
	}
	newNodes := []string{}
	for _, n := range nodes {
		if n != node {
			newNodes = append(newNodes, n)
		}
	}
	s.byDigest[digest] = newNodes
}

// getAdvertiseAddr returns the public address available that can be contacted by
// peers inside cluster, and thus, it should avoid ip addresses that are only local,
// like 127.0.0.1 or docker bridge 172.20.xx.xx.
// TODO: figure out the proper way to get public ip address.
func getAdvertiseAddr() string {
	// get the ip address
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		log.Printf("failed to check interfaces: %s", err)
		return ""
	}
	ip := ""
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				ip = ipnet.IP.To4().String()
			}
			// if it is the `10.0.0.0/8`, use it
			if strings.HasPrefix(ip, "10.") {
				break
			}
		}
	}
	if ip == "" {
		log.Println("failed to get a non loopback address")
	}
	return ip
}
