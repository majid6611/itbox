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
  padding: 0.3rem 0.4rem;
  font-size: 0.82rem;
  font-weight: 600;
  color: var(--text-faint);
  text-transform: uppercase;
  letter-spacing: 0.03em;
}
a {
  display: block;
  padding: 0.3rem 0.4rem;
  border-radius: 6px;
  font-size: 0.88rem;
  text-decoration: none;
  color: var(--text-dim);
}
a:hover {
  background: var(--surface-hover);
  color: var(--text);
}
a.active {
  font-weight: 600;
  color: var(--accent);
  background: var(--accent-soft);
}
</style>
