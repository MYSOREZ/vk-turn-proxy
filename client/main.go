// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cbeuw/connutil"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/pion/dtls/v3"
	"github.com/pion/dtls/v3/pkg/crypto/selfsign"
	"github.com/pion/transport/v4/vnet"
	"github.com/pion/turn/v5"
)

type getCredsFunc func(string) (string, string, string, error)

func getVkCreds(link string) (string, string, string, error) {
	jar, _ := cookiejar.New(nil)

	userAgent := getRandomProfile().UserAgent // Рандомный юзер-агент
	generatedName := generateName("r")        // Рандомное имя

	doRequest := func(data string, url string) (resp map[string]interface{}, err error) {
		dialer := &net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}

		dialer.Resolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				// Пытаемся 2 раза достучаться до Яндекса (на случай потери пакета)
				for i := 0; i < 2; i++ {
					conn, err := net.Dial("udp", "77.88.8.8:53")
					if err == nil {
						return conn, nil
					}
					time.Sleep(200 * time.Millisecond) // Микро-пауза перед повтором
				}
				// Если Яндекс не ответил, пробуем Google как последний шанс
				return net.Dial("udp", "8.8.8.8:53")
			},
		}

		client := &http.Client{
			Timeout: 25 * time.Second, // Чуть больше времени для мобильной сети
			Jar:     jar,
			Transport: &http.Transport{
				DialContext:         dialer.DialContext,
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
		}
		defer client.CloseIdleConnections()

		req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte(data)))
		if err != nil {
			return nil, err
		}

		req.Header.Add("User-Agent", userAgent)
		req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

		httpResp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer httpResp.Body.Close()

		body, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return nil, err
		}

		err = json.Unmarshal(body, &resp)
		if err != nil {
			return nil, err
		}

		if errResp, hasErr := resp["error"]; hasErr {
			return nil, fmt.Errorf("VK API Error: %v", errResp)
		}

		return resp, nil
	}

	var resp map[string]interface{}
	var err error

	// Шаг 1: Получаем куки сессии (Jar)
	data := "client_secret=QbYic1K3lEV5kTGiqlq2&client_id=6287487&scopes=audio_anonymous%2Cvideo_anonymous%2Cphotos_anonymous%2Cprofile_anonymous&isApiOauthAnonymEnabled=false&version=1&app_id=6287487"
	url := "https://login.vk.com/?act=get_anonym_token"
	resp, err = doRequest(data, url)
	if err != nil {
		return "", "", "", fmt.Errorf("step 1 error: %s", err)
	}

	// Шаг 2: Токен сообщений
	data = "client_id=6287487&token_type=messages&client_secret=QbYic1K3lEV5kTGiqlq2&version=1&app_id=6287487"
	url = "https://login.vk.com/?act=get_anonym_token"
	resp, err = doRequest(data, url)
	if err != nil {
		return "", "", "", fmt.Errorf("step 2 error: %s", err)
	}
	token3 := resp["data"].(map[string]interface{})["access_token"].(string)

	// Шаг 3: Токен звонка
	data = fmt.Sprintf("vk_join_link=https://vk.com/call/join/%s&name=%s&access_token=%s", link, generatedName, token3)
	url = "https://api.vk.com/method/calls.getAnonymousToken?v=5.264"
	resp, err = doRequest(data, url)
	if err != nil {
		return "", "", "", fmt.Errorf("step 3 error: %s", err)
	}
	token4 := resp["response"].(map[string]interface{})["token"].(string)

	// Шаг 4: Авторизация в медиа-серверах ВК
	data = fmt.Sprintf("session_data=%%7B%%22version%%22%%3A2%%2C%%22device_id%%22%%3A%%22%s%%22%%2C%%22client_version%%22%%3A1.1%%2C%%22client_type%%22%%3A%%22SDK_JS%%22%%7D&method=auth.anonymLogin&format=JSON&application_key=CGMMEJLGDIHBABABA", uuid.New())
	url = "https://calls.okcdn.ru/fb.do"
	resp, err = doRequest(data, url)
	if err != nil {
		return "", "", "", fmt.Errorf("step 4 error: %s", err)
	}
	token5 := resp["session_key"].(string)

	// Шаг 5: Получение параметров TURN-сервера
	data = fmt.Sprintf("joinLink=%s&isVideo=false&protocolVersion=5&anonymToken=%s&method=vchat.joinConversationByLink&format=JSON&application_key=CGMMEJLGDIHBABABA&session_key=%s", link, token4, token5)
	url = "https://calls.okcdn.ru/fb.do"
	resp, err = doRequest(data, url)
	if err != nil {
		return "", "", "", fmt.Errorf("step 5 error: %s", err)
	}

	user := resp["turn_server"].(map[string]interface{})["username"].(string)
	pass := resp["turn_server"].(map[string]interface{})["credential"].(string)
	turn := resp["turn_server"].(map[string]interface{})["urls"].([]interface{})[0].(string)

	clean := strings.Split(turn, "?")[0]
	address := strings.TrimPrefix(strings.TrimPrefix(clean, "turn:"), "turns:")

	return user, pass, address, nil
}

