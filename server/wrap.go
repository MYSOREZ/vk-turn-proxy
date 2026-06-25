// SPDX-License-Identifier: MIT

package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"time"

	dtlsnet "github.com/pion/dtls/v3/pkg/net"
	pionudp "github.com/pion/transport/v4/udp"
	"golang.org/x/crypto/hkdf"
)

const wrapKeyLen = 32

// maxObfsOverhead: 12 (RTP header) + 16 (AEAD tag) + 24 (max padding) + 1 (padLen) = 53
const maxObfsOverhead = 53

func deriveWrapKey(password string) ([]byte, error) {
	key := make([]byte, wrapKeyLen)
	r := hkdf.New(sha256.New, []byte(password), []byte("VK-TURN-WRAP-v1"), []byte("rtp-obfs/chacha20poly1305"))
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, fmt.Errorf("wrap: hkdf: %w", err)
	}
	return key, nil
}

func listenWrapped(addr *net.UDPAddr, key []byte) (dtlsnet.PacketListener, error) {
	if len(key) != wrapKeyLen {
		return nil, fmt.Errorf("wrap: key must be %d bytes (got %d)", wrapKeyLen, len(key))
	}
	inner, err := pionudp.Listen("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("wrap: udp listen: %w", err)
	}
	return &wrapPacketListener{
		inner: dtlsnet.PacketListenerFromListener(inner),
		key:   key,
	}, nil
}

type wrapPacketListener struct {
	inner dtlsnet.PacketListener
	key   []byte
}

func (l *wrapPacketListener) Accept() (net.PacketConn, net.Addr, error) {
	pc, addr, err := l.inner.Accept()
	if err != nil {
		return pc, addr, err
	}
	return &wrapPacketConn{
		inner:     pc,
		key:       l.key,
		obfsCfg:   NewObfsConfig(),
		obfsState: NewObfsState(),
	}, addr, nil
}

func (l *wrapPacketListener) Close() error   { return l.inner.Close() }
func (l *wrapPacketListener) Addr() net.Addr { return l.inner.Addr() }

type wrapPacketConn struct {
	inner     net.PacketConn
	key       []byte
	obfsCfg   *ObfsConfig
	obfsState *ObfsState
}

func (c *wrapPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	buf := make([]byte, len(p)+maxObfsOverhead)
	n, addr, err := c.inner.ReadFrom(buf)
	if err != nil {
		return 0, addr, err
	}
	plain, err := obfsUnwrapPacket(c.key, buf[:n], p)
	if err != nil {
		return 0, addr, fmt.Errorf("wrap: unwrap: %w", err)
	}
	return plain, addr, nil
}

func (c *wrapPacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	out, err := obfsWrapPacket(c.key, p, c.obfsCfg, c.obfsState)
	if err != nil {
		return 0, fmt.Errorf("wrap: wrap: %w", err)
	}
	if _, err := c.inner.WriteTo(out, addr); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *wrapPacketConn) Close() error                       { return c.inner.Close() }
func (c *wrapPacketConn) LocalAddr() net.Addr                { return c.inner.LocalAddr() }
func (c *wrapPacketConn) SetDeadline(t time.Time) error      { return c.inner.SetDeadline(t) }
func (c *wrapPacketConn) SetReadDeadline(t time.Time) error  { return c.inner.SetReadDeadline(t) }
func (c *wrapPacketConn) SetWriteDeadline(t time.Time) error { return c.inner.SetWriteDeadline(t) }
