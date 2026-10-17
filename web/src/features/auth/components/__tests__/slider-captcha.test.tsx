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
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { fetchSlideCaptcha } from '@/features/auth/api'

import { SliderCaptcha, type CaptchaProof } from '../slider-captcha'

vi.mock('@/features/auth/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/auth/api')>()),
  fetchImageCaptcha: vi.fn(),
  fetchSlideCaptcha: vi.fn(),
}))

const mockedFetchSlide = vi.mocked(fetchSlideCaptcha)

const slideChallenge = {
  slide_id: 'slide-1',
  master_image: 'data:image/jpeg;base64,AAA',
  tile_image: 'data:image/png;base64,BBB',
  tile_y: 80,
  tile_width: 64,
  tile_height: 64,
  image_width: 300,
  image_height: 220,
  expires_in: 300,
}

function Harness() {
  const [proof, setProof] = useState<CaptchaProof | null>(null)
  return (
    <>
      <SliderCaptcha value={proof} onChange={setProof} />
      <output data-testid='proof-state'>{JSON.stringify(proof)}</output>
    </>
  )
}

describe('SliderCaptcha', () => {
  it('renders puzzle images and an accessible slider after fetch', async () => {
    mockedFetchSlide.mockResolvedValueOnce({ ...slideChallenge })
    render(<Harness />)

    expect(await screen.findByAltText('Puzzle image')).toHaveAttribute(
      'src',
      'data:image/jpeg;base64,AAA'
    )
    expect(screen.getByAltText('Puzzle piece')).toBeInTheDocument()
    expect(
      screen.getByRole('slider', { name: 'Puzzle slider' })
    ).toHaveAttribute('aria-valuemax', '236')
    // Visible hint plus the screen-reader live region announce the same text.
    expect(screen.getAllByText('Slide to complete the puzzle')).toHaveLength(2)
  })

  it('arrow keys move the knob and report a slide proof', async () => {
    const user = userEvent.setup()
    mockedFetchSlide.mockResolvedValueOnce({ ...slideChallenge })
    render(<Harness />)
    const knob = await screen.findByRole('slider', {
      name: 'Puzzle slider',
    })

    knob.focus()
    await user.keyboard('{ArrowRight}')
    await waitFor(() =>
      expect(screen.getByTestId('proof-state')).toHaveTextContent(
        '"kind":"slide"'
      )
    )
    expect(screen.getByTestId('proof-state')).toHaveTextContent(
      '"id":"slide-1"'
    )
    expect(knob).toHaveAttribute('aria-valuenow', '4')
    expect(screen.getAllByText('Verification passed')).toHaveLength(2)
  })

  it('refresh fetches a new challenge and clears the proof', async () => {
    const user = userEvent.setup()
    mockedFetchSlide
      .mockResolvedValueOnce({ ...slideChallenge })
      .mockResolvedValueOnce({ ...slideChallenge, slide_id: 'slide-2' })
    render(<Harness />)
    const knob = await screen.findByRole('slider', {
      name: 'Puzzle slider',
    })

    knob.focus()
    await user.keyboard('{ArrowRight}')
    await waitFor(() =>
      expect(screen.getByTestId('proof-state')).toHaveTextContent(
        '"id":"slide-1"'
      )
    )

    await user.click(
      screen.getByRole('button', { name: 'Refresh image captcha' })
    )
    await waitFor(() =>
      expect(screen.getByTestId('proof-state')).toHaveTextContent('null')
    )
    expect(mockedFetchSlide).toHaveBeenCalledTimes(2)
  })

  it('falls back to text captcha and back', async () => {
    const user = userEvent.setup()
    mockedFetchSlide.mockResolvedValueOnce({ ...slideChallenge })
    render(<Harness />)
    await screen.findByRole('slider', { name: 'Puzzle slider' })

    await user.click(screen.getByText('Use text captcha instead'))
    expect(screen.getByLabelText('Image captcha code')).toBeInTheDocument()

    await user.click(screen.getByText('Back to slider captcha'))
    expect(
      screen.getByRole('slider', { name: 'Puzzle slider' })
    ).toBeInTheDocument()
  })

  it('auto-refreshes an untouched challenge when it expires', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    try {
      mockedFetchSlide
        .mockResolvedValueOnce({ ...slideChallenge, expires_in: 120 })
        .mockResolvedValueOnce({ ...slideChallenge, slide_id: 'slide-10' })
      render(<Harness />)
      await screen.findByRole('slider', { name: 'Puzzle slider' })
      expect(mockedFetchSlide).toHaveBeenCalledTimes(1)

      await vi.advanceTimersByTimeAsync(120 * 1000)
      await waitFor(() => expect(mockedFetchSlide).toHaveBeenCalledTimes(2))
    } finally {
      vi.useRealTimers()
    }
  })

  it('does not auto-refresh once a proof exists', async () => {
    const user = userEvent.setup()
    vi.useFakeTimers({ shouldAdvanceTime: true })
    try {
      mockedFetchSlide.mockResolvedValueOnce({
        ...slideChallenge,
        expires_in: 120,
      })
      render(<Harness />)
      const knob = await screen.findByRole('slider', {
        name: 'Puzzle slider',
      })

      knob.focus()
      await user.keyboard('{ArrowRight}')
      await waitFor(() =>
        expect(screen.getByTestId('proof-state')).toHaveTextContent(
          '"kind":"slide"'
        )
      )

      await vi.advanceTimersByTimeAsync(120 * 1000)
      expect(mockedFetchSlide).toHaveBeenCalledTimes(1)
    } finally {
      vi.useRealTimers()
    }
  })

  it('pressing the refresh button does not start a drag gesture', async () => {
    const user = userEvent.setup()
    mockedFetchSlide
      .mockResolvedValueOnce({ ...slideChallenge })
      .mockResolvedValueOnce({ ...slideChallenge, slide_id: 'slide-9' })
    render(<Harness />)
    await screen.findByRole('slider', { name: 'Puzzle slider' })
    const refresh = screen.getByRole('button', {
      name: 'Refresh image captcha',
    })

    // A real pointer press on the button bubbles to the drag area; it must
    // not arm a drag, otherwise pointer capture would swallow the click.
    fireEvent.pointerDown(refresh, {
      clientX: 10,
      clientY: 10,
      button: 0,
      pointerId: 7,
    })
    fireEvent.pointerUp(refresh, {
      clientX: 10,
      clientY: 10,
      button: 0,
      pointerId: 7,
    })
    await user.click(refresh)

    await waitFor(() =>
      expect(screen.getByTestId('proof-state')).toHaveTextContent('null')
    )
    expect(mockedFetchSlide).toHaveBeenCalledTimes(2)
  })
})
