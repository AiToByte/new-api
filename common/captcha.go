package common

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// CaptchaEnabled gates image-captcha verification on login and registration.
// It is intentionally default-on: this distribution serves external users.
var CaptchaEnabled = true

const (
	// CaptchaCodeLength is the number of characters in one answer.
	CaptchaCodeLength = 5
	// captchaIDBytes is the entropy of a challenge id (128 bit).
	captchaIDBytes = 16
	// captchaMaxEntries bounds the in-memory challenge store.
	captchaMaxEntries = 1000
	// captchaImageWidth/Height fit 5 jittered 7x13 glyphs plus noise margins.
	captchaImageWidth  = 200
	captchaImageHeight = 60
)

// captchaAlphabet excludes confusable glyphs (0/O, 1/I/L). Its length is a
// power of two, so rejection-free modulo sampling stays unbiased.
const captchaAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// captchaValidDuration is a var (not const) so tests can shrink it.
var captchaValidDuration = 5 * time.Minute

type captchaValue struct {
	code      string
	expiresAt time.Time
}

var captchaMutex sync.Mutex
var captchaMap = make(map[string]captchaValue)

// ImageCaptcha is a freshly issued login/registration challenge.
type ImageCaptcha struct {
	ID        string
	Image     string
	ExpiresIn int
}

