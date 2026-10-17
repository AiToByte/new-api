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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { fetchImageCaptcha } from '@/features/auth/api'

import { ImageCaptcha, type ImageCaptchaValue } from '../image-captcha'

vi.mock('@/features/auth/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/auth/api')>()),
  fetchImageCaptcha: vi.fn(),
}))

const mockedFetch = vi.mocked(fetchImageCaptcha)

function Harness() {
  const [value, setValue] = useState<ImageCaptchaValue>({ id: '', code: '' })
  return (
    <>
      <ImageCaptcha value={value} onChange={setValue} />
      <output data-testid='captcha-state'>{`${value.id}:${value.code}`}</output>
    </>
  )
}

describe('ImageCaptcha', () => {
  it('renders the fetched image with an accessible name and code input', async () => {
    mockedFetch.mockResolvedValueOnce({
      captcha_id: 'challenge-1',
      image: 'data:image/png;base64,AAA',
      expires_in: 300,
    })
    render(<Harness />)

    expect(
      await screen.findByAltText('Image captcha, enter the characters shown')
    ).toHaveAttribute('src', 'data:image/png;base64,AAA')
    expect(screen.getByLabelText('Image captcha code')).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Refresh image captcha' })
    ).toBeInTheDocument()
    expect(screen.getByTestId('captcha-state')).toHaveTextContent(
      'challenge-1:'
    )
  })

  it('reports typed code with the current challenge id', async () => {
    const user = userEvent.setup()
    mockedFetch.mockResolvedValueOnce({
      captcha_id: 'challenge-2',
      image: 'data:image/png;base64,BBB',
      expires_in: 300,
    })
    render(<Harness />)
    await screen.findByAltText('Image captcha, enter the characters shown')

    await user.type(screen.getByLabelText('Image captcha code'), 'aBc12')
    expect(screen.getByTestId('captcha-state')).toHaveTextContent(
      'challenge-2:aBc12'
    )
  })

  it('refresh fetches a new challenge and clears the typed code', async () => {
    const user = userEvent.setup()
    mockedFetch
      .mockResolvedValueOnce({
        captcha_id: 'challenge-3',
        image: 'data:image/png;base64,CCC',
        expires_in: 300,
      })
      .mockResolvedValueOnce({
        captcha_id: 'challenge-4',
        image: 'data:image/png;base64,DDD',
        expires_in: 300,
      })
    render(<Harness />)
    await screen.findByAltText('Image captcha, enter the characters shown')

    await user.type(screen.getByLabelText('Image captcha code'), 'zz9')
    expect(screen.getByTestId('captcha-state')).toHaveTextContent(
      'challenge-3:zz9'
    )

    await user.click(
      screen.getByRole('button', { name: 'Refresh image captcha' })
    )
    await waitFor(() =>
      expect(screen.getByTestId('captcha-state')).toHaveTextContent(
        'challenge-4:'
      )
    )
    expect(screen.getByLabelText('Image captcha code')).toHaveValue('')
  })

  it('shows a retry state when the challenge fails to load', async () => {
    const user = userEvent.setup()
    mockedFetch
      .mockRejectedValueOnce(new Error('network down'))
      .mockResolvedValueOnce({
        captcha_id: 'challenge-5',
        image: 'data:image/png;base64,EEE',
        expires_in: 300,
      })
    render(<Harness />)

    expect(
      await screen.findByText('Failed to load, please refresh')
    ).toBeInTheDocument()

    await user.click(
      screen.getByRole('button', { name: 'Refresh image captcha' })
    )
    expect(
      await screen.findByAltText('Image captcha, enter the characters shown')
    ).toBeInTheDocument()
  })
})