func getYandexCreds(link string) (string, string, string, error) {
	const debug = false
	const telemostConfHost = "cloud-api.yandex.ru"
	telemostConfPath := fmt.Sprintf("%s%s%s", "/telemost_front/v2/telemost/conferences/https%3A%2F%2Ftelemost.yandex.ru%2Fj%2F", link, "/connection?next_gen_media_platform_allowed=false")
	userAgent := getRandomProfile().UserAgent // Рандомный юзер-агент
	generatedName := generateName("r")        // Рандомное имя

	type ConferenceResponse struct {
		URI                 string `json:"uri"`
		RoomID              string `json:"room_id"`
		PeerID              string `json:"peer_id"`
		ClientConfiguration struct {
			MediaServerURL string `json:"media_server_url"`
		} `json:"client_configuration"`
		Credentials string `json:"credentials"`
	}

	type PartMeta struct {
		Name        string `json:"name"`
		Role        string `json:"role"`
		Description string `json:"description"`
		SendAudio   bool   `json:"sendAudio"`
		SendVideo   bool   `json:"sendVideo"`
	}

	type PartAttrs struct {
		Name        string `json:"name"`
		Role        string `json:"role"`
		Description string `json:"description"`
	}

	type SdkInfo struct {
		Implementation string `json:"implementation"`
		Version        string `json:"version"`
		UserAgent      string `json:"userAgent"`
		HwConcurrency  int    `json:"hwConcurrency"`
	}

	type Capabilities struct {
		OfferAnswerMode             []string `json:"offerAnswerMode"`
		InitialSubscriberOffer      []string `json:"initialSubscriberOffer"`
		SlotsMode                   []string `json:"slotsMode"`
		SimulcastMode               []string `json:"simulcastMode"`
		SelfVadStatus               []string `json:"selfVadStatus"`
		DataChannelSharing          []string `json:"dataChannelSharing"`
		VideoEncoderConfig          []string `json:"videoEncoderConfig"`
		DataChannelVideoCodec       []string `json:"dataChannelVideoCodec"`
		BandwidthLimitationReason   []string `json:"bandwidthLimitationReason"`
		SdkDefaultDeviceManagement  []string `json:"sdkDefaultDeviceManagement"`
		JoinOrderLayout             []string `json:"joinOrderLayout"`
		PinLayout                   []string `json:"pinLayout"`
		SendSelfViewVideoSlot       []string `json:"sendSelfViewVideoSlot"`
		ServerLayoutTransition      []string `json:"serverLayoutTransition"`
		SdkPublisherOptimizeBitrate []string `json:"sdkPublisherOptimizeBitrate"`
		SdkNetworkLostDetection     []string `json:"sdkNetworkLostDetection"`
		SdkNetworkPathMonitor       []string `json:"sdkNetworkPathMonitor"`
		PublisherVp9                []string `json:"publisherVp9"`
		SvcMode                     []string `json:"svcMode"`
		SubscriberOfferAsyncAck     []string `json:"subscriberOfferAsyncAck"`
		SvcModes                    []string `json:"svcModes"`
		ReportTelemetryModes        []string `json:"reportTelemetryModes"`
		KeepDefaultDevicesModes     []string `json:"keepDefaultDevicesModes"`
	}

	type HelloPayload struct {
		ParticipantMeta        PartMeta     `json:"participantMeta"`
		ParticipantAttributes  PartAttrs    `json:"participantAttributes"`
		SendAudio              bool         `json:"sendAudio"`
		SendVideo              bool         `json:"sendVideo"`
		SendSharing            bool         `json:"sendSharing"`
		ParticipantID          string       `json:"participantId"`
		RoomID                 string       `json:"roomId"`
		ServiceName            string       `json:"serviceName"`
		Credentials            string       `json:"credentials"`
		CapabilitiesOffer      Capabilities `json:"capabilitiesOffer"`
		SdkInfo                SdkInfo      `json:"sdkInfo"`
		SdkInitializationID    string       `json:"sdkInitializationId"`
		DisablePublisher       bool         `json:"disablePublisher"`
		DisableSubscriber      bool         `json:"disableSubscriber"`
		DisableSubscriberAudio bool         `json:"disableSubscriberAudio"`
	}

	type HelloRequest struct {
		UID   string       `json:"uid"`
		Hello HelloPayload `json:"hello"`
	}

	type FlexUrls []string

	type WSSResponse struct {
		UID         string `json:"uid"`
		ServerHello struct {
			RtcConfiguration struct {
				IceServers []struct {
					Urls       FlexUrls `json:"urls"`
					Username   string   `json:"username,omitempty"`
					Credential string   `json:"credential,omitempty"`
				} `json:"iceServers"`
			} `json:"rtcConfiguration"`
		} `json:"serverHello"`
	}

	type WSSAck struct {
		Uid string `json:"uid"`
		Ack struct {
			Status struct {
				Code string `json:"code"`
			} `json:"status"`
		} `json:"ack"`
	}

	type WSSData struct {
		ParticipantId string
		RoomId        string
		Credentials   string
		Wss           string
	}

	endpoint := "https://" + telemostConfHost + telemostConfPath
	client := &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     90 * time.Second,
		},
	}
	defer client.CloseIdleConnections()
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Referer", "https://telemost.yandex.ru/")
	req.Header.Set("Origin", "https://telemost.yandex.ru")
	req.Header.Set("Client-Instance-Id", uuid.New().String())

	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", "", "", fmt.Errorf("GetConference: status=%s body=%s", resp.Status, string(body))
	}

	var result ConferenceResponse
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", "", fmt.Errorf("decode conf: %v", err)
	}
	data := WSSData{
		ParticipantId: result.PeerID,
		RoomId:        result.RoomID,
		Credentials:   result.Credentials,
		Wss:           result.ClientConfiguration.MediaServerURL,
	}
	h := http.Header{}
	h.Set("Origin", "https://telemost.yandex.ru")
	h.Set("User-Agent", userAgent)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	dialer := websocket.Dialer{}
	conn, _, err := dialer.DialContext(ctx, data.Wss, h)
	if err != nil {
		return "", "", "", fmt.Errorf("ws dial: %w", err)
	}
	defer conn.Close()

	req1 := HelloRequest{
		UID: uuid.New().String(),
		Hello: HelloPayload{
			ParticipantMeta: PartMeta{
				Name:        generatedName,
				Role:        "SPEAKER",
				Description: "",
				SendAudio:   false,
				SendVideo:   false,
			},
			ParticipantAttributes: PartAttrs{
				Name:        generatedName,
				Role:        "SPEAKER",
				Description: "",
			},
			SendAudio:   false,
			SendVideo:   false,
			SendSharing: false,

			ParticipantID: data.ParticipantId,
			RoomID:        data.RoomId,
			ServiceName:   "telemost",
			Credentials:   data.Credentials,
			SdkInfo: SdkInfo{
				Implementation: "browser",
				Version:        "5.15.0",
				UserAgent:      userAgent,
				HwConcurrency:  4,
			},
			SdkInitializationID:    uuid.New().String(),
			DisablePublisher:       false,
			DisableSubscriber:      false,
			DisableSubscriberAudio: false,
			CapabilitiesOffer: Capabilities{
				OfferAnswerMode:             []string{"SEPARATE"},
				InitialSubscriberOffer:      []string{"ON_HELLO"},
				SlotsMode:                   []string{"FROM_CONTROLLER"},
				SimulcastMode:               []string{"DISABLED"},
				SelfVadStatus:               []string{"FROM_SERVER"},
				DataChannelSharing:          []string{"TO_RTP"},
				VideoEncoderConfig:          []string{"NO_CONFIG"},
				DataChannelVideoCodec:       []string{"VP8"},
				BandwidthLimitationReason:   []string{"BANDWIDTH_REASON_DISABLED"},
				SdkDefaultDeviceManagement:  []string{"SDK_DEFAULT_DEVICE_MANAGEMENT_DISABLED"},
				JoinOrderLayout:             []string{"JOIN_ORDER_LAYOUT_DISABLED"},
				PinLayout:                   []string{"PIN_LAYOUT_DISABLED"},
				SendSelfViewVideoSlot:       []string{"SEND_SELF_VIEW_VIDEO_SLOT_DISABLED"},
				ServerLayoutTransition:      []string{"SERVER_LAYOUT_TRANSITION_DISABLED"},
				SdkPublisherOptimizeBitrate: []string{"SDK_PUBLISHER_OPTIMIZE_BITRATE_DISABLED"},
				SdkNetworkLostDetection:     []string{"SDK_NETWORK_LOST_DETECTION_DISABLED"},
				SdkNetworkPathMonitor:       []string{"SDK_NETWORK_PATH_MONITOR_DISABLED"},
				PublisherVp9:                []string{"PUBLISH_VP9_DISABLED"},
				SvcMode:                     []string{"SVC_MODE_DISABLED"},
				SubscriberOfferAsyncAck:     []string{"SUBSCRIBER_OFFER_ASYNC_ACK_DISABLED"},
				SvcModes:                    []string{"FALSE"},
				ReportTelemetryModes:        []string{"TRUE"},
				KeepDefaultDevicesModes:     []string{"TRUE"},
			},
		},
	}

	if debug {
		b, _ := json.MarshalIndent(req1, "", "  ")
		log.Printf("Sending HELLO:\n%s", string(b))
	}

	if err := conn.WriteJSON(req1); err != nil {
		return "", "", "", fmt.Errorf("ws write: %w", err)
	}

	conn.SetReadDeadline(time.Now().Add(15 * time.Second))

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return "", "", "", fmt.Errorf("ws read: %w", err)
		}
		if debug {
			s := string(msg)
			if len(s) > 800 {
				s = s[:800] + "...(truncated)"
			}
			log.Printf("WSS recv: %s", s)
		}

		var ack WSSAck
		if err := json.Unmarshal(msg, &ack); err == nil && ack.Ack.Status.Code != "" {
			continue
		}

		var resp WSSResponse
		if err := json.Unmarshal(msg, &resp); err == nil {
			ice := resp.ServerHello.RtcConfiguration.IceServers
			for _, s := range ice {
				for _, u := range s.Urls {
					if !strings.HasPrefix(u, "turn:") && !strings.HasPrefix(u, "turns:") {
						continue
					}
					if strings.Contains(u, "transport=tcp") {
						continue
					}
					clean := strings.Split(u, "?")[0]
					address := strings.TrimPrefix(strings.TrimPrefix(clean, "turn:"), "turns:")

					return s.Username, s.Credential, address, nil
				}
			}
		}
	}
}

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
		listenConn.SetDeadline(time.Now())
		dtlsConn.SetDeadline(time.Now())
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
	listenConn.SetDeadline(time.Time{})
	dtlsConn.SetDeadline(time.Time{})
}

