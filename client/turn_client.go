// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/logging"
	"github.com/pion/transport/v4"
	"github.com/pion/turn/v5"
	"github.com/wlynxg/anet"
)

// androidNet переопределяет только Interfaces для обхода netlink на Android.
type androidNet struct {
	transport.Net
}

func (n *androidNet) Interfaces() ([]*transport.Interface, error) {
	ifaces, err := anet.Interfaces()
	if err != nil {
		return nil, err
	}
	res := make([]*transport.Interface, len(ifaces))
	for i, iface := range ifaces {
		res[i] = transport.NewInterface(iface)
	}
	return res, nil
}

func (n *androidNet) ResolveUDPAddr(network, address string) (*net.UDPAddr, error) {
	return net.ResolveUDPAddr(network, address)
}

func oneTurnConnection(ctx context.Context, turnParams *turnParams, peer *net.UDPAddr, conn2 net.PacketConn, c chan<- error) {
	var err error = nil
	defer func() { c <- err }()
	
	user, pass, url, err1 := turnParams.getCreds(turnParams.link)
	if err1 != nil {
		err = fmt.Errorf("creds fail: %s", err1)
		return
	}
	
	urlhost, urlport, _ := net.SplitHostPort(url)
	if turnParams.host != "" { urlhost = turnParams.host }
	if turnParams.port != "" { urlport = turnParams.port }
	
	turnServerAddr := net.JoinHostPort(urlhost, urlport)
	// Важно: Resolve сразу, чтобы в логах был IP
	resolvedAddr, _ := net.ResolveUDPAddr("udp", turnServerAddr)
	if resolvedAddr != nil {
		fmt.Printf("Connecting to TURN: %s (%s)\n", turnServerAddr, resolvedAddr.IP)
	}

	var turnConn net.PacketConn
	if turnParams.udp {
		// Используем ListenPacket (unconnected) вместо DialUDP. 
		// Это намного стабильнее для долгоживущих сессий на Android.
		conn, err2 := net.ListenPacket("udp", ":0")
		if err2 != nil {
			err = fmt.Errorf("udp listen fail: %s", err2)
			return
		}
		defer conn.Close()
		turnConn = conn
	} else {
		d := net.Dialer{Timeout: 10 * time.Second}
		conn, err2 := d.DialContext(ctx, "tcp", turnServerAddr)
		if err2 != nil {
			err = fmt.Errorf("tcp dial fail: %s", err2)
			return
		}
		defer conn.Close()
		turnConn = turn.NewSTUNConn(conn)
	}

	var addrFamily turn.RequestedAddressFamily = turn.RequestedAddressFamilyIPv4
	if peer.IP.To16() != nil && peer.IP.To4() == nil {
		addrFamily = turn.RequestedAddressFamilyIPv6
	}

	cfg := &turn.ClientConfig{
		STUNServerAddr:         turnServerAddr,
		TURNServerAddr:         turnServerAddr,
		Conn:                   turnConn,
		Username:               user,
		Password:               pass,
		RequestedAddressFamily: addrFamily,
		LoggerFactory:          logging.NewDefaultLoggerFactory(),
		Net:                    &androidNet{},
	}

	client, err1 := turn.NewClient(cfg)
	if err1 != nil {
		err = fmt.Errorf("client create fail: %s", err1)
		return
	}
	defer client.Close()

	if err1 = client.Listen(); err1 != nil {
		err = fmt.Errorf("listen fail: %s", err1)
		return
	}

	relayConn, err1 := client.Allocate()
	if err1 != nil {
		err = fmt.Errorf("allocate fail: %s", err1)
		return
	}
	defer relayConn.Close()

	log.Printf("relayed-address=%s", relayConn.LocalAddr().String())

	wg := sync.WaitGroup{}
	wg.Add(2)
	turnctx, turncancel := context.WithCancel(context.Background())
	context.AfterFunc(turnctx, func() {
		_ = relayConn.SetDeadline(time.Now())
		_ = conn2.SetDeadline(time.Now())
	})
	
	var lastAddr atomic.Value
	
	// Read DTLS -> Write TURN
	go func() {
		defer wg.Done()
		defer turncancel()
		buf := make([]byte, 1600)
		for {
			n, addr, err := conn2.ReadFrom(buf)
			if err != nil { return }
			lastAddr.Store(addr)
			// При использовании unconnected сокета, WriteTo на relayConn 
			// сам поддерживает нужные разрешения в фоне.
			_, err = relayConn.WriteTo(buf[:n], peer)
			if err != nil { return }
		}
	}()

	// Read TURN -> Write DTLS
	go func() {
		defer wg.Done()
		defer turncancel()
		buf := make([]byte, 1600)
		for {
			n, _, err := relayConn.ReadFrom(buf)
			if err != nil { return }
			if addr, ok := lastAddr.Load().(net.Addr); ok {
				_, _ = conn2.WriteTo(buf[:n], addr)
			}
		}
	}()

	wg.Wait()
}
