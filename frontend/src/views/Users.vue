<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useUsersStore } from '../stores/users'
import { useGroupsStore } from '../stores/groups'

const users = useUsersStore()
const groups = useGroupsStore()
const form = reactive({ username: '', email: '', name: '', password: '', group: '' })
const newGroupName = ref('')
const busy = reactive<Record<string, boolean>>({})
const editing = reactive<Record<string, { email: string; name: string }>>({})
const revealed = ref<{ username: string; password: string } | null>(null)

onMounted(() => {
  users.fetchAll()
  groups.fetchAll()
})

function randomHex(bytes: number) {
  const b = new Uint8Array(bytes)
  crypto.getRandomValues(b)
  return Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('')
}

function generatePassword() {
  form.password = randomHex(16)
}

async function submitCreate() {
  busy.create = true
  try {
    const password = await users.create({ ...form })
    revealed.value = { username: form.username, password }
    form.username = ''
    form.email = ''
    form.name = ''
    form.password = ''
    form.group = ''
  } finally {
    busy.create = false
  }
}

function startEdit(u: { username: string; email: string; name: string }) {
  editing[u.username] = { email: u.email, name: u.name }
}

function cancelEdit(username: string) {
  delete editing[username]
}

async function saveEdit(username: string) {
  busy[username] = true
  try {
    await users.update(username, editing[username])
    cancelEdit(username)
  } finally {
    busy[username] = false
  }
}

async function changeGroup(username: string, group: string) {
  busy[username] = true
  try {
    await users.changeGroup(username, group)
  } finally {
    busy[username] = false
  }
}

async function resetPassword(username: string) {
  busy[username] = true
  try {
    const password = await users.resetPassword(username, '')
    revealed.value = { username, password }
  } finally {
    busy[username] = false
  }
}

async function remove(username: string) {
  busy[username] = true
  try {
    await users.remove(username)
  } finally {
    busy[username] = false
  }
}

async function disable(username: string) {
  if (!confirm(`Disable ${username}? They'll lose access everywhere until re-enabled.`)) return
  busy[username] = true
  try {
    await users.disable(username)
  } finally {
    busy[username] = false
  }
}

async function enable(username: string) {
  busy[username] = true
  try {
    const password = await users.enable(username)
    revealed.value = { username, password }
  } finally {
    busy[username] = false
  }
}

async function createGroup() {
  busy.createGroup = true
  try {
    await groups.create(newGroupName.value)
    newGroupName.value = ''
  } finally {
    busy.createGroup = false
  }
}

async function removeGroup(name: string, memberCount: number) {
  if (memberCount > 0 && !confirm(`"${name}" has ${memberCount} member(s). Delete it anyway? They'll need to be moved to another group.`)) {
    return
  }
  busy['group:' + name] = true
  try {
    await groups.remove(name)
  } finally {
    busy['group:' + name] = false
  }
}
</script>

