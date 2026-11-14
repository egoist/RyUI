//go:build !unix

package devtunnel

import "net"

func listenShared(addr string) (net.Listener, error) { return net.Listen("tcp", addr) }
