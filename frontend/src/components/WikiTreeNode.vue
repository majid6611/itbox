<script setup lang="ts">
import type { WikiPageSummary } from '../api/types'

export interface WikiTreeNodeData {
  name: string
  fullPath: string
  page?: WikiPageSummary
  children: WikiTreeNodeData[]
}

defineProps<{
  node: WikiTreeNodeData
  activePath: string
}>()
</script>

<template>
  <li>
    <router-link
      v-if="node.page"
      :to="`/portal/wiki/${node.fullPath}`"
      :class="{ active: node.fullPath === activePath }"
    >
      {{ node.name }}
    </router-link>
    <span v-else class="folder">{{ node.name }}</span>
    <ul v-if="node.children.length" class="children">
      <WikiTreeNode v-for="c in node.children" :key="c.fullPath" :node="c" :active-path="activePath" />
    </ul>
  </li>
</template>

<style scoped>
li {
  list-style: none;
}
.children {
  margin: 0;
  padding-left: 0.9rem;
}
.folder {
  display: block;
  padding: 0.2rem 0;
  font-size: 0.85rem;
  opacity: 0.7;
}
a {
  display: block;
  padding: 0.2rem 0;
  font-size: 0.9rem;
  text-decoration: none;
  color: inherit;
}
a:hover {
  text-decoration: underline;
}
a.active {
  font-weight: bold;
  color: #4a9eff;
}
</style>
