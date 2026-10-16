package common

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"math"
	mrand "math/rand"
	"strings"
	"sync"
	"time"

	"github.com/wenlng/go-captcha/v2/slide"
)

const (
	// slideImageWidth/Height follow the go-captcha slide defaults so the
	// frontend can scale drag coordinates 1:1 with the issued images.
	slideImageWidth  = 300
	slideImageHeight = 220
	// slideGraphSize is the canvas of the procedural jigsaw piece triple.
	slideGraphSize = 72
	// slideTolerancePx is the accepted drag error on the 300px-wide image.
	slideTolerancePx = 6
	// slideMaxTrailPoints bounds the drag-trail payload.
	slideMaxTrailPoints = 500
	// slideMaxTrailSeconds bounds the drag duration.
	slideMaxTrailSeconds = 120
)

// SlideCaptcha is a freshly issued jigsaw challenge. TileX is intentionally
// absent: the answer lives server-side only.
type SlideCaptcha struct {
	ID          string
	MasterImage string
	TileImage   string
	TileY       int
	TileWidth   int
	TileHeight  int
	ImageWidth  int
	ImageHeight int
	ExpiresIn   int
}

type slideTrailPoint struct {
	X int64 `json:"x"`
	Y int64 `json:"y"`
	T int64 `json:"t"`
}

var (
	slideBuilder     slide.Builder
	slideBuilderOnce sync.Once
	slideGenMutex    sync.Mutex
)

func slideBackgroundSeed() int64 {
	var seed [8]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return time.Now().UnixNano()
	}
	return int64(binary.LittleEndian.Uint64(seed[:]))
}

type slidePalette struct {
	top    color.RGBA
	bottom color.RGBA
	glow   color.RGBA
}

var slidePalettes = []slidePalette{
	{color.RGBA{0x1D, 0x4E, 0xD8, 0xFF}, color.RGBA{0x0B, 0x12, 0x2E, 0xFF}, color.RGBA{0x93, 0xC5, 0xFD, 0xFF}},
	{color.RGBA{0x0F, 0x76, 0x6E, 0xFF}, color.RGBA{0x06, 0x1B, 0x1A, 0xFF}, color.RGBA{0x5E, 0xEA, 0xD4, 0xFF}},
	{color.RGBA{0x7C, 0x3A, 0xED, 0xFF}, color.RGBA{0x1E, 0x1B, 0x3A, 0xFF}, color.RGBA{0xDD, 0xD6, 0xFE, 0xFF}},
	{color.RGBA{0xB4, 0x53, 0x09, 0xFF}, color.RGBA{0x1C, 0x10, 0x07, 0xFF}, color.RGBA{0xFD, 0xD6, 0x8A, 0xFF}},
}

func lerpChannel(a, b uint8, t float64) uint8 {
	return uint8(float64(a)*(1-t) + float64(b)*t)
}

// makeSlideBackground renders an abstract gradient scene with soft discs and
// grain. Procedural art avoids binary assets and licensing questions while
// giving every challenge a fresh look.
func makeSlideBackground(rng *mrand.Rand) image.Image {
	pal := slidePalettes[rng.Intn(len(slidePalettes))]
	img := image.NewRGBA(image.Rect(0, 0, slideImageWidth, slideImageHeight))
	for y := 0; y < slideImageHeight; y++ {
		t := float64(y) / float64(slideImageHeight-1)
		for x := 0; x < slideImageWidth; x++ {
			img.Set(x, y, color.RGBA{
				lerpChannel(pal.top.R, pal.bottom.R, t),
				lerpChannel(pal.top.G, pal.bottom.G, t),
				lerpChannel(pal.top.B, pal.bottom.B, t),
				0xFF,
			})
		}
	}
	for range 3 + rng.Intn(3) {
		cx := float64(rng.Intn(slideImageWidth))
		cy := float64(rng.Intn(slideImageHeight))
		radius := 30 + rng.Float64()*70
		for y := 0; y < slideImageHeight; y++ {
			for x := 0; x < slideImageWidth; x++ {
				dx := float64(x) - cx
				dy := float64(y) - cy
				dist := math.Sqrt(dx*dx + dy*dy)
				if dist >= radius {
					continue
				}
				blend := (1 - dist/radius) * 0.35
				old := img.RGBAAt(x, y)
				img.Set(x, y, color.RGBA{
					lerpChannel(old.R, pal.glow.R, blend),
					lerpChannel(old.G, pal.glow.G, blend),
					lerpChannel(old.B, pal.glow.B, blend),
					0xFF,
				})
			}
		}
	}
	for range 900 {
		x := rng.Intn(slideImageWidth)
		y := rng.Intn(slideImageHeight)
		old := img.RGBAAt(x, y)
		jitter := uint8(rng.Intn(24))
		img.Set(x, y, color.RGBA{
			old.R/2 + jitter,
			old.G/2 + jitter,
			old.B/2 + jitter,
			0xFF,
		})
	}
	addSlideWaveBands(img, rng)
	addSlideShapes(img, rng)
	for range 600 {
		x := rng.Intn(slideImageWidth)
		y := rng.Intn(slideImageHeight)
		old := img.RGBAAt(x, y)
		keep := 100 - rng.Intn(50)
		img.Set(x, y, color.RGBA{
			uint8(int(old.R) * keep / 100),
			uint8(int(old.G) * keep / 100),
			uint8(int(old.B) * keep / 100),
			0xFF,
		})
	}
	return img
}

