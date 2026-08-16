<template>
  <Card class="relative overflow-hidden">
    <div v-if="tableRow" class="absolute left-1 top-1 z-10">
      <Checkbox
        class="size-5 bg-accent hover:bg-background-accent"
        :model-value="tableRow.getIsSelected()"
        :aria-label="$t('components.item.view.selectable.select_card')"
        @update:model-value="tableRow.toggleSelected()"
      />
    </div>
    <Badge
      v-if="organized"
      class="absolute right-2 top-2 z-10 shadow-sm"
      :class="dispositionClass"
      :title="$t('closet.disposition')"
    >
      {{ organizationMetadata.disposition }}
    </Badge>
    <NuxtLink :to="`/item/${item.id}`">
      <div class="relative h-[200px]">
        <img
          v-if="imageUrl && objectContain"
          class="absolute h-[200px] w-full object-cover blur-md"
          loading="lazy"
          :src="imageUrl"
          alt=""
        />
        <img
          v-if="imageUrl"
          class="absolute h-[200px] w-full shadow-md"
          :class="objectContain ? 'object-contain' : 'object-cover'"
          loading="lazy"
          :src="imageUrl"
          :alt="item.name"
        />
        <div class="absolute inset-x-1 bottom-1">
          <Badge class="text-wrap bg-secondary text-secondary-foreground hover:bg-secondary/70 hover:underline">
            <NuxtLink v-if="item.parent" :to="`/location/${item.parent.id}`">
              {{ locationString }}
            </NuxtLink>
          </Badge>
        </div>
      </div>
      <div class="col-span-4 flex grow flex-col gap-y-1 p-4 pt-2">
        <h2 class="line-clamp-2 text-ellipsis text-wrap text-lg font-bold">
          {{ item.name }}
        </h2>
        <p v-if="organized && item.manufacturer" class="truncate text-sm text-muted-foreground">
          {{ item.manufacturer }}
        </p>
        <div v-if="organized && organizationChips.length" class="flex flex-wrap gap-1 pb-1">
          <Badge
            v-for="chip in organizationChips"
            :key="chip.key"
            variant="outline"
            class="font-normal"
            :title="chip.inferred ? $t('closet.inferred') : undefined"
          >
            {{ chip.value }}<span v-if="chip.inferred" class="ml-0.5 text-muted-foreground">*</span>
          </Badge>
        </div>
        <Separator class="mb-1" />
        <TooltipProvider :delay-duration="0">
          <div class="flex items-center gap-2">
            <Tooltip v-if="item.insured">
              <TooltipTrigger>
                <MdiShieldCheck class="size-5 text-primary" />
              </TooltipTrigger>
              <TooltipContent>
                {{ $t("global.insured") }}
              </TooltipContent>
            </Tooltip>
            <Tooltip v-if="item.archived">
              <TooltipTrigger>
                <MdiArchive class="size-5 text-destructive" />
              </TooltipTrigger>
              <TooltipContent>
                {{ $t("global.archived") }}
              </TooltipContent>
            </Tooltip>
            <div class="grow" />
            <Tooltip>
              <TooltipTrigger>
                <Badge>
                  {{ item.quantity }}
                </Badge>
              </TooltipTrigger>
              <TooltipContent>
                {{ $t("global.quantity") }}
              </TooltipContent>
            </Tooltip>
          </div>
        </TooltipProvider>
        <Markdown
          :class="organized ? 'mb-2 line-clamp-2 text-ellipsis' : 'mb-2 line-clamp-3 text-ellipsis'"
          :source="item.description"
        />
        <div class="-mr-1 mt-auto flex flex-wrap justify-end gap-2">
          <TagChip v-for="tag in itemTags" :key="tag.id" :tag="tag" size="sm" :ancestors="tag.ancestors" />
        </div>
      </div>
    </NuxtLink>
  </Card>
</template>

<script setup lang="ts">
  import type { EntityOut, EntitySummary } from "~~/lib/api/types/data-contracts";
  import MdiShieldCheck from "~icons/mdi/shield-check";
  import MdiArchive from "~icons/mdi/archive";
  import { Badge } from "@/components/ui/badge";
  import { Card } from "@/components/ui/card";
  import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
  import { Separator } from "@/components/ui/separator";
  import Markdown from "@/components/global/Markdown.vue";
  import TagChip from "@/components/Tag/Chip.vue";
  import type { Row } from "@tanstack/vue-table";
  import { Checkbox } from "@/components/ui/checkbox";
  import { clothingMetadata } from "~/lib/closet-organization";

  const api = useUserApi();
  const preferences = useViewPreferences();

  const imageUrl = computed(() => {
    if (!props.item.imageId) {
      return "/no-image.jpg";
    }
    if (props.item.thumbnailId) {
      return api.authURL(`/entities/${props.item.id}/attachments/${props.item.thumbnailId}`);
    } else {
      return api.authURL(`/entities/${props.item.id}/attachments/${props.item.imageId}`);
    }
  });

  const itemTags = computed(() => {
    return useTagStore().withAncestors(props.item.tags);
  });

  const props = defineProps({
    item: {
      type: Object as () => EntityOut | EntitySummary,
      required: true,
    },
    locationFlatTree: {
      type: Array as () => FlatTreeItem[],
      required: false,
      default: () => [],
    },
    tableRow: {
      type: Object as () => Row<EntitySummary>,
      required: false,
      default: () => null,
    },
    organized: {
      type: Boolean,
      default: false,
    },
  });

  const organizationMetadata = computed(() => clothingMetadata(props.item as EntitySummary));
  const organizationChips = computed(() => {
    const values = [
      { key: "garmentType", value: organizationMetadata.value.garmentType },
      { key: "primaryColor", value: organizationMetadata.value.primaryColor },
      { key: "size", value: organizationMetadata.value.size },
      { key: "season", value: organizationMetadata.value.season },
      { key: "condition", value: organizationMetadata.value.condition },
    ];
    return values
      .filter(chip => chip.value)
      .slice(0, 4)
      .map(chip => ({
        ...chip,
        inferred: organizationMetadata.value.inferred.includes(chip.key as "garmentType" | "primaryColor" | "season"),
      }));
  });

  const dispositionClass = computed(() => {
    switch (organizationMetadata.value.disposition.toLocaleLowerCase()) {
      case "trash":
        return "bg-destructive text-destructive-foreground";
      case "sell":
        return "bg-blue-600 text-white";
      case "give away":
      case "donate":
        return "bg-emerald-600 text-white";
      case "recycle":
        return "bg-teal-600 text-white";
      case "keep":
      case "keep for move":
        return "bg-primary text-primary-foreground";
      default:
        return "bg-secondary text-secondary-foreground";
    }
  });

  const objectContain = computed(() => imageUrl.value !== "/no-image.jpg" && !preferences.value.legacyImageFit);

  const locationString = computed(
    () => props.locationFlatTree.find(l => l.id === props.item.parent?.id)?.treeString || props.item.parent?.name
  );
</script>

<style lang="css"></style>
