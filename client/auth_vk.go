// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bschaatsbergen/dnsdialer"
	"github.com/google/uuid"
)

const GlobalUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"

func performPoW(input string, difficulty int) string {
	prefix := strings.Repeat("0", difficulty)
	var nonce int64 = 0
	for {
		data := fmt.Sprintf("%s%d", input, nonce)
		hash := sha256.Sum256([]byte(data))
		hashHex := hex.EncodeToString(hash[:])
		if strings.HasPrefix(hashHex, prefix) {
			return hashHex
		}
		nonce++
		if nonce > 10000000 {
			return ""
		}
	}
}

func solveSmartCaptcha(httpClient *http.Client, redirectUri string) (string, error) {
	u, err := url.Parse(redirectUri)
	if err != nil {
		return "", err
	}
	sessionToken := u.Query().Get("session_token")

	// Имитируем небольшую задержку перед загрузкой (человеческий фактор)
	time.Sleep(1 * time.Second)

	respPage, err := httpClient.Get(redirectUri)
	if err != nil {
		return "", err
	}
	defer respPage.Body.Close()
	html, _ := io.ReadAll(respPage.Body)
	htmlStr := string(html)

	reInput := regexp.MustCompile(`const powInput = "([^"]+)"`)
	reDiff := regexp.MustCompile(`const difficulty = (\d+)`)
	powInput := sessionToken
	if m := reInput.FindStringSubmatch(htmlStr); len(m) > 1 {
		powInput = m[1]
	}
	difficulty := 2
	if m := reDiff.FindStringSubmatch(htmlStr); len(m) > 1 {
		difficulty, _ = strconv.Atoi(m[1])
	}

	commonHeaders := func(req *http.Request) {
		req.Header.Set("User-Agent", GlobalUA)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "https://id.vk.ru")
		req.Header.Set("Referer", redirectUri)
	}

	params := url.Values{}
	params.Set("session_token", sessionToken)
	params.Set("domain", "vk.com")
	
	reqS, _ := http.NewRequest("POST", "https://api.vk.ru/method/captchaNotRobot.settings?v=5.131", strings.NewReader(params.Encode()))
	commonHeaders(reqS)
	httpClient.Do(reqS)

	params.Set("browser_fp", "539e030fbe394e70ac36a05d791eb7da")
	deviceInfo := `{"screenWidth":1920,"screenHeight":1080,"screenAvailWidth":1920,"screenAvailHeight":1040,"innerWidth":1872,"innerHeight":904,"devicePixelRatio":1,"language":"ru","languages":["ru","en"],"webdriver":false,"hardwareConcurrency":12,"deviceMemory":8,"colorDepth":24,"touchSupport":false}`
	params.Set("device", deviceInfo)
	reqD, _ := http.NewRequest("POST", "https://api.vk.ru/method/captchaNotRobot.componentDone?v=5.131", strings.NewReader(params.Encode()))
	commonHeaders(reqD)
	httpClient.Do(reqD)

	powHash := performPoW(powInput, difficulty)

	// Пауза перед финальным чеком (как будто кликнули)
	time.Sleep(500 * time.Millisecond)

	params.Del("device")
	params.Set("hash", powHash)
	params.Set("accelerometer", "[]")
	params.Set("gyroscope", "[]")
	params.Set("motion", "[]")
	params.Set("cursor", "[]")
	params.Set("taps", "[]")
	
	reqC, _ := http.NewRequest("POST", "https://api.vk.ru/method/captchaNotRobot.check?v=5.131", strings.NewReader(params.Encode()))
	commonHeaders(reqC)
	respC, err := httpClient.Do(reqC)
	if err != nil {
		return "", err
	}
	defer respC.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(respC.Body).Decode(&result)

	if res, ok := result["response"].(map[string]interface{}); ok {
		if token, ok := res["success_token"].(string); ok && token != "" {
			return token, nil
		}
	}
	return "", fmt.Errorf("solve status fail: %v", result)
}

