import { describe, expect, it } from "vitest";
import { matchPreferredLocale } from "./locale";

describe("matchPreferredLocale", () => {
  it("matches the first browser preference by base language before considering later preferences", () => {
    expect(matchPreferredLocale(["en", "zh-CN"], ["en-US", "zh-CN"])).toBe("en");
  });

  it("prefers an exact locale match", () => {
    expect(matchPreferredLocale(["pt-BR", "pt-PT"], ["pt-PT", "pt-BR"])).toBe("pt-PT");
  });

  it("matches locales case-insensitively and accepts underscores", () => {
    expect(matchPreferredLocale(["zh-TW"], ["ZH_tw"])).toBe("zh-TW");
  });

  it("moves to the next browser preference when a language is unsupported", () => {
    expect(matchPreferredLocale(["en", "fr"], ["cy-GB", "fr-CA"])).toBe("fr");
  });

  it("returns null when no browser preference is supported", () => {
    expect(matchPreferredLocale(["en", "fr"], ["cy-GB", "ga-IE"])).toBeNull();
  });
});
