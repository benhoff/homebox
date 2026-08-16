import { describe, expect, it } from "vitest";
import type { EntitySummary } from "~/lib/api/types/data-contracts";
import {
  CLOTHING_FIELD_NAMES,
  clothingMetadata,
  clothingTemplateFields,
  hasStructuredClothingMetadata,
  mergeClothingFields,
} from "./closet-organization";

function item(overrides: Partial<EntitySummary> = {}): EntitySummary {
  return {
    id: "item-1",
    assetId: "1",
    name: "Dark Blue Quarter-Zip Pullover",
    description: "",
    manufacturer: "Example",
    fields: [],
    quantity: 1,
    insured: false,
    archived: false,
    createdAt: "2026-08-16T00:00:00Z",
    updatedAt: "2026-08-16T00:00:00Z",
    purchasePrice: 0,
    soldDate: "",
    tags: [],
    itemCount: 0,
    ...overrides,
  };
}

describe("closet organization", () => {
  it("infers useful display facets from existing names without changing the item", () => {
    const metadata = clothingMetadata(item());
    expect(metadata.garmentType).toBe("Quarter-zip");
    expect(metadata.primaryColor).toBe("Dark blue");
    expect(metadata.season).toBe("Cold weather");
    expect(metadata.disposition).toBe("Undecided");
    expect(metadata.inferred).toEqual(["garmentType", "primaryColor", "season"]);
  });

  it("prefers structured metadata over name inference", () => {
    const metadata = clothingMetadata(
      item({
        fields: [
          {
            id: "field-1",
            type: "text",
            name: "Garment type",
            textValue: "Sweater",
            numberValue: 0,
            booleanValue: false,
          },
          {
            id: "field-2",
            type: "text",
            name: "Primary color",
            textValue: "Teal",
            numberValue: 0,
            booleanValue: false,
          },
        ],
      })
    );
    expect(metadata.garmentType).toBe("Sweater");
    expect(metadata.primaryColor).toBe("Teal");
    expect(metadata.inferred).toEqual(["season"]);
  });

  it("adds missing clothing fields while preserving existing values", () => {
    const existing = [
      {
        id: "field-1",
        type: "text",
        name: "Size",
        textValue: "L",
        numberValue: 0,
        booleanValue: false,
      },
      {
        id: "field-2",
        type: "text",
        name: "Move disposition",
        textValue: "Sell",
        numberValue: 0,
        booleanValue: false,
      },
    ];
    const fields = mergeClothingFields(item({ fields: existing }), existing);
    expect(fields.find(field => field.name === "Size")?.textValue).toBe("L");
    expect(fields.find(field => field.name === "Move disposition")?.textValue).toBe("Sell");
    expect(fields.find(field => field.name === "Garment type")?.textValue).toBe("Quarter-zip");
    expect(fields.find(field => field.name === "Primary color")?.textValue).toBe("Dark blue");
    expect(fields).toHaveLength(6);
  });

  it("defines the complete reusable clothing template", () => {
    const fields = clothingTemplateFields();
    expect(fields.map(field => field.name)).toEqual(Object.values(CLOTHING_FIELD_NAMES));
    expect(fields.find(field => field.name === "Move disposition")?.textValue).toBe("Undecided");
    expect(hasStructuredClothingMetadata(item())).toBe(false);
  });
});