type connectedUDPConn struct {
	*net.UDPConn
}

func (c *connectedUDPConn) WriteTo(p []byte, _ net.Addr) (int, error) {
	return c.Write(p)
}

type turnParams struct {
	host     string
	port     string
	link     string
	udp      bool
	getCreds getCredsFunc
}

func oneTurnConnection(ctx context.Context, turnParams *turnParams, peer *net.UDPAddr, conn2 net.PacketConn, c chan<- error) {
	var err error = nil
	defer func() { c <- err }()

	// 1. Быстрый Jitter
	time.Sleep(time.Duration(time.Now().UnixNano()%300) * time.Millisecond)

	user, pass, url, err1 := turnParams.getCreds(turnParams.link)
	if err1 != nil {
		return
	}

	urlhost, urlport, _ := net.SplitHostPort(url)
	turnServerAddr := net.JoinHostPort(urlhost, urlport)
	turnServerUdpAddr, _ := net.ResolveUDPAddr("udp", turnServerAddr)

	conn, err2 := net.DialUDP("udp", nil, turnServerUdpAddr)
	if err2 != nil {
		return
	}
	defer conn.Close()

	// Запоминаем текущий локальный IP для детекции смены сети
	initialIP := ""
	if ifaces, errI := net.Interfaces(); errI == nil {
		initialIP = fmt.Sprintf("%v", ifaces)
	}

	vNet, _ := vnet.NewNet(&vnet.NetConfig{})
	cfg := &turn.ClientConfig{
		STUNServerAddr: turnServerAddr,
		TURNServerAddr: turnServerAddr,
		Conn:           &connectedUDPConn{conn},
		Username:       user,
		Password:       pass,
		Net:            vNet,
	}

	client, err1 := turn.NewClient(cfg)
	if err1 != nil {
		return
	}
	defer client.Close()
	_ = client.Listen()

	relayConn, err1 := client.Allocate()
	if err1 != nil {
		return
	}
	defer relayConn.Close()

	log.Printf("SUCCESS: %s", relayConn.LocalAddr().String())

	wg := sync.WaitGroup{}
	wg.Add(3) // +1 для IP Watcher
	turnctx, turncancel := context.WithCancel(context.Background())
	defer turncancel()

	// 2. IP WATCHER: Детектор смены сети (Wi-Fi <-> LTE)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-turnctx.Done():
				return
			case <-ticker.C:
				if ifaces, errI := net.Interfaces(); errI == nil {
					currentIP := fmt.Sprintf("%v", ifaces)
					if currentIP != initialIP {
						log.Printf("NETWORK CHANGE DETECTED! Force restart...")
						turncancel() // Мгновенно убиваем текущий туннель
						return
					}
				}
			}
		}
	}()

	var addr atomic.Value

	// 3. Улучшенный Worker с таймаутами
	worker := func(readConn net.PacketConn, writeConn net.PacketConn, toPeer bool) {
		defer wg.Done()
		defer turncancel()
		buf := make([]byte, 1600)
		for {
			select {
			case <-turnctx.Done():
				return
			default:
				// Заставляем сокет просыпаться, чтобы проверить статус turnctx
				readConn.SetReadDeadline(time.Now().Add(10 * time.Second))

				n, addr1, err1 := readConn.ReadFrom(buf)
				if err1 != nil {
					if nerr, ok := err1.(net.Error); ok && nerr.Timeout() {
						continue // Просто проснулись по таймауту, идем на новый круг
					}
					return
				}

				var target net.Addr
				if toPeer {
					addr.Store(addr1)
					target = peer
				} else {
					a, ok := addr.Load().(net.Addr)
					if !ok {
						continue
					}
					target = a
				}
				_, _ = writeConn.WriteTo(buf[:n], target)
			}
		}
	}

	go worker(conn2, relayConn, true)
	go worker(relayConn, conn2, false)
	wg.Wait()
}

