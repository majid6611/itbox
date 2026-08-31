<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { usePortalStore } from '../../stores/portal'
import { ApiError } from '../../api/client'
import Icon from '../../components/Icon.vue'

const portal = usePortalStore()
const router = useRouter()
const form = reactive({ username: '', password: '' })
const busy = ref(false)
const error = ref('')

async function submit() {
  busy.value = true
  error.value = ''
  try {
    await portal.login(form.username, form.password)
    router.push({ name: 'portal-wiki' })
  } catch (e) {
    error.value = e instanceof ApiError ? 'Wrong username or password.' : 'Login failed.'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="login-wrap">
    <form class="login-box card" @submit.prevent="submit">
      <div class="mark"><Icon name="mark" :size="22" /></div>
      <h1>Employee Portal</h1>
      <p class="hint">Log in with your company username and password.</p>
      <label>
        Username
        <input v-model="form.username" type="text" autocomplete="username" required />
      </label>
      <label>
        Password
        <input v-model="form.password" type="password" autocomplete="current-password" required />
      </label>
      <p v-if="error" class="error-message">{{ error }}</p>
      <button type="submit" :disabled="busy">{{ busy ? 'Logging in…' : 'Log in' }}</button>
    </form>
  </div>
</template>

<style scoped>
.login-wrap {
  display: flex;
  justify-content: center;
  padding-top: 4rem;
  min-height: calc(100vh - 8.5rem);
  background:
    radial-gradient(circle at 15% 0%, var(--accent-soft), transparent 45%),
    radial-gradient(circle at 100% 30%, var(--accent-soft), transparent 40%);
}
.login-box {
  display: flex;
  flex-direction: column;
  gap: 0.85rem;
  width: 20rem;
  height: fit-content;
  padding: 2.25rem 1.85rem;
}
.mark {
  width: 2.75rem;
  height: 2.75rem;
  border-radius: 10px;
  background: var(--accent-soft);
  color: var(--accent);
  display: flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 0.4rem;
}
.login-box h1 {
  font-size: 1.3rem;
  margin: 0;
}
.hint {
  font-size: 0.85rem;
  color: var(--text-dim);
  margin: 0 0 0.25rem;
}
label {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
  font-size: 0.85rem;
  font-weight: 500;
}
.error-message {
  color: var(--danger-text);
  font-size: 0.85rem;
  margin: 0;
}
</style>
