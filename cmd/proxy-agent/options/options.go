package options

import "github.com/spf13/pflag"

// ProxyOptions specifies the required parameters for running a proxy registry.
type ProxyOptions struct {
	BindAddress string `json:"bindAddress"`
	Port        int64  `json:"port"`

	// Peer is the host where a registration should happen.
	Peer string `json:"peer"`
}

// NewProxyOptions creates a default proxy options.
func NewProxyOptions() *ProxyOptions {
	return &ProxyOptions{
		BindAddress: "0.0.0.0",
		Port:        5000,
	}
}

// AddFlags parses flag values into the options itself.
func (opt *ProxyOptions) AddFlags(fs *pflag.FlagSet) {
	fs.Int64VarP(&opt.Port, "port", "p", opt.Port, "the port that the agent will listen on")
	fs.StringVarP(&opt.BindAddress, "addr", "a", opt.BindAddress, "the address that the agent will listen on")
	fs.StringVarP(&opt.Peer, "peer", "", opt.Peer, "the peer address that the agent can contact in order to form a cluster")
}
