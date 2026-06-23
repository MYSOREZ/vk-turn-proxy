package main

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestCaptchaSolveModeForAttempt(t *testing.T) {
	t.Parallel()

	t.Run("default flow", func(t *testing.T) {
		t.Parallel()

		mode, ok := captchaSolveModeForAttempt(0, false, true)
		if !ok || mode != captchaSolveModeAuto {
			t.Fatalf("expected first attempt to use auto captcha, got mode=%v ok=%v", mode, ok)
		}

		mode, ok = captchaSolveModeForAttempt(1, false, true)
		if !ok || mode != captchaSolveModeSliderPOC {
			t.Fatalf("expected second attempt to use slider POC, got mode=%v ok=%v", mode, ok)
		}

		mode, ok = captchaSolveModeForAttempt(2, false, true)
		if !ok || mode != captchaSolveModeManual {
			t.Fatalf("expected third attempt to use manual captcha, got mode=%v ok=%v", mode, ok)
		}

		if _, ok = captchaSolveModeForAttempt(3, false, true); ok {
			t.Fatal("expected no fourth captcha attempt in default flow")
		}
	})

	t.Run("manual only flow", func(t *testing.T) {
		t.Parallel()

		mode, ok := captchaSolveModeForAttempt(0, true, true)
		if !ok || mode != captchaSolveModeManual {
			t.Fatalf("expected manual mode on first attempt, got mode=%v ok=%v", mode, ok)
		}

		if _, ok = captchaSolveModeForAttempt(1, true, true); ok {
			t.Fatal("expected only one manual captcha attempt when manual mode is forced")
		}
	})

	t.Run("flow without slider poc", func(t *testing.T) {
		t.Parallel()

		mode, ok := captchaSolveModeForAttempt(0, false, false)
		if !ok || mode != captchaSolveModeAuto {
			t.Fatalf("expected auto captcha first, got mode=%v ok=%v", mode, ok)
		}

		mode, ok = captchaSolveModeForAttempt(1, false, false)
		if !ok || mode != captchaSolveModeManual {
			t.Fatalf("expected manual captcha second when slider POC is disabled, got mode=%v ok=%v", mode, ok)
		}

		if _, ok = captchaSolveModeForAttempt(2, false, false); ok {
			t.Fatal("expected only two attempts when slider POC is disabled")
		}
	})
}

func TestParseVkCaptchaErrorV2RedirectOnly(t *testing.T) {
	t.Parallel()

	// VK's current error_code:14 response no longer includes captcha_sid or
	// captcha_img; it only carries redirect_uri with an embedded session_token.
	errData := map[string]interface{}{
		"error_code": float64(14),
		"error_msg":  "Captcha need",
		"redirect_uri": "https://id.vk.ru/not_robot_captcha?domain=vk.com" +
			"&session_token=abc.def.ghi&variant=popup&blank=1",
	}

	captchaErr := ParseVkCaptchaError(errData)
	if captchaErr == nil {
		t.Fatal("expected captcha error to be parsed, got nil")
	}
	if !captchaErr.IsCaptchaError() {
		t.Fatalf("expected IsCaptchaError to be true, got code=%d redirect=%q session=%q",
			captchaErr.ErrorCode, captchaErr.RedirectURI, captchaErr.SessionToken)
	}
	if captchaErr.SessionToken != "abc.def.ghi" {
		t.Fatalf("expected session_token to be extracted from redirect_uri, got %q", captchaErr.SessionToken)
	}
	if captchaErr.CaptchaSid != "" {
		t.Fatalf("expected empty captcha_sid, got %q", captchaErr.CaptchaSid)
	}
}

func TestParseVkCaptchaErrorLegacySid(t *testing.T) {
	t.Parallel()

	// Legacy format with captcha_sid/captcha_img must still parse.
	errData := map[string]interface{}{
		"error_code":   float64(14),
		"error_msg":    "Captcha needed",
		"captcha_sid":  "123456789",
		"captcha_img":  "https://api.vk.com/captcha.php?sid=123456789",
		"redirect_uri": "https://id.vk.com/not_robot_captcha?session_token=tok",
	}

	captchaErr := ParseVkCaptchaError(errData)
	if captchaErr == nil {
		t.Fatal("expected captcha error to be parsed, got nil")
	}
	if captchaErr.CaptchaSid != "123456789" {
		t.Fatalf("expected captcha_sid 123456789, got %q", captchaErr.CaptchaSid)
	}
	if captchaErr.CaptchaImg == "" {
		t.Fatal("expected captcha_img to be preserved")
	}
}

func TestDebugThrottledfSuppressesRepeats(t *testing.T) {
	// Not parallel: mutates package-global isDebug and log output.
	prevDebug := isDebug
	isDebug = true
	defer func() { isDebug = prevDebug }()

	var buf bytes.Buffer
	prevOut := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	}()

	key := "test-throttle-" + t.Name()
	debugThrottleState.Delete(key)

	for i := 0; i < 5; i++ {
		debugThrottledf(key, "tick %d", i)
	}

	if got := strings.Count(buf.String(), "tick "); got != 1 {
		t.Fatalf("expected throttle to emit exactly once within interval, got %d lines: %q", got, buf.String())
	}
}
