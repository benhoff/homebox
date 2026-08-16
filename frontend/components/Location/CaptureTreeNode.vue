<script setup lang="ts">
  import type { TreeItem } from "~~/lib/api/types/data-contracts";
  import MdiChevronRight from "~icons/mdi/chevron-right";
  import MdiMapMarker from "~icons/mdi/map-marker";
  import LocationCaptureTreeNode from "./CaptureTreeNode.vue";

  const props = defineProps<{
    item: TreeItem;
    selectedId?: string;
  }>();
  const emit = defineEmits<{
    select: [location: { id: string; name: string }];
  }>();
  const expanded = ref(false);
  const locationChildren = computed(() => (props.item.children ?? []).filter(child => child.type === "location"));
</script>

<template>
  <li role="treeitem" :aria-expanded="locationChildren.length > 0 ? expanded : undefined">
    <div class="flex items-center gap-1">
      <button
        type="button"
        class="flex size-9 shrink-0 items-center justify-center rounded-md hover:bg-accent"
        :class="{ invisible: locationChildren.length === 0 }"
        :aria-label="expanded ? 'Collapse location' : 'Expand location'"
        @click="expanded = !expanded"
      >
        <MdiChevronRight class="size-5 transition-transform" :class="{ 'rotate-90': expanded }" />
      </button>
      <button
        type="button"
        class="flex min-w-0 grow items-center gap-2 rounded-md border px-3 py-2 text-left hover:bg-accent"
        :class="selectedId === item.id ? 'border-primary bg-primary/10 text-primary' : 'border-transparent'"
        @click="emit('select', { id: item.id, name: item.name })"
      >
        <MdiMapMarker class="size-5 shrink-0" />
        <span class="truncate">{{ item.name }}</span>
      </button>
    </div>
    <ul v-if="expanded" role="group" class="ml-5 border-l pl-2">
      <LocationCaptureTreeNode
        v-for="child in locationChildren"
        :key="child.id"
        :item="child"
        :selected-id="selectedId"
        @select="emit('select', $event)"
      />
    </ul>
  </li>
</template>
