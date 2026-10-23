<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import { ApiError } from '../api/client'
import Icon from '../components/Icon.vue'

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
      <div class="login-brand"><div class="mark"><Icon name="mark" :size="22" /></div><strong>IT Platform</strong></div>
      <h1>Welcome back</h1>
      <p class="subtitle">Sign in to manage your IT workspace.</p>
      <label>
        Email
        <input v-model="email" type="email" required autocomplete="username" />
      </label>
      <label>
        Password
        <input v-model="password" type="password" required autocomplete="current-password" />
      </label>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <button type="submit" :disabled="loading">{{ loading ? 'Logging in…' : 'Log in' }}</button>
      <p class="other-login">Not an administrator? <router-link to="/portal/login">Employee sign-in</router-link></p>
    </form>
  </div>
</template>

<style scoped>
.other-login {
  margin: 1rem 0 0;
  font-size: 0.85rem;
  color: var(--text-dim);
  text-align: center;
}
.login {
  display: flex;
  justify-content: center;
  margin-top: 4.5rem;
  min-height: calc(100vh - 8.5rem);
  background:
    radial-gradient(circle at 15% 0%, var(--accent-soft), transparent 45%),
    radial-gradient(circle at 100% 30%, var(--accent-soft), transparent 40%);
}
form {
  display: flex;
  flex-direction: column;
  gap: 0.85rem;
  width: 320px;
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

<style scoped>
.login { margin-top: 2.5rem; align-items: center; }
form { width: min(100%, 400px); padding: 2.5rem; border-radius: 16px; gap: 1.1rem; }
.login-brand { display: flex; align-items: center; gap: .75rem; margin-bottom: 1rem; font-size: .9rem; }
.mark { margin: 0; }
h1 { font-size: 1.7rem; letter-spacing: -.04em; }
.subtitle { line-height: 1.6; }
input { min-height: 44px; margin-top: .2rem; }
button[type='submit'] { min-height: 44px; margin-top: .25rem; }
@media (max-width: 480px) { form { padding: 1.75rem 1.25rem; } .login { margin-top: 1rem; } }
</style>
