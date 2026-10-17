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
'use strict'

// Production-only loader for src/i18n/locales/en.json. 99% of that file is
// identity mapping (key === value, keys are the English source strings), and
// i18next reproduces those entries verbatim without the resource: a missing
// key resolves to the key itself and still runs interpolation on it
// (Translator.translate -> extendTranslation). Dropping the identity entries
// shrinks the entry chunk by roughly a quarter (about 240KB raw / 55KB gzip)
// with no behavioural difference; non-identity mappings (punctuation and
// embeds translated from Chinese) are kept. The dev server intentionally uses
// the untouched file so missingKey debug logging stays identical.

function slimEnglishLocale(locale) {
  const translation = locale && locale.translation
  if (!translation || typeof translation !== 'object') return locale
  const slim = {}
  for (const [key, value] of Object.entries(translation)) {
    if (typeof value === 'string' && value === key) continue
    slim[key] = value
  }
  return { ...locale, translation: slim }
}

function enSlimLoader(source) {
  const text = String(source).replace(/^\uFEFF/, '')
  return JSON.stringify(slimEnglishLocale(JSON.parse(text)))
}

module.exports = enSlimLoader
module.exports.slimEnglishLocale = slimEnglishLocale
