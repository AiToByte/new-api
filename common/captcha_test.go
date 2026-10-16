package common

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewImageCaptchaRoundtrip(t *testing.T) {
	captcha, err := NewImageCaptcha()
	require.NoError(t, err)
	require.Len(t, captcha.ID, captchaIDBytes*2)
	require.True(t, strings.HasPrefix(captcha.Image, "data:image/png;base64,"))
	assert.Equal(t, int(captchaValidDuration.Seconds()), captcha.ExpiresIn)

	// The answer is server-side only; extract it from the store for the test.
	captchaMutex.Lock()
	answer := captchaMap[captchaNamespaceText+captcha.ID].code
	captchaMutex.Unlock()
	require.Len(t, answer, CaptchaCodeLength)
	for _, ch := range answer {
		assert.Contains(t, captchaAlphabet, string(ch))
	}

	assert.True(t, VerifyImageCaptcha(captcha.ID, answer))
	assert.False(t, VerifyImageCaptcha(captcha.ID, answer), "challenge must be single-use")
}

func TestVerifyImageCaptchaRejectsBadInput(t *testing.T) {
	captcha, err := NewImageCaptcha()
	require.NoError(t, err)

	assert.False(t, VerifyImageCaptcha("", "ABCDE"))
	assert.False(t, VerifyImageCaptcha(captcha.ID, ""))
	assert.False(t, VerifyImageCaptcha(captcha.ID, "XXXXX"))

	captchaMutex.Lock()
	_, stillStored := captchaMap[captchaNamespaceText+captcha.ID]
	captchaMutex.Unlock()
	assert.False(t, stillStored, "failed attempt must still consume the challenge")
}

func TestVerifyImageCaptchaIsCaseInsensitive(t *testing.T) {
	captcha, err := NewImageCaptcha()
	require.NoError(t, err)

	captchaMutex.Lock()
	answer := captchaMap[captchaNamespaceText+captcha.ID].code
	captchaMutex.Unlock()

	assert.True(t, VerifyImageCaptcha(captcha.ID, strings.ToLower(answer)))
}

func TestVerifyImageCaptchaExpires(t *testing.T) {
	previous := captchaValidDuration
	captchaValidDuration = 20 * time.Millisecond
	t.Cleanup(func() { captchaValidDuration = previous })

	captcha, err := NewImageCaptcha()
	require.NoError(t, err)

	captchaMutex.Lock()
	answer := captchaMap[captchaNamespaceText+captcha.ID].code
	captchaMutex.Unlock()

	time.Sleep(40 * time.Millisecond)
	assert.False(t, VerifyImageCaptcha(captcha.ID, answer))
}

func TestNewImageCaptchaRendersDecodablePNG(t *testing.T) {
	captcha, err := NewImageCaptcha()
	require.NoError(t, err)

	raw := strings.TrimPrefix(captcha.Image, "data:image/png;base64,")
	pngBytes, err := base64.StdEncoding.DecodeString(raw)
	require.NoError(t, err)

	img, err := png.Decode(bytes.NewReader(pngBytes))
	require.NoError(t, err)
	assert.Equal(t, image.Rect(0, 0, captchaImageWidth, captchaImageHeight), img.Bounds())
}

func TestImageCaptchaRedisRoundtrip(t *testing.T) {
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	if err := client.Ping(client.Context()).Err(); err != nil {
		_ = client.Close()
		t.Skipf("redis is not reachable at %s", addr)
	}
	t.Cleanup(func() { _ = client.Close() })

	previousRDB, previousEnabled := RDB, RedisEnabled
	RDB, RedisEnabled = client, true
	t.Cleanup(func() { RDB, RedisEnabled = previousRDB, previousEnabled })

	captcha, err := NewImageCaptcha()
	require.NoError(t, err)

	stored, err := client.Get(client.Context(), captchaRedisKey(captchaNamespaceText, captcha.ID)).Result()
	require.NoError(t, err)
	require.Len(t, stored, CaptchaCodeLength)

	assert.True(t, VerifyImageCaptcha(captcha.ID, stored))
	assert.False(t, VerifyImageCaptcha(captcha.ID, stored), "redis challenge must be single-use")

	_, err = client.Get(client.Context(), captchaRedisKey(captchaNamespaceText, captcha.ID)).Result()
	assert.ErrorIs(t, err, redis.Nil)
}
