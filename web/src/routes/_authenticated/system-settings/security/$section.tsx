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
import { createFileRoute, redirect } from '@tanstack/react-router'

import { SecuritySettings } from '@/features/system-settings/security'
import {
  SECURITY_DEFAULT_SECTION,
  SECURITY_SECTION_IDS,
} from '@/features/system-settings/security/section-registry.tsx'
import { guardSectionParam } from '@/features/system-settings/utils/route-config'

export const Route = createFileRoute(
  '/_authenticated/system-settings/security/$section'
)({
  beforeLoad: ({ params }) => {
    if (params.section === 'sensitive-words') {
      throw redirect({
        to: '/system-settings/request-policies/$section',
        params: { section: 'filtering' },
        replace: true,
      })
    }
    guardSectionParam(
      params.section,
      SECURITY_SECTION_IDS,
      SECURITY_DEFAULT_SECTION,
      '/system-settings/security/$section'
    )
  },
  component: SecuritySettings,
})
