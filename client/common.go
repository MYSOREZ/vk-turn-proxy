// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package main

import (
	"net"
)

type getCredsFunc func(string) (string, string, string, error)

type turnParams struct {
	host     string
	port     string
	link     string
	udp      bool
	getCreds getCredsFunc
}

type connectedUDPConn struct {
	*net.UDPConn
}

func (c *connectedUDPConn) WriteTo(p []byte, _ net.Addr) (int, error) {
	return c.Write(p)
}
