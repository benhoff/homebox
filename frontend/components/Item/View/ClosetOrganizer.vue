<script setup lang="ts">
  import type {
    EntitySummary,
    EntityTemplateCreate,
    EntityTemplateUpdate,
    EntityTypeSummary,
    EntityUpdate,
    TemplateField,
  } from "~/lib/api/types/data-contracts";
  import { useI18n } from "vue-i18n";
  import MdiHanger from "~icons/mdi/hanger";
  import MdiBroom from "~icons/mdi/broom";
  import MdiLoading from "~icons/mdi/loading";
  import MdiRefresh from "~icons/mdi/refresh";
  import { toast } from "@/components/ui/sonner";
  import { Badge } from "@/components/ui/badge";
  import { Button, ButtonGroup } from "@/components/ui/button";
  import { Card } from "@/components/ui/card";
  import { Label } from "@/components/ui/label";
  import { Progress } from "@/components/ui/progress";
  import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
  import ItemCard from "@/components/Item/Card.vue";
  import {
    CLOTHING_TEMPLATE_NAME,
    clothingMetadata,
    clothingTemplateFields,
    hasStructuredClothingMetadata,
    mergeClothingFields,
  } from "~/lib/closet-organization";

  const props = defineProps<{
    items: EntitySummary[];
  }>();

  const emit = defineEmits<{
    (e: "refresh"): void;
  }>();

  const { t } = useI18n();
  const api = useUserApi();
  const confirm = useConfirm();
  const entityTypeStore = useEntityTypeStore();
  const mode = ref<"browse" | "declutter">("browse");
  const all = "__all__";
  const filters = reactive({
    garmentType: all,
    primaryColor: all,
    size: all,
    season: all,
    condition: all,
    brand: all,
  });
  const setupRunning = ref(false);
  const setupCompleted = ref(0);

  const metadataByID = computed(() => new Map(props.items.map(item => [item.id, clothingMetadata(item)])));
  const structuredCount = computed(() => props.items.filter(hasStructuredClothingMetadata).length);
  const setupProgress = computed(() => (props.items.length ? (setupCompleted.value / props.items.length) * 100 : 0));

  function sortedUnique(values: string[]) {
    return [...new Set(values.filter(Boolean))].sort((left, right) => left.localeCompare(right));
  }

  const options = computed(() => ({
    garmentType: sortedUnique([...metadataByID.value.values()].map(value => value.garmentType)),
    primaryColor: sortedUnique([...metadataByID.value.values()].map(value => value.primaryColor)),
    size: sortedUnique([...metadataByID.value.values()].map(value => value.size)),
    season: sortedUnique([...metadataByID.value.values()].map(value => value.season)),
    condition: sortedUnique([...metadataByID.value.values()].map(value => value.condition)),
    brand: sortedUnique(props.items.map(item => item.manufacturer)),
  }));

  const filteredItems = computed(() =>
    props.items.filter(item => {
      const metadata = metadataByID.value.get(item.id)!;
      return (
        (filters.garmentType === all || metadata.garmentType === filters.garmentType) &&
        (filters.primaryColor === all || metadata.primaryColor === filters.primaryColor) &&
        (filters.size === all || metadata.size === filters.size) &&
        (filters.season === all || metadata.season === filters.season) &&
        (filters.condition === all || metadata.condition === filters.condition) &&
        (filters.brand === all || item.manufacturer === filters.brand)
      );
    })
  );

  const dispositionOrder = ["Undecided", "Keep for move", "Sell", "Give away", "Donate", "Recycle", "Trash"];
  const groups = computed(() => {
    const grouped = new Map<string, EntitySummary[]>();
    for (const item of filteredItems.value) {
      const metadata = metadataByID.value.get(item.id)!;
      const key = mode.value === "browse" ? metadata.garmentType || t("closet.uncategorized") : metadata.disposition;
      const current = grouped.get(key) || [];
      current.push(item);
      grouped.set(key, current);
    }
    const keys = [...grouped.keys()].sort((left, right) => {
      if (mode.value === "declutter") {
        const leftIndex = dispositionOrder.indexOf(left);
        const rightIndex = dispositionOrder.indexOf(right);
        return (
          (leftIndex < 0 ? dispositionOrder.length : leftIndex) -
          (rightIndex < 0 ? dispositionOrder.length : rightIndex)
        );
      }
      if (left === t("closet.uncategorized")) return 1;
      if (right === t("closet.uncategorized")) return -1;
      return left.localeCompare(right);
    });
    return keys.map(name => ({
      name,
      items: grouped.get(name)!.sort((left, right) => {
        const leftMeta = metadataByID.value.get(left.id)!;
        const rightMeta = metadataByID.value.get(right.id)!;
        return (
          leftMeta.primaryColor.localeCompare(rightMeta.primaryColor) ||
          left.manufacturer.localeCompare(right.manufacturer) ||
          left.name.localeCompare(right.name)
        );
      }),
    }));
  });

  function resetFilters() {
    Object.assign(filters, {
      garmentType: all,
      primaryColor: all,
      size: all,
      season: all,
      condition: all,
      brand: all,
    });
  }

  function mergeTemplateFields(existing: TemplateField[]) {
    const next = existing.map(field => ({ ...field }));
    for (const field of clothingTemplateFields()) {
      if (!next.some(current => current.name.trim().toLocaleLowerCase() === field.name.toLocaleLowerCase())) {
        next.push(field);
      }
    }
    return next;
  }

  async function ensureClothingTemplate() {
    const templates = await api.templates.getAll();
    if (templates.error) throw new Error("load templates");
    const summary = templates.data.find(template => template.name.trim().toLocaleLowerCase() === "clothing");
    if (!summary) {
      const created = await api.templates.create({
        name: CLOTHING_TEMPLATE_NAME,
        description: t("closet.setup.template_description"),
        notes: "",
        defaultName: null,
        defaultDescription: null,
        defaultQuantity: 1,
        defaultManufacturer: null,
        defaultModelNumber: null,
        defaultWarrantyDetails: null,
        defaultLocationId: null,
        defaultTagIds: [],
        defaultInsured: false,
        defaultLifetimeWarranty: false,
        includeWarrantyFields: false,
        includePurchaseFields: false,
        includeSoldFields: false,
        fields: clothingTemplateFields(),
      } satisfies EntityTemplateCreate);
      if (created.error) throw new Error("create template");
      return created.data.id;
    }

    const current = await api.templates.get(summary.id);
    if (current.error) throw new Error("load clothing template");
    const fields = mergeTemplateFields(current.data.fields || []);
    if (fields.length !== current.data.fields.length) {
      const updated = await api.templates.update(summary.id, {
        id: summary.id,
        name: current.data.name,
        description: current.data.description,
        notes: current.data.notes,
        defaultName: current.data.defaultName || null,
        defaultDescription: current.data.defaultDescription || null,
        defaultQuantity: current.data.defaultQuantity,
        defaultManufacturer: current.data.defaultManufacturer || null,
        defaultModelNumber: current.data.defaultModelNumber || null,
        defaultWarrantyDetails: current.data.defaultWarrantyDetails || null,
        defaultLocationId: current.data.defaultLocation?.id || null,
        defaultTagIds: current.data.defaultTags?.map(tag => tag.id) || [],
        defaultInsured: current.data.defaultInsured,
        defaultLifetimeWarranty: current.data.defaultLifetimeWarranty,
        includeWarrantyFields: current.data.includeWarrantyFields,
        includePurchaseFields: current.data.includePurchaseFields,
        includeSoldFields: current.data.includeSoldFields,
        fields,
      } satisfies EntityTemplateUpdate);
      if (updated.error) throw new Error("update clothing template");
    }
    return summary.id;
  }

  async function ensureClothingType(templateID: string): Promise<EntityTypeSummary> {
    await entityTypeStore.ensureFetched();
    let clothingType = entityTypeStore.itemTypes.find(type => type.name.trim().toLocaleLowerCase() === "clothing");
    if (!clothingType) {
      const created = await api.entityTypes.create({
        name: CLOTHING_TEMPLATE_NAME,
        icon: "mdi-hanger",
        isLocation: false,
        defaultTemplateId: templateID,
      });
      if (created.error) throw new Error("create clothing type");
      clothingType = created.data;
    } else if (clothingType.defaultTemplateId !== templateID) {
      const updated = await api.entityTypes.update(clothingType.id, {
        id: clothingType.id,
        name: clothingType.name,
        icon: clothingType.icon,
        isLocation: false,
        defaultTemplateId: templateID,
      });
      if (updated.error) throw new Error("update clothing type");
      clothingType = updated.data;
    }
    await entityTypeStore.refresh();
    return clothingType;
  }

  async function enrichItem(summary: EntitySummary, clothingType: EntityTypeSummary) {
    const response = await api.items.get(summary.id);
    if (response.error) throw new Error(summary.name);
    const item = response.data;
    const fields = mergeClothingFields({ ...summary, fields: item.fields }, item.fields);
    const payload = {
      ...item,
      id: item.id,
      parentId: item.parent?.id || null,
      entityTypeId: clothingType.id,
      tagIds: item.tags.map(tag => tag.id),
      fields,
    } as EntityUpdate;
    const updated = await api.items.update(item.id, payload);
    if (updated.error) throw new Error(summary.name);
  }

  async function setupClothingMetadata() {
    if (setupRunning.value || props.items.length === 0) return;
    const { isCanceled } = await confirm.open(t("closet.setup.confirm", { count: props.items.length }));
    if (isCanceled) return;

    setupRunning.value = true;
    setupCompleted.value = 0;
    const failed: string[] = [];
    try {
      const templateID = await ensureClothingTemplate();
      const clothingType = await ensureClothingType(templateID);
      for (const item of props.items) {
        try {
          await enrichItem(item, clothingType);
        } catch {
          failed.push(item.name);
        } finally {
          setupCompleted.value++;
        }
      }
      emit("refresh");
      if (failed.length) {
        toast.error(t("closet.setup.partial", { count: failed.length }));
      } else {
        toast.success(t("closet.setup.complete", { count: props.items.length }));
      }
    } catch (error) {
      console.error(error);
      toast.error(t("closet.setup.failed"));
    } finally {
      setupRunning.value = false;
    }
  }
