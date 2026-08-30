<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { usePortalStore } from '../../stores/portal'
import { ApiError } from '../../api/client'

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
    <form class="login-box" @submit.prevent="submit">
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
}
.login-box {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  width: 20rem;
  border: 1px solid #333;
  border-radius: 8px;
  padding: 1.5rem;
}
.login-box h1 {
  font-size: 1.25rem;
  margin: 0;
}
.hint {
  font-size: 0.85rem;
  opacity: 0.8;
  margin: 0 0 0.25rem;
}
label {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  font-size: 0.9rem;
}
.error-message {
  color: #e5534b;
  font-size: 0.85rem;
  margin: 0;
}
</style>
