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

For commercial licensing, please contact support@quantumnous.com
*/
import { Check, RefreshCw } from 'lucide-react'
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type PointerEvent as ReactPointerEvent,
} from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  fetchSlideCaptcha,
  type SlideCaptchaChallenge,
} from '@/features/auth/api'
import { cn } from '@/lib/utils'

import { ImageCaptcha, type ImageCaptchaValue } from './image-captcha'

export type CaptchaProof =
  | { kind: 'slide'; id: string; x: number; y: number; trail: string }
  | { kind: 'text'; id: string; code: string }

type SliderCaptchaProps = {
  value: CaptchaProof | null
  onChange: (value: CaptchaProof | null) => void
  disabled?: boolean
}

type TrailPoint = { x: number; y: number; t: number }

const MAX_TRAIL_POINTS = 200
const KEYBOARD_STEP = 4

/**
 * Self-hosted jigsaw slider for login/registration forms.
 *
 * The tile answer never leaves the server: only the dragged position travels
 * back and is verified server-side. A text-captcha fallback is built in for
 * keyboard and screen-reader users. Remount (via `key`) for a fresh
 * challenge, e.g. after a failed submit that consumed the previous one.
 */
export function SliderCaptcha(props: SliderCaptchaProps) {
  const { t } = useTranslation()
  const [mode, setMode] = useState<'slide' | 'text'>('slide')
  const [challenge, setChallenge] = useState<SlideCaptchaChallenge | null>(null)
  const [challengeId, setChallengeId] = useState('')
  const [isLoading, setIsLoading] = useState(true)
  const [hasError, setHasError] = useState(false)
  const [dragX, setDragX] = useState(0)
  const [dragging, setDragging] = useState(false)
  const [announcement, setAnnouncement] = useState('')
  const [textValue, setTextValue] = useState<ImageCaptchaValue>({
    id: '',
    code: '',
  })
  const mountedRef = useRef(true)
  const onChangeRef = useRef(props.onChange)
  onChangeRef.current = props.onChange
  const valueRef = useRef(props.value)
  valueRef.current = props.value
  const trailRef = useRef<TrailPoint[]>([])
  const areaRef = useRef<HTMLDivElement | null>(null)
  const trackRef = useRef<HTMLDivElement | null>(null)
  const tileRef = useRef<HTMLImageElement | null>(null)
  const knobRef = useRef<HTMLDivElement | null>(null)
  const gestureRef = useRef<{
    startClientX: number
    startDragX: number
    currentX: number
  } | null>(null)

  const loadChallenge = useCallback(async () => {
    if (mountedRef.current) {
      setIsLoading(true)
      setHasError(false)
    }
    try {
      const next = await fetchSlideCaptcha()
      if (!mountedRef.current) return
      trailRef.current = []
      setDragX(0)
      setChallenge(next)
      setChallengeId(next.slide_id)
      setAnnouncement(t('Slide to complete the puzzle'))
      onChangeRef.current(null)
    } catch {
      if (!mountedRef.current) return
      setHasError(true)
    } finally {
      if (mountedRef.current) {
        setIsLoading(false)
      }
    }
  }, [t])

  useEffect(() => {
    mountedRef.current = true
    void loadChallenge()
    return () => {
      mountedRef.current = false
    }
  }, [loadChallenge])

  // Auto-refresh an untouched challenge before it expires server-side, so
  // an idle form can never submit a guaranteed-stale proof.
  useEffect(() => {
    if (mode !== 'slide' || !challenge || valueRef.current) return
    const timeoutMs = challenge.expires_in * 1000
    if (!Number.isFinite(timeoutMs) || timeoutMs <= 0) return
    const timer = setTimeout(() => {
      if (!mountedRef.current || valueRef.current) return
      setAnnouncement(t('Loading puzzle'))
      void loadChallenge()
    }, timeoutMs)
    return () => clearTimeout(timer)
  }, [challenge, mode, loadChallenge, t])

  if (!challenge) {
    return (
      <div className='grid gap-2'>
        <div className='overflow-hidden rounded-md border'>
          {isLoading || !hasError ? (
            <Skeleton
              className='aspect-[15/11] w-full'
              aria-label={t('Loading puzzle')}
            />
          ) : (
            <div className='flex aspect-[15/11] w-full flex-col items-center justify-center gap-2'>
              <span className='text-muted-foreground text-xs'>
                {t('Failed to load, please refresh')}
              </span>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={() => void loadChallenge()}
              >
                <RefreshCw className='h-4 w-4' />
                {t('Refresh image captcha')}
              </Button>
            </div>
          )}
        </div>
      </div>
    )
  }

  const imgW = challenge.image_width
  const imgH = challenge.image_height
  const maxX = Math.max(imgW - challenge.tile_width, 0)

  function clampX(x: number) {
    return Math.min(Math.max(x, 0), maxX)
  }

  function emitProof(nextX: number) {
    trailRef.current.push({
      x: Math.round(nextX),
      y: challenge?.tile_y ?? 0,
      t: Date.now(),
    })
    if (trailRef.current.length > MAX_TRAIL_POINTS) {
      trailRef.current = trailRef.current.slice(-MAX_TRAIL_POINTS)
    }
    setAnnouncement(t('Verification passed'))
    onChangeRef.current({
      kind: 'slide',
      id: challengeId,
      x: Math.round(nextX),
      y: challenge?.tile_y ?? 0,
      trail: JSON.stringify(trailRef.current),
    })
  }

  function handlePointerDown(
    event: ReactPointerEvent<HTMLDivElement>,
    surfaceRef: React.RefObject<HTMLDivElement | null>
  ) {
    if (props.disabled || isLoading || mode !== 'slide') return
    const active = challenge
    if (!active) return
    if (event.target instanceof Element && event.target.closest('button')) {
      // Let interactive children (e.g. the refresh button) receive their
      // own gestures. Capturing here would retarget the click to the area
      // and silently swallow it.
      return
    }
    try {
      event.currentTarget.setPointerCapture(event.pointerId)
    } catch {
      // Pointer capture is unavailable (older browsers): dragging still
      // tracks while the pointer stays over the area.
    }
    const rect = surfaceRef.current?.getBoundingClientRect()
    if (!rect || rect.width <= 0) return
    // Grab relative: the tile follows the pointer delta instead of jumping
    // its center under the cursor.
    gestureRef.current = {
      startClientX: event.clientX,
      startDragX: dragX,
      currentX: dragX,
    }
    setDragging(true)
    trailRef.current = [
      { x: Math.round(dragX), y: active.tile_y, t: Date.now() },
    ]
  }

  function handlePointerMove(
    event: ReactPointerEvent<HTMLDivElement>,
    surfaceRef: React.RefObject<HTMLDivElement | null>
  ) {
    if (!dragging || props.disabled || mode !== 'slide') return
    const gesture = gestureRef.current
    const active = challenge
    if (!gesture || !active) return
    const rect = surfaceRef.current?.getBoundingClientRect()
    if (!rect || rect.width <= 0) return
    const nextX = clampX(
      gesture.startDragX +
        ((event.clientX - gesture.startClientX) / rect.width) * imgW
    )
    // Direct DOM write: compositor-only, no React render per frame.
    // dragX state is the gesture base; it only commits on pointer-up.
    const shiftPx = ((nextX - dragX) / imgW) * rect.width
    if (tileRef.current) {
      tileRef.current.style.transform = `translateX(${shiftPx}px)`
    }
    if (knobRef.current) {
      knobRef.current.style.transform = `translateX(${shiftPx}px)`
    }
    gesture.currentX = nextX
    trailRef.current.push({
      x: Math.round(nextX),
      y: active.tile_y,
      t: Date.now(),
    })
    if (trailRef.current.length > MAX_TRAIL_POINTS) {
      trailRef.current = trailRef.current.slice(-MAX_TRAIL_POINTS)
    }
  }

  function handlePointerUp() {
    if (!dragging) return
    setDragging(false)
    const active = challenge
    if (tileRef.current) {
      tileRef.current.style.transform = ''
    }
    if (knobRef.current) {
      knobRef.current.style.transform = ''
    }
    const gesture = gestureRef.current
    gestureRef.current = null
    if (mode !== 'slide' || props.disabled || !active || !gesture) return
    setDragX(gesture.currentX)
    setAnnouncement(t('Verification passed'))
    onChangeRef.current({
      kind: 'slide',
      id: challengeId,
      x: Math.round(gesture.currentX),
      y: active.tile_y,
      trail: JSON.stringify(trailRef.current),
    })
  }

  function handlePointerCancel() {
    // Revert: transforms are cleared, state was never touched mid-gesture.
    if (tileRef.current) {
      tileRef.current.style.transform = ''
    }
    if (knobRef.current) {
      knobRef.current.style.transform = ''
    }
    gestureRef.current = null
    setDragging(false)
  }

  function handleKnobKeyDown(event: React.KeyboardEvent<HTMLDivElement>) {
    if (props.disabled || mode !== 'slide') return
    let step = 0
    if (event.key === 'ArrowLeft') {
      step = -KEYBOARD_STEP
    } else if (event.key === 'ArrowRight') {
      step = KEYBOARD_STEP
    } else {
      return
    }
    event.preventDefault()
    const nextX = clampX(dragX + step)
    setDragX(nextX)
    emitProof(nextX)
  }

  function switchMode(next: 'slide' | 'text') {
    setMode(next)
    onChangeRef.current(null)
  }

  if (mode === 'text') {
    return (
      <div className='grid gap-2'>
        <ImageCaptcha
          value={textValue}
          onChange={(next) => {
            setTextValue(next)
            onChangeRef.current({ kind: 'text', id: next.id, code: next.code })
          }}
          disabled={props.disabled}
        />
        <div>
          <button
            type='button'
            onClick={() => switchMode('slide')}
            className='text-muted-foreground text-xs underline-offset-4 hover:underline'
          >
            {t('Back to slider captcha')}
          </button>
        </div>
      </div>
    )
  }

  const proofSet =
    props.value?.kind === 'slide' && props.value.id === challengeId

  return (
    <div className='grid gap-2'>
      <span aria-live='polite' className='sr-only'>
        {announcement}
      </span>
      <div
        ref={areaRef}
        className={cn(
          'relative w-full touch-none overflow-hidden rounded-md border select-none',
          props.disabled ? 'opacity-60' : 'cursor-grab',
          dragging && 'cursor-grabbing'
        )}
        style={{ aspectRatio: `${imgW} / ${imgH}` }}
        onPointerDown={(event) => handlePointerDown(event, areaRef)}
        onPointerMove={(event) => handlePointerMove(event, areaRef)}
        onPointerUp={handlePointerUp}
        onPointerCancel={handlePointerCancel}
      >
        <img
          src={challenge.master_image}
          alt={t('Puzzle image')}
          className='absolute inset-0 h-full w-full'
          draggable={false}
        />
        <img
          ref={tileRef}
          src={challenge.tile_image}
          alt={t('Puzzle piece')}
          className='absolute will-change-transform'
          style={{
            left: `${(dragX / imgW) * 100}%`,
            top: `${(challenge.tile_y / imgH) * 100}%`,
            width: `${(challenge.tile_width / imgW) * 100}%`,
          }}
          draggable={false}
        />
        <Button
          type='button'
          variant='secondary'
          size='icon'
          className='absolute top-1 right-1 h-7 w-7 opacity-80'
          onClick={() => void loadChallenge()}
          disabled={props.disabled || isLoading}
          aria-label={t('Refresh image captcha')}
          title={t('Refresh image captcha')}
        >
          <RefreshCw
            className={isLoading ? 'h-3.5 w-3.5 animate-spin' : 'h-3.5 w-3.5'}
          />
        </Button>
      </div>
      <div className='flex items-center gap-2'>
        <div
          ref={trackRef}
          role='slider'
          tabIndex={props.disabled ? -1 : 0}
          aria-label={t('Puzzle slider')}
          aria-valuemin={0}
          aria-valuemax={Math.round(maxX)}
          aria-valuenow={Math.round(dragX)}
          onKeyDown={handleKnobKeyDown}
          onPointerDown={(event) => handlePointerDown(event, trackRef)}
          onPointerMove={(event) => handlePointerMove(event, trackRef)}
          onPointerUp={handlePointerUp}
          onPointerCancel={handlePointerCancel}
          className={cn(
            'relative h-9 flex-1 touch-none rounded-md outline-none select-none focus-visible:ring-2',
            proofSet ? 'border border-green-500/50 bg-green-500/10' : 'bg-muted'
          )}
        >
          <div
            ref={knobRef}
            className={cn(
              'pointer-events-none absolute top-1 bottom-1 flex w-10 items-center justify-center rounded font-mono text-xs will-change-transform',
              proofSet
                ? 'bg-green-500 text-white'
                : 'bg-gradient-to-r from-muted-foreground/40 to-muted-foreground/20'
            )}
            style={{ left: `calc(${(dragX / imgW) * 100}% + 4px)` }}
          >
            {proofSet ? <Check className='h-4 w-4' /> : '»'}
          </div>
          <span className='pointer-events-none absolute inset-0 flex items-center justify-center text-xs'>
            {proofSet ? (
              <span className='inline-flex items-center gap-1 text-green-600 dark:text-green-400'>
                <Check className='h-3.5 w-3.5' />
                {t('Verification passed')}
              </span>
            ) : (
              <span className='text-muted-foreground'>
                {t('Slide to complete the puzzle')}
              </span>
            )}
          </span>
        </div>
      </div>
      <div>
        <button
          type='button'
          onClick={() => switchMode('text')}
          className='text-muted-foreground text-xs underline-offset-4 hover:underline'
        >
          {t('Use text captcha instead')}
        </button>
      </div>
    </div>
  )
}
