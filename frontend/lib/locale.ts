function normalizeLocale(locale: string): string {
  return locale.trim().replaceAll("_", "-").toLowerCase();
}

/**
 * Match supported locales in browser preference order.
 *
 * Each preferred locale must get both an exact and a language-family match
 * before considering the next preference. For example, `en-US, zh-CN` should
 * resolve to supported `en`, not `zh-CN` merely because the latter is exact.
 */
export function matchPreferredLocale(supportedLocales: string[], preferredLocales: string[]): string | null {
  const supported = supportedLocales.map(locale => ({ locale, normalized: normalizeLocale(locale) }));

  for (const preferredLocale of preferredLocales) {
    const preferred = normalizeLocale(preferredLocale);
    if (!preferred) {
      continue;
    }

    const exact = supported.find(candidate => candidate.normalized === preferred);
    if (exact) {
      return exact.locale;
    }

    const language = preferred.split("-")[0];
    const languageMatch = supported.find(
      candidate => candidate.normalized === language || candidate.normalized.startsWith(`${language}-`)
    );
    if (languageMatch) {
      return languageMatch.locale;
    }
  }

  return null;
}