<template>
  <div>
    <h1>Users</h1>

    <div v-if="revealed" class="revealed">
      Password for <strong>{{ revealed.username }}</strong>: <code>{{ revealed.password }}</code>
      <button type="button" @click="revealed = null">Dismiss</button>
    </div>

    <div v-if="!users.loading && !users.available" class="notice">
      The Identity module isn't installed yet, so there are no company user
      accounts to manage. Install it from the
      <router-link to="/modules">Module Store</router-link>
      first.
    </div>

    <template v-else>
      <div v-if="!groups.loading && groups.groups.length === 0" class="notice">
        No groups yet — create one below before adding users (every user needs a group).
      </div>

      <form @submit.prevent="submitCreate" class="create-form">
        <label>
          Username
          <input v-model="form.username" type="text" required />
        </label>
        <label>
          Full name
          <input v-model="form.name" type="text" required />
        </label>
        <label>
          Email
          <input v-model="form.email" type="email" required />
        </label>
        <label>
          Group
          <select v-model="form.group" required>
            <option value="" disabled>Select a group</option>
            <option v-for="g in groups.groups" :key="g.name" :value="g.name">{{ g.name }}</option>
          </select>
        </label>
        <label>
          Password
          <div class="secret-field">
            <input v-model="form.password" type="text" placeholder="Leave blank to auto-generate" minlength="8" />
            <button type="button" @click="generatePassword">Generate</button>
          </div>
          <span class="hint">At least 8 characters, or leave blank</span>
        </label>
        <button type="submit" :disabled="busy.create || groups.groups.length === 0">
          {{ busy.create ? 'Creating…' : 'Create user' }}
        </button>
      </form>

      <table class="users-table">
        <thead>
          <tr>
            <th>Username</th>
            <th>Name</th>
            <th>Email</th>
            <th>Group</th>
            <th>Status</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="u in users.users" :key="u.username" :class="{ disabled: u.disabled }">
            <td>{{ u.username }}</td>
            <template v-if="editing[u.username]">
              <td><input v-model="editing[u.username].name" type="text" /></td>
              <td><input v-model="editing[u.username].email" type="email" /></td>
              <td>{{ u.group || '—' }}</td>
              <td></td>
              <td class="row-actions">
                <button :disabled="busy[u.username]" @click="saveEdit(u.username)">Save</button>
                <button class="secondary" :disabled="busy[u.username]" @click="cancelEdit(u.username)">Cancel</button>
              </td>
            </template>
            <template v-else>
              <td>{{ u.name }}</td>
              <td>{{ u.email }}</td>
              <td>
                <select :value="u.group" :disabled="busy[u.username]" @change="changeGroup(u.username, ($event.target as HTMLSelectElement).value)">
                  <option v-for="g in groups.groups" :key="g.name" :value="g.name">{{ g.name }}</option>
                </select>
              </td>
              <td>
                <span class="pill" :class="u.disabled ? 'pill-neutral' : 'pill-good'">{{ u.disabled ? 'disabled' : 'active' }}</span>
              </td>
              <td class="row-actions">
                <button class="secondary" :disabled="busy[u.username]" @click="startEdit(u)">Edit</button>
                <button class="secondary" :disabled="busy[u.username]" @click="resetPassword(u.username)">Reset password</button>
                <button v-if="u.disabled" :disabled="busy[u.username]" @click="enable(u.username)">Enable</button>
                <button v-else class="secondary" :disabled="busy[u.username]" @click="disable(u.username)">Disable</button>
                <button class="secondary" :disabled="busy[u.username]" @click="remove(u.username)">Remove</button>
              </td>
            </template>
          </tr>
        </tbody>
      </table>

      <h2>Groups</h2>
      <form @submit.prevent="createGroup" class="create-form">
        <label>
          New group name
          <input v-model="newGroupName" type="text" required />
        </label>
        <button type="submit" :disabled="busy.createGroup">{{ busy.createGroup ? 'Creating…' : 'Create group' }}</button>
      </form>

      <table class="users-table">
        <thead>
          <tr>
            <th>Name</th>
            <th>Members</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="g in groups.groups" :key="g.name">
            <td>{{ g.name }}</td>
            <td>{{ g.members.length ? g.members.join(', ') : '—' }}</td>
            <td>
              <button class="secondary" :disabled="busy['group:' + g.name]" @click="removeGroup(g.name, g.members.length)">Delete</button>
            </td>
          </tr>
        </tbody>
      </table>
    </template>
  </div>
</template>

<style scoped>
h1 {
  margin-bottom: 1.5rem;
}
h2 {
  margin-top: 2.5rem;
}
.notice {
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 1rem;
  max-width: 32rem;
  margin-bottom: 1rem;
  color: var(--text-dim);
  font-size: 0.9rem;
  background: var(--surface);
}
.revealed {
  border: 1px solid var(--success-text);
  background: var(--success-bg);
  border-radius: 10px;
  padding: 0.75rem 1rem;
  margin-bottom: 1rem;
  display: flex;
  align-items: center;
  gap: 0.75rem;
  flex-wrap: wrap;
  color: var(--success-text);
  font-size: 0.9rem;
}
.revealed code {
  color: var(--text);
}
.create-form {
  display: flex;
  flex-wrap: wrap;
  gap: 0.85rem;
  align-items: flex-end;
  margin-bottom: 1.5rem;
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 1.1rem;
  background: var(--surface);
}
.create-form label {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
  font-size: 0.85rem;
  font-weight: 500;
}
.secret-field {
  display: flex;
  gap: 0.4rem;
}
.secret-field input {
  flex: 1;
  font-family: var(--font-mono);
}
.users-table {
  border-collapse: collapse;
  width: 100%;
  margin-bottom: 2rem;
}
.users-table th {
  text-align: left;
  padding: 0.6rem 0.7rem;
  font-family: var(--font-ui);
  font-size: 0.72rem;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--text-faint);
  border-bottom: 1px solid var(--border);
}
.users-table td {
  text-align: left;
  padding: 0.6rem 0.7rem;
  border-bottom: 1px solid var(--border);
}
.row-actions {
  display: flex;
  gap: 0.4rem;
  flex-wrap: wrap;
}
.hint {
  font-size: 0.78rem;
  color: var(--text-faint);
}
.users-table tr.disabled {
  opacity: 0.55;
}
</style>
