import type { EntityFieldData, EntitySummary, TemplateField } from "~/lib/api/types/data-contracts";

export const CLOTHING_TEMPLATE_NAME = "Clothing";
export const NIL_UUID = "00000000-0000-0000-0000-000000000000";

export const CLOTHING_FIELD_NAMES = {
  garmentType: "Garment type",
  primaryColor: "Primary color",
  size: "Size",
  season: "Season",
  condition: "Condition",
  disposition: "Move disposition",
} as const;

const FIELD_ALIASES: Record<keyof typeof CLOTHING_FIELD_NAMES, string[]> = {
  garmentType: ["garment type", "clothing type", "garment category"],
  primaryColor: ["primary color", "colour", "color"],
  size: ["size", "clothing size", "garment size"],
  season: ["season", "weather"],
  condition: ["condition", "garment condition"],
  disposition: ["move disposition", "disposition"],
};

const GARMENT_PATTERNS: Array<[RegExp, string]> = [
  [/\bquarter[ -]?zip\b/i, "Quarter-zip"],
  [/\bfull[ -]?zip\b/i, "Full-zip"],
  [/\bdress shirt\b/i, "Dress shirt"],
  [/\bbutton[ -]?down\b/i, "Button-down shirt"],
  [/\bpolo\b/i, "Polo"],
  [/\bhoodie\b/i, "Hoodie"],
  [/\bfleece\b/i, "Fleece"],
  [/\bsweater\b/i, "Sweater"],
  [/\bpullover\b/i, "Pullover"],
  [/\bt[ -]?shirt\b/i, "T-shirt"],
  [/\bshirt\b/i, "Shirt"],
  [/\bjeans?\b/i, "Jeans"],
  [/\b(?:pants|trousers)\b/i, "Pants"],
  [/\bshorts\b/i, "Shorts"],
  [/\b(?:jacket|coat)\b/i, "Outerwear"],
  [/\bdress\b/i, "Dress"],
  [/\bskirt\b/i, "Skirt"],
  [/\b(?:shoes|sneakers|boots)\b/i, "Footwear"],
];

const COLOR_PATTERNS: Array<[RegExp, string]> = [
  [/\bdark olive(?: green)?\b/i, "Dark olive green"],
  [/\bolive green\b/i, "Olive green"],
  [/\blight blue\b/i, "Light blue"],
  [/\bdark blue\b/i, "Dark blue"],
  [/\bnavy(?: blue)?\b/i, "Navy"],
  [/\bcharcoal(?: gray| grey)?\b/i, "Charcoal"],
  [/\bdark gr[ae]y\b/i, "Dark gray"],
  [/\blight gr[ae]y\b/i, "Light gray"],
  [/\bmaroon\b/i, "Maroon"],
  [/\bburgundy\b/i, "Burgundy"],
  [/\bcream\b/i, "Cream"],
  [/\bbeige\b/i, "Beige"],
  [/\bblack\b/i, "Black"],
  [/\bwhite\b/i, "White"],
  [/\bgr[ae]y\b/i, "Gray"],
  [/\bblue\b/i, "Blue"],
  [/\bgreen\b/i, "Green"],
  [/\bred\b/i, "Red"],
  [/\bpink\b/i, "Pink"],
  [/\bpurple\b/i, "Purple"],
  [/\bbrown\b/i, "Brown"],
  [/\btan\b/i, "Tan"],
  [/\byellow\b/i, "Yellow"],
  [/\borange\b/i, "Orange"],
];

export type ClothingMetadata = {
  garmentType: string;
  primaryColor: string;
  size: string;
  season: string;
  condition: string;
  disposition: string;
  inferred: Array<"garmentType" | "primaryColor" | "season">;
};

function normalized(value: string | null | undefined) {
  return (value || "").trim().toLocaleLowerCase();
}

function textField(item: Pick<EntitySummary, "fields">, key: keyof typeof CLOTHING_FIELD_NAMES) {
  const aliases = FIELD_ALIASES[key];
  return item.fields?.find(field => aliases.includes(normalized(field.name)))?.textValue.trim() || "";
}

function inferFromPatterns(text: string, patterns: Array<[RegExp, string]>) {
  return patterns.find(([pattern]) => pattern.test(text))?.[1] || "";
}

function inferredSeason(garmentType: string) {
  if (["Quarter-zip", "Full-zip", "Hoodie", "Fleece", "Sweater", "Pullover", "Outerwear"].includes(garmentType)) {
    return "Cold weather";
  }
  if (["Polo", "T-shirt", "Shorts"].includes(garmentType)) return "Warm weather";
  return garmentType ? "All season" : "";
}

export function clothingMetadata(item: EntitySummary): ClothingMetadata {
  const searchable = `${item.name} ${item.description}`;
  const inferred: ClothingMetadata["inferred"] = [];
  let garmentType = textField(item, "garmentType");
  if (!garmentType) {
    garmentType = inferFromPatterns(searchable, GARMENT_PATTERNS);
    if (garmentType) inferred.push("garmentType");
  }
  let primaryColor = textField(item, "primaryColor");
  if (!primaryColor) {
    primaryColor = inferFromPatterns(searchable, COLOR_PATTERNS);
    if (primaryColor) inferred.push("primaryColor");
  }
  let season = textField(item, "season");
  if (!season) {
    season = inferredSeason(garmentType);
    if (season) inferred.push("season");
  }
  return {
    garmentType,
    primaryColor,
    size: textField(item, "size"),
    season,
    condition: textField(item, "condition"),
    disposition: textField(item, "disposition") || "Undecided",
    inferred,
  };
}

export function isLikelyClothing(item: EntitySummary) {
  return !!clothingMetadata(item).garmentType || normalized(item.entityType?.name) === "clothing";
}

export function hasStructuredClothingMetadata(item: EntitySummary) {
  return !!textField(item, "garmentType") && !!textField(item, "primaryColor");
}

export function mergeClothingFields(item: EntitySummary, fields: EntityFieldData[]) {
  const metadata = clothingMetadata(item);
  const values: Record<keyof typeof CLOTHING_FIELD_NAMES, string> = {
    garmentType: metadata.garmentType,
    primaryColor: metadata.primaryColor,
    size: metadata.size,
    season: metadata.season,
    condition: metadata.condition,
    disposition: metadata.disposition,
  };
  const next = fields.map(field => ({ ...field }));
  for (const key of Object.keys(CLOTHING_FIELD_NAMES) as Array<keyof typeof CLOTHING_FIELD_NAMES>) {
    const existing = next.find(field => FIELD_ALIASES[key].includes(normalized(field.name)));
    if (existing) {
      if (!existing.textValue.trim() && values[key]) existing.textValue = values[key];
      continue;
    }
    next.push({
      id: null as unknown as string,
      type: "text",
      name: CLOTHING_FIELD_NAMES[key],
      textValue: values[key],
      numberValue: 0,
      booleanValue: false,
    });
  }
  return next;
}

export function clothingTemplateFields(): TemplateField[] {
  return (Object.keys(CLOTHING_FIELD_NAMES) as Array<keyof typeof CLOTHING_FIELD_NAMES>).map(key => ({
    id: NIL_UUID,
    type: "text",
    name: CLOTHING_FIELD_NAMES[key],
    textValue: key === "disposition" ? "Undecided" : "",
    numberValue: 0,
    booleanValue: false,
    timeValue: "",
  }));
}