func blendOver(dst color.RGBA, src color.RGBA) color.RGBA {
	a := float64(src.A) / 255
	return color.RGBA{
		uint8(float64(dst.R)*(1-a) + float64(src.R)*a),
		uint8(float64(dst.G)*(1-a) + float64(src.G)*a),
		uint8(float64(dst.B)*(1-a) + float64(src.B)*a),
		0xFF,
	}
}

// addSlideWaveBands paints translucent sine bands across the scene.
func addSlideWaveBands(img *image.RGBA, rng *mrand.Rand) {
	for range 2 + rng.Intn(2) {
		centerY := float64(rng.Intn(slideImageHeight))
		amplitude := 8 + rng.Float64()*16
		frequency := 0.02 + rng.Float64()*0.03
		phase := rng.Float64() * math.Pi * 2
		thickness := 5 + rng.Float64()*9
		light := rng.Intn(2) == 0
		var paint color.RGBA
		if light {
			paint = color.RGBA{0xFF, 0xFF, 0xFF, 0x2E}
		} else {
			paint = color.RGBA{0x00, 0x00, 0x00, 0x2E}
		}
		for y := 0; y < slideImageHeight; y++ {
			for x := 0; x < slideImageWidth; x++ {
				waveY := centerY + amplitude*math.Sin(float64(x)*frequency+phase)
				if math.Abs(float64(y)-waveY) <= thickness {
					img.Set(x, y, blendOver(img.RGBAAt(x, y), paint))
				}
			}
		}
	}
}

// addSlideShapes scatters translucent rotated rectangles and triangles.
func addSlideShapes(img *image.RGBA, rng *mrand.Rand) {
	for range 5 + rng.Intn(4) {
		cx := float64(rng.Intn(slideImageWidth))
		cy := float64(rng.Intn(slideImageHeight))
		w := 20 + rng.Float64()*50
		h := 14 + rng.Float64()*36
		angle := rng.Float64() * math.Pi
		alpha := uint8(40 + rng.Intn(32))
		var paint color.RGBA
		switch rng.Intn(3) {
		case 0:
			paint = color.RGBA{0xFF, 0xFF, 0xFF, alpha}
		case 1:
			paint = color.RGBA{0x00, 0x00, 0x00, alpha}
		default:
			paint = color.RGBA{0xF8, 0xFA, 0xFC, alpha}
		}
		isTriangle := rng.Intn(2) == 0
		cos, sin := math.Cos(angle), math.Sin(angle)
		x0 := int(math.Max(cx-math.Abs(w/2*cos)-math.Abs(h/2*sin)-1, 0))
		x1 := int(math.Min(cx+math.Abs(w/2*cos)+math.Abs(h/2*sin)+1, slideImageWidth))
		y0 := int(math.Max(cy-math.Abs(w/2*sin)-math.Abs(h/2*cos)-1, 0))
		y1 := int(math.Min(cy+math.Abs(w/2*sin)+math.Abs(h/2*cos)+1, slideImageHeight))
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				lx := float64(x) - cx
				ly := float64(y) - cy
				rx := lx*cos + ly*sin
				ry := -lx*sin + ly*cos
				inside := math.Abs(rx) <= w/2 && math.Abs(ry) <= h/2
				if inside && isTriangle {
					inside = ry >= (h/w)*math.Abs(rx)-h/2
				}
				if inside {
					img.Set(x, y, blendOver(img.RGBAAt(x, y), paint))
				}
			}
		}
	}
}

// jigsawInside reports whether a pixel belongs to the piece silhouette:
// a rounded square with a tab on the right and a blank carved on top.
func jigsawInside(x, y, size int) bool {
	half := float64(size) / 2
	cx, cy := float64(x)-half, float64(y)-half
	halfSide := half - 8
	radius := 8.0

	qx := math.Abs(cx) - (halfSide - radius)
	qy := math.Abs(cy) - (halfSide - radius)
	rrDist := math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - radius
	inside := rrDist <= 0

	tabDist := math.Hypot(cx-(halfSide+2), cy)
	if tabDist <= 11 {
		inside = true
	}
	blankDist := math.Hypot(cx+4, cy-(halfSide+2))
	if blankDist <= 10 {
		inside = false
	}
	return inside
}

func jigsawEdge(x, y, size int) bool {
	if !jigsawInside(x, y, size) {
		return false
	}
	return !jigsawInside(x-2, y, size) || !jigsawInside(x+2, y, size) ||
		!jigsawInside(x, y-2, size) || !jigsawInside(x, y+2, size)
}

