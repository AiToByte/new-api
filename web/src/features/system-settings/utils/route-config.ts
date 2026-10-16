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
import { redirect } from '@tanstack/react-router'
import * as z from 'zod'

/**
 * Create search schema for settings routes with section parameter
 */
export function createSectionSearchSchema<TSectionId extends string>(
  sectionIds: readonly [TSectionId, ...TSectionId[]],
  defaultSection: TSectionId
) {
  return z.object({
    section: z
      .enum(sectionIds as unknown as [string, ...string[]])
      .optional()
      .catch(defaultSection),
  })
}

/**
 * Options for guarding a `$section` path-param route.
 */
export type SectionParamGuardOptions = {
  /** When true, invalid-section redirects replace the history entry */
  replace?: boolean
  /** Custom section validator (e.g. a registry type guard). Defaults to `sectionIds.includes`. */
  isValidSection?: (section: string) => boolean
}

/**
 * Guard a `$section` path-param route.
 *
 * Valid sections fall through; anything else redirects to `defaultSection`.
 * Extra route logic (legacy section redirects, role guards, search cleanup)
 * stays in the route's own `beforeLoad` around this call.
 */
export function guardSectionParam<TSectionId extends string>(
  section: string,
  sectionIds: readonly TSectionId[],
  defaultSection: TSectionId,
  to: string,
  options?: SectionParamGuardOptions
): void {
  const isValid =
    options?.isValidSection ??
    ((value: string) => (sectionIds as readonly string[]).includes(value))
  if (!isValid(section)) {
    throw redirect({
      to,
      params: { section: defaultSection },
      ...(options?.replace === true ? { replace: true as const } : {}),
    })
  }
}

/**
 * Redirect an index route to its default section.
 */
export function redirectToDefaultSection<TSectionId extends string>(
  defaultSection: TSectionId,
  to: string,
  options?: { replace?: boolean }
): never {
  throw redirect({
    to,
    params: { section: defaultSection },
    ...(options?.replace === true ? { replace: true as const } : {}),
  })
}

/**
 * Configuration for creating a settings route config
 */
export type SettingsRouteConfigOptions<
  TSectionId extends string,
  TComponent = unknown,
> = {
  /** Section IDs array from section-registry */
  sectionIds: readonly [TSectionId, ...TSectionId[]]
  /** Default section ID */
  defaultSection: TSectionId
  /** Settings component to render */
  component: TComponent
  /** Route path for redirect (e.g., '/system-settings/site') */
  routePath: string
  /** Whether to redirect to default section if no section is provided (default: false) */
  redirectToDefault?: boolean
}

/**
 * Create a settings route configuration with common setup
 * This abstracts the repetitive pattern of:
 * - Creating search schema
 * - Setting up validateSearch
 * - Optionally redirecting to default section
 *
 * @example
 * ```tsx
 * export const Route = createFileRoute('/_authenticated/system-settings/site')(
 *   createSettingsRouteConfig({
 *     sectionIds: SITE_SECTION_IDS,
 *     defaultSection: SITE_DEFAULT_SECTION,
 *     component: SiteSettings,
 *     routePath: '/system-settings/site',
 *     redirectToDefault: true,
 *   })
 * )
 * ```
 */
export function createSettingsRouteConfig<
  TSectionId extends string,
  TComponent = unknown,
>(options: SettingsRouteConfigOptions<TSectionId, TComponent>) {
  const {
    sectionIds,
    defaultSection,
    component,
    routePath,
    redirectToDefault = false,
  } = options

  const searchSchema = createSectionSearchSchema(sectionIds, defaultSection)

  const routeConfig = {
    validateSearch: searchSchema,
    component,
    ...(redirectToDefault && {
      beforeLoad: ({
        search,
      }: {
        search?: { section?: TSectionId | string }
      }) => {
        if (!search?.section) {
          throw redirect({
            to: routePath,
            search: { section: defaultSection } as Record<string, unknown>,
          })
        }
      },
    }),
  }

  return routeConfig
}