func getVkCreds(link string, dialer *dnsdialer.Dialer) (string, string, string, error) {
	jar, _ := cookiejar.New(nil)
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		Jar:     jar,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     90 * time.Second,
			DialContext:         dialer.DialContext,
		},
	}

	var currentCaptchaToken string
	var currentCaptchaSid string

	var doRequest func(string, string) (map[string]interface{}, error)
	doRequest = func(data string, requestUrl string) (map[string]interface{}, error) {
		finalData := data
		if currentCaptchaToken != "" && currentCaptchaSid != "" {
			// Передаем токен во всех возможных полях для надежности
			finalData = fmt.Sprintf("%s&captcha_sid=%s&captcha_key=%s&captcha_token=%s&success_token=%s", 
				data, currentCaptchaSid, currentCaptchaToken, currentCaptchaToken, currentCaptchaToken)
		}

		req, _ := http.NewRequest("POST", requestUrl, bytes.NewBuffer([]byte(finalData)))
		req.Header.Set("User-Agent", GlobalUA)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		httpResp, err := httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer httpResp.Body.Close()

		respBody, _ := io.ReadAll(httpResp.Body)
		var resp map[string]interface{}
		json.Unmarshal(respBody, &resp)

		if errVal, exists := resp["error"]; exists {
			if errMap, ok := errVal.(map[string]interface{}); ok {
				code := fmt.Sprintf("%v", errMap["error_code"])
				if code == "14" {
					sid := fmt.Sprintf("%v", errMap["captcha_sid"])
					redirectUri, _ := errMap["redirect_uri"].(string)
					
					// Если мы уже пробовали решить и снова получили 14 - значит решение не принято
					if currentCaptchaToken != "" {
						log.Printf("!!! RETRY FAILED. VK still asks for captcha. Response: %s", string(respBody))
						return nil, fmt.Errorf("CAPTCHA_WAIT_REQUIRED")
					}

					if redirectUri != "" {
						log.Printf("!!! SMART CAPTCHA (SID: %s). AUTO-SOLVING...", sid)
						token, solveErr := solveSmartCaptcha(httpClient, redirectUri)
						if solveErr == nil {
							log.Printf("!!! CAPTCHA AUTO-SOLVED! RETRYING ORIGINAL REQUEST...")
							currentCaptchaToken = token
							currentCaptchaSid = sid
							return doRequest(data, requestUrl)
						}
						log.Printf("!!! AUTO-SOLVE FAILED: %v", solveErr)
					}
					return nil, fmt.Errorf("CAPTCHA_WAIT_REQUIRED")
				}
				return nil, fmt.Errorf("VK Error: %v", errMap["error_msg"])
			}
		}
		return resp, nil
	}

	getNestedString := func(m map[string]interface{}, keys ...string) (string, error) {
		var current interface{} = m
		for _, key := range keys {
			if next, ok := current.(map[string]interface{}); ok {
				current = next[key]
			} else {
				return "", fmt.Errorf("key [%s] missing", key)
			}
		}
		if s, ok := current.(string); ok {
			return s, nil
		}
		return "", fmt.Errorf("value not string")
	}

	clientId := "6287487" 
	clientSecret := "QbYic1K3lEV5kTGiqlq2"

	resp, err := doRequest(fmt.Sprintf("client_id=%s&token_type=messages&client_secret=%s&version=1&app_id=%s", clientId, clientSecret, clientId), "https://login.vk.ru/?act=get_anonym_token")
	if err != nil {
		return "", "", "", err
	}

	token1, err := getNestedString(resp, "data", "access_token")
	if err != nil {
		return "", "", "", fmt.Errorf("step1: %v", err)
	}

	resp, err = doRequest(fmt.Sprintf("vk_join_link=https://vk.com/call/join/%s&name=123&access_token=%s", link, token1), fmt.Sprintf("https://api.vk.ru/method/calls.getAnonymousToken?v=5.274&client_id=%s", clientId))
	if err != nil {
		return "", "", "", err
	}

	token2, err := getNestedString(resp, "response", "token")
	if err != nil {
		return "", "", "", fmt.Errorf("step2: %v", err)
	}

	resp, err = doRequest(fmt.Sprintf("session_data=%%7B%%22version%%22%%3A2%%2C%%22device_id%%22%%3A%%22%s%%22%%2C%%22client_version%%22%%3A1.1%%2C%%22client_type%%22%%3A%%22SDK_JS%%22%%7D&method=auth.anonymLogin&format=JSON&application_key=CGMMEJLGDIHBABABA", uuid.New()), "https://calls.okcdn.ru/fb.do")
	if err != nil {
		return "", "", "", err
	}

	token3, err := getNestedString(resp, "session_key")
	if err != nil {
		return "", "", "", fmt.Errorf("step3: %v", err)
	}

	resp, err = doRequest(fmt.Sprintf("joinLink=%s&isVideo=false&protocolVersion=5&anonymToken=%s&method=vchat.joinConversationByLink&format=JSON&application_key=CGMMEJLGDIHBABABA&session_key=%s", link, token2, token3), "https://calls.okcdn.ru/fb.do")
	if err != nil {
		return "", "", "", err
	}

	user, _ := getNestedString(resp, "turn_server", "username")
	pass, _ := getNestedString(resp, "turn_server", "credential")
	
	var turnAddr string
	if ts, ok := resp["turn_server"].(map[string]interface{}); ok {
		if urls, ok := ts["urls"].([]interface{}); ok && len(urls) > 0 {
			turnAddr, _ = urls[0].(string)
		}
	}

	address := strings.TrimPrefix(strings.TrimPrefix(strings.Split(turnAddr, "?")[0], "turn:"), "turns:")
	return user, pass, address, nil
}

func getYandexCreds(link string) (string, string, string, error) {
	return "", "", "", fmt.Errorf("yandex not implemented")
}
