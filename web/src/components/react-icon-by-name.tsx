import type { IconBaseProps, IconType } from 'react-icons'
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
import { LuCreditCard } from 'react-icons/lu'
import { SiAlipay, SiStripe, SiWechat } from 'react-icons/si'

// Curated payment-brand icons, statically imported so bundlers tree-shake
// everything else away. This replaced the previous 30-pack dynamic loader
// (~22MB across route chunks): only these names resolve now. Admins needing
// other artwork use the http(s) image-URL path in getPaymentIcon; unknown
// names intentionally render nothing (same as before).
const CURATED_ICONS: Record<string, IconType> = {
  SiAlipay,
  SiStripe,
  SiWechat,
  LuCreditCard,
}

function normalizeIconName(name: string | null | undefined): string | null {
  const trimmed = name?.trim()
  if (!trimmed || !/^[A-Z][A-Za-z0-9]*$/.test(trimmed)) return null
  return trimmed
}

type ReactIconByNameProps = IconBaseProps & {
  name?: string | null
}

export function ReactIconByName({ name, ...props }: ReactIconByNameProps) {
  const iconName = normalizeIconName(name)
  const Icon = (iconName && CURATED_ICONS[iconName]) || null
  if (!Icon) return null
  return <Icon {...props} />
}
