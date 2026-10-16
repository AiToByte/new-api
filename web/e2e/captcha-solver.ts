/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type { Page } from '@playwright/test'

export type SlideChallengeImages = {
  master_image: string
  tile_image: string
  image_width: number
  image_height: number
  tile_y: number
}

// Solve a jigsaw challenge WITHOUT any server-side answer: the hole is the
// only compact shadowed region, so the tile alpha-footprint correlates
// against an illumination-normalized map (pixel / local mean). Background
// gradients, bands and decoys normalize to ~1.0; the hole scores ~0.65.
// Calibrated offline against saved challenges; score >= 0.85 means the
// caller should refresh for a new challenge instead of submitting.
export async function solveSlideX(
  page: Page,
  images: SlideChallengeImages
): Promise<{ x: number; score: number }> {
  return page.evaluate(
    async ({
      master,
      tile,
      tileY,
    }: {
      master: string
      tile: string
      tileY: number
    }) => {
      const load = (src: string) =>
        new Promise<HTMLImageElement>((resolve, reject) => {
          const img = new Image()
          img.addEventListener('load', () => resolve(img), { once: true })
          img.addEventListener(
            'error',
            () => reject(new Error('captcha image load failed')),
            { once: true }
          )
          img.src = src
        })
      const [m, t] = await Promise.all([load(master), load(tile)])
      const mw = m.naturalWidth
      const mh = m.naturalHeight
      const tw = t.naturalWidth
      const th = t.naturalHeight
      const mc = document.createElement('canvas')
      mc.width = mw
      mc.height = mh
      const mctx = mc.getContext('2d', { willReadFrequently: true })
      if (!mctx) throw new Error('no 2d context')
      mctx.drawImage(m, 0, 0)
      const md = mctx.getImageData(0, 0, mw, mh).data
      const tc = document.createElement('canvas')
      tc.width = tw
      tc.height = th
      const tctx = tc.getContext('2d', { willReadFrequently: true })
      if (!tctx) throw new Error('no 2d context')
      tctx.drawImage(t, 0, 0)
      const td = tctx.getImageData(0, 0, tw, th).data
      const opaque: number[] = []
      for (let i = 0; i < tw * th; i++) {
        if (td[i * 4 + 3] > 128) opaque.push(i)
      }
      const bright = new Float64Array(mw * mh)
      for (let i = 0; i < mw * mh; i++) {
        bright[i] = (md[i * 4] + md[i * 4 + 1] + md[i * 4 + 2]) / 3
      }
      const integ = new Float64Array((mw + 1) * (mh + 1))
      for (let y = 0; y < mh; y++) {
        let row = 0
        for (let x = 0; x < mw; x++) {
          row += bright[y * mw + x]
          integ[(y + 1) * (mw + 1) + x + 1] = integ[y * (mw + 1) + x + 1] + row
        }
      }
      const boxMean = (x0: number, y0: number, x1: number, y1: number) => {
        const xa = Math.max(0, x0)
        const ya = Math.max(0, y0)
        const xb = Math.min(mw, x1)
        const yb = Math.min(mh, y1)
        const area = Math.max(1, (xb - xa) * (yb - ya))
        const s =
          integ[yb * (mw + 1) + xb] -
          integ[ya * (mw + 1) + xb] -
          integ[yb * (mw + 1) + xa] +
          integ[ya * (mw + 1) + xa]
        return s / area
      }
      const R = 25
      let bestX = 0
      let bestScore = Infinity
      for (let x = 0; x <= mw - tw; x += 1) {
        for (let dy = -40; dy <= 24; dy += 2) {
          let sum = 0
          for (const i of opaque) {
            const px = i % tw
            const myy = tileY + ((i / tw) | 0) + dy
            if (myy < 0 || myy >= mh) continue
            const b = bright[myy * mw + x + px]
            const m0 = boxMean(x + px - R, myy - R, x + px + R + 1, myy + R + 1)
            sum += b / Math.max(1, m0)
          }
          const mean = sum / opaque.length
          if (mean < bestScore) {
            bestScore = mean
            bestX = x
          }
        }
      }
      return { x: bestX, score: bestScore }
    },
    {
      master: images.master_image,
      tile: images.tile_image,
      tileY: images.tile_y,
    }
  )
}
