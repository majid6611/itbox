<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import { ApiError } from '../api/client'

const auth = useAuthStore()
const router = useRouter()

const email = ref('')
const password = ref('')
const error = ref<string | null>(null)
const loading = ref(false)

async function handleSubmit() {
  error.value = null
  loading.value = true
  try {
    await auth.login(email.value, password.value)
    router.push({ name: 'dashboard' })
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : 'Login failed'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login">
    <form class="card" @submit.prevent="handleSubmit">
      <h1>IT Platform</h1>
      <p class="subtitle">Admin sign-in</p>
      <label>
        Email
        <input v-model="email" type="email" required autocomplete="username" />
      </label>
      <label>
        Password
        <input v-model="password" type="password" required autocomplete="current-password" />
      </label>
      <p v-if="error" class="error">{{ error }}</p>
      <button type="submit" :disabled="loading">{{ loading ? 'Logging in…' : 'Log in' }}</button>
    </form>
  </div>
</template>

<style scoped>
.login {
  display: flex;
  justify-content: center;
  margin-top: 4.5rem;
}
form {
  display: flex;
  flex-direction: column;
  gap: 0.85rem;
  width: 320px;
  padding: 2rem 1.85rem;
}
h1 {
  font-size: 1.3rem;
  margin-bottom: 0.1rem;
}
.subtitle {
  color: var(--text-dim);
  font-size: 0.85rem;
  margin: 0 0 0.6rem;
}
label {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
  font-size: 0.85rem;
  font-weight: 500;
}
.error {
  color: var(--danger-text);
  font-size: 0.85rem;
  margin: 0;
}
</style>
