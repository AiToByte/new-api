package middleware

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// ImageCaptchaCheck verifies human proof on login and registration. It
// mirrors TurnstileCheck: the client passes the challenge as query
// parameters. A jigsaw proof (slide_id/slide_x/slide_y) is preferred; the
// text proof (captcha_id/captcha_code) remains as the accessible fallback.
func ImageCaptchaCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !common.CaptchaEnabled {
			c.Next()
			return
		}
		if c.Query("slide_id") != "" {
			if verifySlideProof(c) {
				c.Next()
				return
			}
			c.Abort()
			return
		}
		if verifyTextProof(c) {
			c.Next()
			return
		}
		c.Abort()
	}
}

func verifySlideProof(c *gin.Context) bool {
	id := c.Query("slide_id")
	xParam := c.Query("slide_x")
	yParam := c.Query("slide_y")
	if id == "" || xParam == "" || yParam == "" {
		common.ApiErrorT(c, "Captcha is required, please complete the image verification")
		return false
	}
	x, xErr := strconv.Atoi(xParam)
	y, yErr := strconv.Atoi(yParam)
	if xErr != nil || yErr != nil {
		common.ApiErrorT(c, "Captcha is incorrect or has expired, please try again")
		return false
	}
	if !common.VerifySlideCaptcha(id, x, y, c.Query("slide_trail")) {
		common.SysLog(common.LogText("slide captcha verification failed from %s", c.ClientIP()))
		common.ApiErrorT(c, "Captcha is incorrect or has expired, please try again")
		return false
	}
	return true
}

func verifyTextProof(c *gin.Context) bool {
	id := c.Query("captcha_id")
	code := c.Query("captcha_code")
	if id == "" || code == "" {
		common.ApiErrorT(c, "Captcha is required, please complete the image verification")
		return false
	}
	if !common.VerifyImageCaptcha(id, code) {
		common.SysLog(common.LogText("image captcha verification failed from %s", c.ClientIP()))
		common.ApiErrorT(c, "Captcha is incorrect or has expired, please try again")
		return false
	}
	return true
}
