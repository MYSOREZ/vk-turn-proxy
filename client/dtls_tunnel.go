// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cbeuw/connutil"
	"github.com/pion/dtls/v3"
	"github.com/pion/dtls/v3/pkg/crypto/selfsign"
)

func dtlsFunc(ctx context.Context, conn net.PacketConn, peer *net.UDPAddr) (net.Conn, error) {
	certificate, err := selfsign.GenerateSelfSigned()
	if err != nil {
		return nil, err
	}
	config := &dtls.Config{
		Certificates:          []tls.Certificate{certificate},
		InsecureSkipVerify:    true,
		ExtendedMasterSecret:  dtls.RequireExtendedMasterSecret,
		CipherSuites:          []dtls.CipherSuiteID{dtls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
		ConnectionIDGenerator: dtls.OnlySendCIDGenerator(),
	}
	ctx1, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	dtlsConn, err := dtls.Client(conn, peer, config)
	if err != nil {
		return nil, err
	}

	if err := dtlsConn.HandshakeContext(ctx1); err != nil {
		return nil, err
	}
	return dtlsConn, nil
}

func oneDtlsConnection(ctx context.Context, peer *net.UDPAddr, listenConn net.PacketConn, connchan chan<- net.PacketConn, okchan chan<- struct{}, c chan<- error) {
	var err error = nil
	defer func() { c <- err }()
	dtlsctx, dtlscancel := context.WithCancel(ctx)
	defer dtlscancel()
	var conn1, conn2 net.PacketConn
	conn1, conn2 = connutil.AsyncPacketPipe()
	go func() {
		for {
			select {
			case <-dtlsctx.Done():
				return
			case connchan <- conn2:
			}
		}
	}()
	dtlsConn, err1 := dtlsFunc(dtlsctx, conn1, peer)
	if err1 != nil {
		err = fmt.Errorf("failed to connect DTLS: %s", err1)
		return
	}
	defer func() {
		if closeErr := dtlsConn.Close(); closeErr != nil {
			err = fmt.Errorf("failed to close DTLS connection: %s", closeErr)
			return
		}
		log.Printf("Closed DTLS connection\n")
	}()
	log.Printf("Established DTLS connection!\n")
	go func() {
		for {
			select {
			case <-dtlsctx.Done():
				return
			case okchan <- struct{}{}:
			}
		}
	}()

	wg := sync.WaitGroup{}
	wg.Add(2)
	context.AfterFunc(dtlsctx, func() {
		if err := listenConn.SetDeadline(time.Now()); err != nil {
			log.Printf("Failed to set listener deadline: %s", err)
		}
		if err := dtlsConn.SetDeadline(time.Now()); err != nil {
			log.Printf("Failed to set DTLS deadline: %s", err)
		}
	})
	var addr atomic.Value
	// Start read-loop on listenConn
	go func() {
		defer wg.Done()
		defer dtlscancel()
		buf := make([]byte, 1600)
		for {
			select {
			case <-dtlsctx.Done():
				return
			default:
			}
			n, addr1, err1 := listenConn.ReadFrom(buf)
			if err1 != nil {
				log.Printf("Failed: %s", err1)
				return
			}

			addr.Store(addr1) // store peer

			_, err1 = dtlsConn.Write(buf[:n])
			if err1 != nil {
				log.Printf("Failed: %s", err1)
				return
			}
		}
	}()

	// Start read-loop on dtlsConn
	go func() {
		defer wg.Done()
		defer dtlscancel()
		buf := make([]byte, 1600)
		for {
			select {
			case <-dtlsctx.Done():
				return
			default:
			}
			n, err1 := dtlsConn.Read(buf)
			if err1 != nil {
				log.Printf("Failed: %s", err1)
				return
			}
			addr1, ok := addr.Load().(net.Addr)
			if !ok {
				log.Printf("Failed: no listener ip")
				return
			}

			_, err1 = listenConn.WriteTo(buf[:n], addr1)
			if err1 != nil {
				log.Printf("Failed: %s", err1)
				return
			}
		}
	}()

	wg.Wait()
	if err := listenConn.SetDeadline(time.Time{}); err != nil {
		log.Printf("Failed to clear listener deadline: %s", err)
	}
	if err := dtlsConn.SetDeadline(time.Time{}); err != nil {
		log.Printf("Failed to clear DTLS deadline: %s", err)
	}
}