// makeSlideGraphTriple renders the mask, edge-overlay and shadow of one
// jigsaw piece. White carries shape alpha for mask/overlay, black for shadow.
func makeSlideGraphTriple() (mask, overlay, shadow image.Image) {
	size := slideGraphSize
	maskImg := image.NewNRGBA(image.Rect(0, 0, size, size))
	overlayImg := image.NewNRGBA(image.Rect(0, 0, size, size))
	shadowImg := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if !jigsawInside(x, y, size) {
				continue
			}
			maskImg.Set(x, y, color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF})
			shadowImg.Set(x, y, color.NRGBA{0x00, 0x00, 0x00, 0x8C})
			if jigsawEdge(x, y, size) {
				overlayImg.Set(x, y, color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF})
			}
		}
	}
	return maskImg, overlayImg, shadowImg
}

func getSlideBuilder() slide.Builder {
	slideBuilderOnce.Do(func() {
		rng := mrand.New(mrand.NewSource(slideBackgroundSeed()))
		backgrounds := []image.Image{
			makeSlideBackground(rng),
			makeSlideBackground(rng),
			makeSlideBackground(rng),
			makeSlideBackground(rng),
		}
		mask, overlay, shadow := makeSlideGraphTriple()
		builder := slide.NewBuilder()
		builder.SetResources(
			slide.WithBackgrounds(backgrounds),
			slide.WithGraphImages([]*slide.GraphImage{
				{OverlayImage: overlay, ShadowImage: shadow, MaskImage: mask},
			}),
		)
		slideBuilder = builder
	})
	return slideBuilder
}

func withSlideDataURL(prefix, raw string) string {
	if strings.HasPrefix(raw, "data:") {
		return raw
	}
	return prefix + raw
}

// NewSlideCaptcha issues a single-use jigsaw challenge.
func NewSlideCaptcha() (*SlideCaptcha, error) {
	id, err := newCaptchaID()
	if err != nil {
		return nil, err
	}

	slideGenMutex.Lock()
	data, err := getSlideBuilder().Make().Generate()
	slideGenMutex.Unlock()
	if err != nil {
		return nil, err
	}
	block := data.GetData()
	if block == nil {
		return nil, fmt.Errorf("slide captcha generation failed")
	}
	master, err := data.GetMasterImage().ToBase64Data()
	if err != nil {
		return nil, err
	}
	tile, err := data.GetTileImage().ToBase64Data()
	if err != nil {
		return nil, err
	}

	answerX, answerY := slideAnswerPosition(block)
	captchaStoreSave(captchaNamespaceSlide, id,
		fmt.Sprintf("%d,%d", answerX, answerY), captchaValidDuration)

	return &SlideCaptcha{
		ID:          id,
		MasterImage: withSlideDataURL("data:image/jpeg;base64,", master),
		TileImage:   withSlideDataURL("data:image/png;base64,", tile),
		TileY:       block.DY,
		TileWidth:   block.Width,
		TileHeight:  block.Height,
		ImageWidth:  slideImageWidth,
		ImageHeight: slideImageHeight,
		ExpiresIn:   int(captchaValidDuration.Seconds()),
	}, nil
}

// slideAnswerPosition returns the drop position the client must reach.
// It is the hole origin (block.X/block.Y), NOT the tile start that the
// library exposes as block.DX/block.DY: the shadow is drawn at X/Y while
// DX/DY is where the tile initially sits.
func slideAnswerPosition(block *slide.Block) (int, int) {
	return block.X, block.Y
}

// VerifySlideCaptcha checks the dragged position against the stored answer
// and applies light trail sanity. Every attempt consumes the challenge.
func VerifySlideCaptcha(id string, x, y int, trail string) bool {
	if id == "" {
		return false
	}
	stored, ok := captchaStoreTake(captchaNamespaceSlide, id)
	if !ok {
		return false
	}
	var wantX, wantY int
	if _, err := fmt.Sscanf(stored, "%d,%d", &wantX, &wantY); err != nil {
		return false
	}
	if absInt(x-wantX) > slideTolerancePx || absInt(y-wantY) > slideTolerancePx {
		return false
	}
	return checkSlideTrail(trail)
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// checkSlideTrail applies minimal plausibility rules to the drag trail:
// parseable, bounded, time-ordered. Position remains the real gate.
func checkSlideTrail(trail string) bool {
	if strings.TrimSpace(trail) == "" {
		return true
	}
	var points []slideTrailPoint
	decoder := json.NewDecoder(strings.NewReader(trail))
	if err := decoder.Decode(&points); err != nil {
		return false
	}
	if len(points) == 0 || len(points) > slideMaxTrailPoints {
		return false
	}
	for i := 1; i < len(points); i++ {
		if points[i].T < points[i-1].T {
			return false
		}
	}
	return points[len(points)-1].T-points[0].T <= slideMaxTrailSeconds*1000
}