func oneDtlsConnectionLoop(ctx context.Context, peer *net.UDPAddr, listenConnChan <-chan net.PacketConn, connchan chan<- net.PacketConn, okchan chan<- struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case listenConn := <-listenConnChan:
			c := make(chan error)
			go oneDtlsConnection(ctx, peer, listenConn, connchan, okchan, c)
			if err := <-c; err != nil {
				log.Printf("%s", err)
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
				if err := <-c; err != nil {
					log.Printf("%s", err)
				}
			default:
			}
		}
	}
}

func main() { //nolint:cyclop
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-signalChan
		log.Printf("Terminating...\n")
		cancel()
		select {
		case <-signalChan:
		case <-time.After(5 * time.Second):
		}
		log.Fatalf("Exit...\n")
	}()

	host := flag.String("turn", "", "override TURN server ip")
	port := flag.String("port", "", "override TURN port")
	listen := flag.String("listen", "127.0.0.1:9000", "listen on ip:port")
	vklink := flag.String("vk-link", "", "VK calls invite link \"https://vk.com/call/join/...\"")
	yalink := flag.String("yandex-link", "", "Yandex telemost invite link \"https://telemost.yandex.ru/j/...\"")
	peerAddr := flag.String("peer", "", "peer server address (host:port)")
	n := flag.Int("n", 0, "connections to TURN (default 16 for VK, 1 for Yandex)")
	udp := flag.Bool("udp", false, "connect to TURN with UDP")
	direct := flag.Bool("no-dtls", false, "connect without obfuscation. DO NOT USE")
	flag.Parse()

	if *peerAddr == "" {
		log.Panicf("Need peer address!")
	}
	peer, err := net.ResolveUDPAddr("udp", *peerAddr)
	if err != nil {
		panic(err)
	}
	if (*vklink == "") == (*yalink == "") {
		log.Panicf("Need either vk-link or yandex-link!")
	}

	var link string
	var getCreds getCredsFunc

	// Настройка менеджера сессий для обхода квот (Error 486)
	var (
		credsMu    sync.Mutex
		cUser      string
		cPass      string
		cTurn      string
		lastUpdate time.Time
	)

	if *vklink != "" {
		parts := strings.Split(*vklink, "join/")
		link = parts[len(parts)-1]

		// Новая логика: принудительное обновление ключей каждые 30 секунд при сбоях
		getCreds = func(l string) (string, string, string, error) {
			credsMu.Lock()
			defer credsMu.Unlock()

			if time.Since(lastUpdate) > 30*time.Second || cUser == "" {
				u, p, t, err := getVkCreds(l)
				if err == nil {
					cUser, cPass, cTurn = u, p, t
					lastUpdate = time.Now()
					log.Printf("SESSION REFRESH: Got new credentials from VK")
				} else {
					return "", "", "", err
				}
			}
			return cUser, cPass, cTurn, nil
		}

		if *n <= 0 {
			*n = 16
		}
	} else {
		parts := strings.Split(*yalink, "j/")
		link = parts[len(parts)-1]
		getCreds = getYandexCreds
		if *n <= 0 {
			*n = 1
		}
	}

	if idx := strings.IndexAny(link, "/?#"); idx != -1 {
		link = link[:idx]
	}

	params := &turnParams{
		*host,
		*port,
		link,
		*udp,
		getCreds,
	}

	listenConnChan := make(chan net.PacketConn)
	listenConn, err := net.ListenPacket("udp", *listen)
	if err != nil {
		log.Panicf("Failed to listen: %s", err)
	}
	context.AfterFunc(ctx, func() {
		if closeErr := listenConn.Close(); closeErr != nil {
			log.Panicf("Failed to close local connection: %s", closeErr)
		}
	})
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case listenConnChan <- listenConn:
			}
		}
	}()

	wg1 := sync.WaitGroup{}
	t := time.Tick(100 * time.Millisecond)
	if *direct {
		for i := 0; i < *n; i++ {
			wg1.Go(func() {
				oneTurnConnectionLoop(ctx, params, peer, listenConnChan, t)
			})
		}
	} else {
		okchan := make(chan struct{})
		connchan := make(chan net.PacketConn)

		wg1.Go(func() {
			oneDtlsConnectionLoop(ctx, peer, listenConnChan, connchan, okchan)
		})

		wg1.Go(func() {
			oneTurnConnectionLoop(ctx, params, peer, connchan, t)
		})

		select {
		case <-okchan:
		case <-ctx.Done():
		}
		for i := 0; i < *n-1; i++ {
			connchan := make(chan net.PacketConn)
			wg1.Go(func() {
				oneDtlsConnectionLoop(ctx, peer, listenConnChan, connchan, nil)
			})
			wg1.Go(func() {
				oneTurnConnectionLoop(ctx, params, peer, connchan, t)
			})
		}
	}

	wg1.Wait()
}
