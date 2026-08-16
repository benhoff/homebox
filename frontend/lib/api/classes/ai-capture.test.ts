import { afterEach, describe, expect, test, vi } from "vitest";
import { Requests } from "../../requests";
import { AICaptureAPI, type AICaptureDraft } from "./ai-capture";

describe("AICaptureAPI", () => {
  afterEach(() => vi.restoreAllMocks());

  test("sends multiple photos and an editable correction draft as multipart form data", async () => {
    const responseDraft: AICaptureDraft = {
      items: [
        {
          clientId: "item-1",
          name: "Drill",
          quantity: 1,
          description: "",
          manufacturer: "",
          modelNumber: "",
          entityTypeId: "type-1",
          tagIds: [],
          photoIndexes: [0, 1],
          needsReview: false,
        },
      ],
      warnings: [],
    };
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify(responseDraft), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      })
    );
    const api = new AICaptureAPI(new Requests("http://homebox.test", "Bearer token"));
    const photos = [
      new File(["one"], "one.jpg", { type: "image/jpeg" }),
      new File(["two"], "two.jpg", { type: "image/jpeg" }),
    ];

    const result = await api.analyze(
      photos,
      { id: "location-1", name: "Garage" },
      {
        draft: responseDraft,
        instruction: "Make the quantity two",
      }
    );

    expect(result.error).toBe(false);
    expect(fetchMock).toHaveBeenCalledOnce();
    const [url, init] = fetchMock.mock.calls[0]!;
    expect(url).toBe("http://homebox.test/api/v1/ai/capture/analyze");
    expect(init?.headers).toMatchObject({ Authorization: "Bearer token" });
    const form = init?.body as FormData;
    expect(form.getAll("photos")).toHaveLength(2);
    expect(form.get("locationId")).toBe("location-1");
    expect(form.get("instruction")).toBe("Make the quantity two");
    expect(JSON.parse(form.get("draft") as string)).toEqual(responseDraft);
  });
});
