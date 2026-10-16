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
import { RefreshCw } from 'lucide-react'
import { useCallback, useEffect, useId, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import {
  fetchImageCaptcha,
  type ImageCaptchaChallenge,
} from '@/features/auth/api'

export type ImageCaptchaValue = {
  id: string
  code: string
}

type ImageCaptchaProps = {
  value: ImageCaptchaValue
  onChange: (value: ImageCaptchaValue) => void
  disabled?: boolean
}

/**
 * Self-hosted image captcha field for login/registration forms.
 *
 * The challenge answer never leaves the server: only the id travels back
 * with the form. Remount (via `key`) to force a fresh challenge, e.g.
 * after a failed submit that consumed the previous one.
 */
export function ImageCaptcha(props: ImageCaptchaProps) {
  const { t } = useTranslation()
  const inputId = useId()
  const [challenge, setChallenge] = useState<ImageCaptchaChallenge | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [hasError, setHasError] = useState(false)
  const mountedRef = useRef(true)
  const onChangeRef = useRef(props.onChange)
  onChangeRef.current = props.onChange

  const loadChallenge = useCallback(async () => {
    if (mountedRef.current) {
      setIsLoading(true)
      setHasError(false)
    }
    try {
      const next = await fetchImageCaptcha()
      if (!mountedRef.current) return
      onChangeRef.current({ id: next.captcha_id, code: '' })
      setChallenge(next)
    } catch {
      if (!mountedRef.current) return
      setHasError(true)
    } finally {
      if (mountedRef.current) {
        setIsLoading(false)
      }
    }
  }, [])

  useEffect(() => {
    mountedRef.current = true
    void loadChallenge()
    return () => {
      mountedRef.current = false
    }
  }, [loadChallenge])

  function renderChallengeVisual() {
    if (isLoading) {
      return (
        <Skeleton
          className='h-full w-full'
          aria-label={t('Loading image captcha')}
        />
      )
    }
    if (hasError || !challenge) {
      return (
        <span className='text-muted-foreground px-2 text-xs'>
          {t('Failed to load, please refresh')}
        </span>
      )
    }
    return (
      <img
        src={challenge.image}
        alt={t('Image captcha, enter the characters shown')}
        className='h-full w-full object-cover'
        draggable={false}
      />
    )
  }

  return (
    <div className='grid gap-2'>
      <Label htmlFor={inputId}>{t('Image captcha')}</Label>
      <div className='flex items-stretch gap-2'>
        <div className='flex h-10 w-36 shrink-0 items-center justify-center overflow-hidden rounded-md border'>
          {renderChallengeVisual()}
        </div>
        <Input
          id={inputId}
          value={props.value.code}
          onChange={(event) =>
            props.onChange({ id: props.value.id, code: event.target.value })
          }
          placeholder={t('Enter the characters above')}
          autoComplete='off'
          autoCapitalize='characters'
          spellCheck={false}
          disabled={props.disabled}
          aria-label={t('Image captcha code')}
        />
        <Button
          type='button'
          variant='outline'
          size='icon'
          onClick={() => void loadChallenge()}
          disabled={props.disabled || isLoading}
          aria-label={t('Refresh image captcha')}
          title={t('Refresh image captcha')}
        >
          <RefreshCw
            className={isLoading ? 'h-4 w-4 animate-spin' : 'h-4 w-4'}
          />
        </Button>
      </div>
    </div>
  )
}
