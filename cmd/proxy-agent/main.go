package main

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"
	"github.com/zhouhaibing089/pull-agent/cmd/proxy-agent/options"
	"github.com/zhouhaibing089/pull-agent/pkg/cluster/serf"
	"github.com/zhouhaibing089/pull-agent/pkg/proxy"
)

var showVersion bool

var version = "{BUILD_VERSION}"

var proxyOpts *options.ProxyOptions

func init() {
	// add flags for root command
	rootCmd.Flags().BoolVarP(&showVersion, "version", "v", false, "show the version and exit")

	// add flags for serveCmd
	proxyOpts = options.NewProxyOptions()
	proxyOpts.AddFlags(serveCmd.Flags())

	rootCmd.AddCommand(serveCmd)
}

var rootCmd = &cobra.Command{
	Use:   "pull-agent",
	Short: "`pull-agent`",
	Long:  "`pull-agent`",
	Run: func(cmd *cobra.Command, args []string) {
		if showVersion {
			fmt.Printf("pull-agent github.com/zhouhaibing089/pull-agent %s\n", version)
			return
		}
		cmd.Usage()
	},
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "`serve`",
	Long:  "`serve`",
	Run: func(cmd *cobra.Command, args []string) {
		// serf agent
		cluster, err := serf.New(proxyOpts.Peer)
		if err != nil {
			log.Fatalf("failed to new cluster: %s", err)
		}
		// run the cluster loop
		go cluster.Serve()
		// run the proxy where we can
		proxy := proxy.New(proxyOpts.BindAddress, proxyOpts.Port, "/tmp/pull-agent", cluster)
		log.Fatal(proxy.ListenAndServe())
	},
}

func main() {
	rootCmd.Execute()
}
