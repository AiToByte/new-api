package common

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wenlng/go-captcha/v2/slide"
)

func storedSlideAnswer(t *testing.T, id string) (int, int) {
	t.Helper()
	captchaMutex.Lock()
	defer captchaMutex.Unlock()
	raw, ok := captchaMap[captchaNamespaceSlide+id]
	require.True(t, ok, "slide challenge must be stored")
	var x, y int
	_, err := fmt.Sscanf(raw.code, "%d,%d", &x, &y)
	require.NoError(t, err)
	return x, y
}

func TestNewSlideCaptchaRoundtrip(t *testing.T) {
	captcha, err := NewSlideCaptcha()
	require.NoError(t, err)
	require.Len(t, captcha.ID, captchaIDBytes*2)
	require.True(t, strings.HasPrefix(captcha.MasterImage, "data:image/jpeg;base64,"))
	require.True(t, strings.HasPrefix(captcha.TileImage, "data:image/png;base64,"))
	assert.Equal(t, slideImageWidth, captcha.ImageWidth)
	assert.Equal(t, slideImageHeight, captcha.ImageHeight)
	assert.Equal(t, int(captchaValidDuration.Seconds()), captcha.ExpiresIn)

	x, y := storedSlideAnswer(t, captcha.ID)
	assert.GreaterOrEqual(t, x, 0)
	assert.Less(t, x, slideImageWidth)
	assert.GreaterOrEqual(t, y, 0)
	assert.Less(t, y, slideImageHeight)

	assert.True(t, VerifySlideCaptcha(captcha.ID, x, y, ""))
	assert.False(t, VerifySlideCaptcha(captcha.ID, x, y, ""), "slide challenge must be single-use")
}

func TestVerifySlideCaptchaTolerance(t *testing.T) {
	captcha, err := NewSlideCaptcha()
	require.NoError(t, err)
	x, y := storedSlideAnswer(t, captcha.ID)

	assert.True(t, VerifySlideCaptcha(captcha.ID, x+slideTolerancePx, y, ""))

	captcha, err = NewSlideCaptcha()
	require.NoError(t, err)
	x, y = storedSlideAnswer(t, captcha.ID)
	assert.False(t, VerifySlideCaptcha(captcha.ID, x+slideTolerancePx+20, y, ""))
	assert.False(t, VerifySlideCaptcha(captcha.ID, x, y+slideTolerancePx+20, ""))
}

func TestVerifySlideCaptchaRejectsBadInput(t *testing.T) {
	assert.False(t, VerifySlideCaptcha("", 10, 10, ""))
	assert.False(t, VerifySlideCaptcha("nonexistent", 10, 10, ""))

	captcha, err := NewSlideCaptcha()
	require.NoError(t, err)
	x, y := storedSlideAnswer(t, captcha.ID)
	assert.False(t, VerifySlideCaptcha(captcha.ID, x, y, "not-json"))
	assert.False(t, VerifySlideCaptcha(captcha.ID, x, y, `[{"x":1,"y":2,"t":5},{"x":2,"y":2,"t":3}]`))
}

func TestVerifySlideCaptchaAcceptsSaneTrail(t *testing.T) {
	captcha, err := NewSlideCaptcha()
	require.NoError(t, err)
	x, y := storedSlideAnswer(t, captcha.ID)
	trail := `[{"x":0,"y":10,"t":1000},{"x":50,"y":12,"t":1120},{"x":120,"y":11,"t":1300}]`
	assert.True(t, VerifySlideCaptcha(captcha.ID, x, y, trail))
}

func TestVerifySlideCaptchaExpires(t *testing.T) {
	previous := captchaValidDuration
	captchaValidDuration = 20 * time.Millisecond
	t.Cleanup(func() { captchaValidDuration = previous })

	captcha, err := NewSlideCaptcha()
	require.NoError(t, err)
	x, y := storedSlideAnswer(t, captcha.ID)

	time.Sleep(40 * time.Millisecond)
	assert.False(t, VerifySlideCaptcha(captcha.ID, x, y, ""))
}

func TestNewSlideCaptchaImagesDecode(t *testing.T) {
	captcha, err := NewSlideCaptcha()
	require.NoError(t, err)

	masterRaw := strings.TrimPrefix(captcha.MasterImage, "data:image/jpeg;base64,")
	masterBytes, err := base64.StdEncoding.DecodeString(masterRaw)
	require.NoError(t, err)
	masterImg, err := jpeg.Decode(bytes.NewReader(masterBytes))
	require.NoError(t, err)
	assert.Equal(t, slideImageWidth, masterImg.Bounds().Dx())
	assert.Equal(t, slideImageHeight, masterImg.Bounds().Dy())

	tileRaw := strings.TrimPrefix(captcha.TileImage, "data:image/png;base64,")
	tileBytes, err := base64.StdEncoding.DecodeString(tileRaw)
	require.NoError(t, err)
	tileImg, err := png.Decode(bytes.NewReader(tileBytes))
	require.NoError(t, err)
	assert.Equal(t, captcha.TileWidth, tileImg.Bounds().Dx())
	assert.Equal(t, captcha.TileHeight, tileImg.Bounds().Dy())
	assert.Greater(t, captcha.TileWidth, 0)
	assert.Greater(t, captcha.TileHeight, 0)
}

func TestSlideAnswerPositionIsTheHoleNotTheTileStart(t *testing.T) {
	block := &slide.Block{X: 150, Y: 100, DX: 12, DY: 100}
	x, y := slideAnswerPosition(block)
	assert.Equal(t, 150, x)
	assert.Equal(t, 100, y)
}