</script>

<template>
  <section class="mt-4 space-y-4">
    <Card class="space-y-4 p-4">
      <div class="flex flex-wrap items-start gap-3">
        <div class="min-w-0 grow">
          <div class="flex items-center gap-2">
            <MdiHanger class="size-5" />
            <h2 class="text-lg font-semibold">{{ $t("closet.title") }}</h2>
            <Badge variant="secondary">{{ items.length }}</Badge>
          </div>
          <p class="mt-1 text-sm text-muted-foreground">
            {{
              $t("closet.metadata_coverage", {
                structured: structuredCount,
                total: items.length,
              })
            }}
          </p>
        </div>
        <Button
          v-if="structuredCount < items.length"
          variant="outline"
          :disabled="setupRunning"
          @click="setupClothingMetadata"
        >
          <MdiLoading v-if="setupRunning" class="animate-spin" />
          <MdiRefresh v-else />
          {{ $t("closet.setup.action") }}
        </Button>
        <ButtonGroup>
          <Button size="sm" :variant="mode === 'browse' ? 'default' : 'outline'" @click="mode = 'browse'">
            <MdiHanger />
            {{ $t("closet.browse") }}
          </Button>
          <Button size="sm" :variant="mode === 'declutter' ? 'default' : 'outline'" @click="mode = 'declutter'">
            <MdiBroom />
            {{ $t("closet.declutter") }}
          </Button>
        </ButtonGroup>
      </div>

      <div v-if="setupRunning" class="space-y-1">
        <Progress :model-value="setupProgress" />
        <p class="text-xs text-muted-foreground">
          {{
            $t("closet.setup.progress", {
              completed: setupCompleted,
              total: items.length,
            })
          }}
        </p>
      </div>

      <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6">
        <Label v-for="filter in ['garmentType', 'primaryColor', 'size', 'season', 'condition', 'brand']" :key="filter">
          <span class="mb-1 block text-xs text-muted-foreground">{{ $t(`closet.filters.${filter}`) }}</span>
          <Select v-model="filters[filter as keyof typeof filters]">
            <SelectTrigger>
              <SelectValue :placeholder="$t('closet.filters.all')" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem :value="all">{{ $t("closet.filters.all") }}</SelectItem>
              <SelectItem v-for="value in options[filter as keyof typeof options]" :key="value" :value="value">
                {{ value }}
              </SelectItem>
            </SelectContent>
          </Select>
        </Label>
      </div>
      <div class="flex justify-end">
        <Button size="sm" variant="ghost" @click="resetFilters">{{ $t("closet.filters.clear") }}</Button>
      </div>
    </Card>

    <div v-if="groups.length" class="space-y-7">
      <section v-for="group in groups" :key="group.name">
        <div class="mb-3 flex items-center gap-2 border-b pb-2">
          <h3 class="text-lg font-semibold">{{ group.name }}</h3>
          <Badge variant="secondary">{{ group.items.length }}</Badge>
        </div>
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4">
          <ItemCard v-for="item in group.items" :key="item.id" :item="item" organized />
        </div>
      </section>
    </div>
    <Card v-else class="p-8 text-center text-muted-foreground">{{ $t("items.no_results") }}</Card>
  </section>
</template>
