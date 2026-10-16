package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// GetImageCaptcha issues a single-use image challenge for login and
// registration forms. The answer lives server-side and is never exposed.
func GetImageCaptcha(c *gin.Context) {
	captcha, err := common.NewImageCaptcha()
	if err != nil {
		common.SysLog(common.LogText("failed to generate image captcha: %s", err.Error()))
		common.ApiErrorT(c, "Failed to generate captcha, please try again")
		return
	}
	common.ApiSuccess(c, gin.H{
		"captcha_id": captcha.ID,
		"image":      captcha.Image,
		"expires_in": captcha.ExpiresIn,
	})
}

// GetSlideCaptcha issues a single-use jigsaw challenge. The answer (tile
// position) lives server-side and is never exposed.
func GetSlideCaptcha(c *gin.Context) {
	captcha, err := common.NewSlideCaptcha()
	if err != nil {
		common.SysLog(common.LogText("failed to generate slide captcha: %s", err.Error()))
		common.ApiErrorT(c, "Failed to generate captcha, please try again")
		return
	}
	common.ApiSuccess(c, gin.H{
		"slide_id":     captcha.ID,
		"master_image": captcha.MasterImage,
		"tile_image":   captcha.TileImage,
		"tile_y":       captcha.TileY,
		"tile_width":   captcha.TileWidth,
		"tile_height":  captcha.TileHeight,
		"image_width":  captcha.ImageWidth,
		"image_height": captcha.ImageHeight,
		"expires_in":   captcha.ExpiresIn,
	})
}