func newCaptchaID() (string, error) {
	raw := make([]byte, captchaIDBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func newCaptchaAnswer() (string, error) {
	raw := make([]byte, CaptchaCodeLength)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	answer := make([]byte, CaptchaCodeLength)
	for i, b := range raw {
		answer[i] = captchaAlphabet[int(b)%len(captchaAlphabet)]
	}
	return string(answer), nil
}

// NewImageCaptcha issues a single-use challenge and renders its PNG.
func NewImageCaptcha() (*ImageCaptcha, error) {
	id, err := newCaptchaID()
	if err != nil {
		return nil, err
	}
	answer, err := newCaptchaAnswer()
	if err != nil {
		return nil, err
	}
	pngBytes, err := renderCaptchaImage(answer)
	if err != nil {
		return nil, err
	}

	captchaMutex.Lock()
	if len(captchaMap) >= captchaMaxEntries {
		sweepExpiredCaptchasLocked()
		if len(captchaMap) >= captchaMaxEntries {
			captchaMutex.Unlock()
			return nil, fmt.Errorf("captcha service is busy, please try again")
		}
	}
	captchaMutex.Unlock()
	captchaStoreSave(captchaNamespaceText, id, answer, captchaValidDuration)

	return &ImageCaptcha{
		ID:        id,
		Image:     "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes),
		ExpiresIn: int(captchaValidDuration.Seconds()),
	}, nil
}

// VerifyImageCaptcha checks a challenge answer. Every attempt consumes the
// challenge, so each guess requires fetching a fresh image.
func VerifyImageCaptcha(id, code string) bool {
	if id == "" || code == "" {
		return false
	}
	stored, ok := captchaStoreTake(captchaNamespaceText, id)
	if !ok {
		return false
	}
	expected := strings.ToUpper(stored)
	actual := strings.ToUpper(strings.TrimSpace(code))
	if len(expected) != len(actual) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}

// captchaRedisAvailable reports whether challenges can live in Redis, which
// is required for multi-node deployments to verify each other's challenges.
func captchaRedisAvailable() bool {
	return RedisEnabled && RDB != nil
}

const (
	// captchaNamespaceText and captchaNamespaceSlide keep the two challenge
	// kinds from colliding in the shared store.
	captchaNamespaceText  = "text:"
	captchaNamespaceSlide = "slide:"
)

func captchaRedisKey(namespace, id string) string {
	return "captcha:" + namespace + id
}

// captchaStoreSave persists a challenge, preferring Redis and falling back
// to memory. Only the random id is logged on failure, never the answer.
// The namespace keeps text and slide challenges from colliding.
func captchaStoreSave(namespace, id, code string, ttl time.Duration) {
	if captchaRedisAvailable() {
		if err := RDB.Set(context.Background(), captchaRedisKey(namespace, id), code, ttl).Err(); err == nil {
			return
		} else {
			SysLog(LogText("captcha redis store failed, falling back to memory: %s", err.Error()))
		}
	}

	captchaMutex.Lock()
	defer captchaMutex.Unlock()
	if len(captchaMap) >= captchaMaxEntries {
		sweepExpiredCaptchasLocked()
	}
	captchaMap[namespace+id] = captchaValue{
		code:      code,
		expiresAt: time.Now().Add(ttl),
	}
}

// captchaStoreTake returns and consumes a challenge answer. A Redis take is
// atomic (GETDEL); ancient servers without GETDEL fall back to GET+DEL.
func captchaStoreTake(namespace, id string) (string, bool) {
	if captchaRedisAvailable() {
		ctx := context.Background()
		key := captchaRedisKey(namespace, id)
		code, err := RDB.GetDel(ctx, key).Result()
		if err == nil {
			return code, true
		}
		if !errors.Is(err, redis.Nil) {
			if strings.Contains(err.Error(), "unknown command") {
				if code, err := RDB.Get(ctx, key).Result(); err == nil {
					_ = RDB.Del(ctx, key).Err()
					return code, true
				}
			} else {
				SysLog(LogText("captcha redis take failed: %s", err.Error()))
			}
			return "", false
		}
	}

	captchaMutex.Lock()
	value, ok := captchaMap[namespace+id]
	delete(captchaMap, namespace+id)
	captchaMutex.Unlock()

	if !ok || time.Now().After(value.expiresAt) {
		return "", false
	}
	return value.code, true
}

// sweepExpiredCaptchasLocked drops expired challenges. The caller must hold
// captchaMutex.
func sweepExpiredCaptchasLocked() {
	now := time.Now()
	for id, value := range captchaMap {
		if now.After(value.expiresAt) {
			delete(captchaMap, id)
		}
	}
}

var captchaInkPalette = []color.RGBA{
	{0x1D, 0x4E, 0xD8, 0xFF},
	{0x1E, 0x3A, 0x8A, 0xFF},
	{0x0F, 0x76, 0x6E, 0xFF},
	{0x7C, 0x2D, 0x12, 0xFF},
	{0x3F, 0x3F, 0x46, 0xFF},
}

func captchaRandIntn(n int) int {
	var b [1]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0
	}
	return int(b[0]) % n
}

// renderCaptchaImage draws the answer with per-glyph jitter, speckle noise
// and two sine interference strokes.
func renderCaptchaImage(answer string) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, captchaImageWidth, captchaImageHeight))
	for y := 0; y < captchaImageHeight; y++ {
		for x := 0; x < captchaImageWidth; x++ {
			img.Set(x, y, color.RGBA{0xF8, 0xFA, 0xFC, 0xFF})
		}
	}

	cellWidth := captchaImageWidth / len(answer)
	for i := range len(answer) {
		ink := captchaInkPalette[captchaRandIntn(len(captchaInkPalette))]
		drawer := &font.Drawer{
			Dst:  img,
			Src:  image.NewUniform(ink),
			Face: basicfont.Face7x13,
			Dot: fixed.Point26_6{
				X: fixed.I(i*cellWidth + 8 + captchaRandIntn(8)),
				Y: fixed.I(22 + captchaRandIntn(22)),
			},
		}
		drawer.DrawString(string(answer[i]))
	}

	speckles := 160
	for range speckles {
		x := captchaRandIntn(captchaImageWidth)
		y := captchaRandIntn(captchaImageHeight)
		gray := uint8(170 + captchaRandIntn(60))
		img.Set(x, y, color.RGBA{gray, gray, gray, 0xFF})
	}

	for _, phase := range []float64{0, 2.1} {
		for x := 0; x < captchaImageWidth; x++ {
			y := int(30 + 12*math.Sin(float64(x)*0.09+phase))
			for dy := -1; dy <= 1; dy++ {
				if y+dy >= 0 && y+dy < captchaImageHeight {
					img.Set(x, y+dy, color.RGBA{0x94, 0xA3, 0xB8, 0xFF})
				}
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
