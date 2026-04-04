// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"log"
	"net"
	"strings"
	"time"
)

func oneDtlsConnectionLoop(ctx context.Context, peer *net.UDPAddr, listenConnChan <-chan net.PacketConn, connchan chan<- net.PacketConn, okchan chan<- struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case listenConn := <-listenConnChan:
			c := make(chan error)
			go oneDtlsConnection(ctx, peer, listenConn, connchan, okchan, c)
			if err := <-c; err != nil {
				log.Printf("DTLS Loop Error: %s", err)
			}
		}
	}
}

func oneTurnConnectionLoop(ctx context.Context, turnParams *turnParams, peer *net.UDPAddr, connchan <-chan net.PacketConn, t <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case conn2 := <-connchan:
			select {
			case <-t:
				c := make(chan error)
				go oneTurnConnection(ctx, turnParams, peer, conn2, c)
				err := <-c
				if err != nil {
					// Если ВК требует капчу, ждем минуту, чтобы не спамить
					if strings.Contains(err.Error(), "CAPTCHA_WAIT_REQUIRED") {
						log.Printf("!!! VK CAPTCHA DETECTED. Waiting 60s to avoid spam...")
						select {
						case <-ctx.Done():
							return
						case <-time.After(60 * time.Second):
						}
					} else {
						log.Printf("TURN Connection Error: %s", err)
						// Обычная пауза при ошибке
						time.Sleep(2 * time.Second)
					}
				}
			default:
			}
		}
	}
}
